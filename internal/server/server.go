// Package server espone i dati della Mappa alla pagina, sia dentro la finestra dell'app
// sia sulla rete locale per il telefono usato come telecomando.
package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"mappaavventura/internal/store"
)

// Desktop sono le azioni che solo la finestra dell'app puo' chiedere al computer.
type Desktop interface {
	PickFile(accept string) (string, error)          // percorso scelto, "" se annullato
	SaveFile(name string, data []byte) (bool, error) // false se annullato
	OpenFolder(path string) error
	OpenURL(url string) error
}

// Server tiene insieme archivio, cartella dei dati e telecomando.
type Server struct {
	st       *store.Store
	dataDir  string
	portable bool
	version  string
	desk     Desktop
	token    string
	lanPort  int
	picked   sync.Map // token -> pickedFile

	namesDirty atomic.Bool // i titoli sono cambiati: i file vanno rinominati
}

type pickedFile struct {
	path string
	name string
	at   time.Time
}

// New crea il server.
func New(st *store.Store, dataDir string, portable bool, version string) (*Server, error) {
	tok, err := st.Token()
	if err != nil {
		return nil, err
	}
	return &Server{st: st, dataDir: dataDir, portable: portable, version: version, token: tok}, nil
}

// SetDesktop collega le azioni della finestra dell'app.
func (s *Server) SetDesktop(d Desktop) { s.desk = d }

const maxAsset = 200 << 20 // 200 MB per file

var segRe = regexp.MustCompile(`^[A-Za-z0-9_\-.~:@+]{1,200}$`)

func validSeg(v string) bool { return segRe.MatchString(v) && v != "." && v != ".." }

// AppHandler serve le richieste della finestra dell'app (azioni sul computer permesse).
func (s *Server) AppHandler() http.Handler { return s.api(true) }

func (s *Server) api(desktop bool) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]any{"desktop": desktop && s.desk != nil, "version": s.version}
		if desktop {
			info["dataDir"] = s.dataDir
			info["portable"] = s.portable
			info["remoteURLs"] = s.RemoteURLs()
		}
		writeJSON(w, info)
	})

	mux.HandleFunc("GET /api/versions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.st.Versions())
	})

	mux.HandleFunc("GET /api/col/{col}", func(w http.ResponseWriter, r *http.Request) {
		col := r.PathValue("col")
		if !validSeg(col) {
			http.Error(w, "nome non valido", http.StatusBadRequest)
			return
		}
		docs, err := s.st.List(col)
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, docs)
	})

	mux.HandleFunc("GET /api/doc/{col}/{id}", func(w http.ResponseWriter, r *http.Request) {
		col, id := r.PathValue("col"), r.PathValue("id")
		if !validSeg(col) || !validSeg(id) {
			http.Error(w, "nome non valido", http.StatusBadRequest)
			return
		}
		data, err := s.st.Get(col, id)
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, map[string]any{"exists": false})
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, map[string]any{"exists": true, "data": data})
	})

	mux.HandleFunc("PUT /api/doc/{col}/{id}", func(w http.ResponseWriter, r *http.Request) {
		col, id := r.PathValue("col"), r.PathValue("id")
		if !validSeg(col) || !validSeg(id) {
			http.Error(w, "nome non valido", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			http.Error(w, "lettura non riuscita", http.StatusBadRequest)
			return
		}
		if err := s.st.Put(col, id, body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.markNames(col)
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("DELETE /api/doc/{col}/{id}", func(w http.ResponseWriter, r *http.Request) {
		col, id := r.PathValue("col"), r.PathValue("id")
		if !validSeg(col) || !validSeg(id) {
			http.Error(w, "nome non valido", http.StatusBadRequest)
			return
		}
		if err := s.st.Delete(col, id); err != nil {
			serverError(w, err)
			return
		}
		s.markNames(col)
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/assets", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.st.ListAssets()
		if err != nil {
			serverError(w, err)
			return
		}
		var used int64
		for _, a := range list {
			used += a.SizeBytes
		}
		writeJSON(w, map[string]any{"assets": list, "usage": map[string]any{"bytes": used, "count": len(list)}})
	})

	mux.HandleFunc("POST /api/assets", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(io.LimitReader(r.Body, maxAsset+1))
		if err != nil {
			http.Error(w, "lettura non riuscita", http.StatusBadRequest)
			return
		}
		if len(data) > maxAsset {
			http.Error(w, "file troppo grande", http.StatusRequestEntityTooLarge)
			return
		}
		ct := r.Header.Get("Content-Type")
		if ct == "" {
			ct = http.DetectContentType(data)
		}
		id, err := s.st.PutAsset(ct, data)
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, map[string]any{"id": id, "url": "/_blob/" + id, "sizeBytes": len(data), "contentType": ct})
	})

	mux.HandleFunc("DELETE /api/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !validSeg(id) {
			http.Error(w, "nome non valido", http.StatusBadRequest)
			return
		}
		if err := s.st.DeleteAsset(id); err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /_blob/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !validSeg(id) {
			http.NotFound(w, r)
			return
		}
		a, raw, err := s.st.ReadAsset(id)
		if errors.Is(err, store.ErrNotFound) || os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		if a.Enc == "b64" {
			// La pagina vuole i brani come testo "data:...;base64,...": lo ricostruisce dal file audio.
			writeWhole(w, "text/plain; charset=utf-8", []byte("data:"+a.ContentType+";base64,"+base64.StdEncoding.EncodeToString(raw)))
			return
		}
		data := raw
		writeWhole(w, a.ContentType, data)
	})

	if desktop {
		s.desktopRoutes(mux)
	}
	return mux
}

func (s *Server) desktopRoutes(mux *http.ServeMux) {
	need := func(w http.ResponseWriter) bool {
		if s.desk == nil {
			http.Error(w, "solo nell'app", http.StatusForbidden)
			return false
		}
		return true
	}

	mux.HandleFunc("POST /api/desktop/pick", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		p, err := s.desk.PickFile(r.URL.Query().Get("accept"))
		if err != nil {
			serverError(w, err)
			return
		}
		if p == "" {
			writeJSON(w, map[string]any{"token": ""})
			return
		}
		tok := fmt.Sprintf("%d", time.Now().UnixNano())
		name := filepath.Base(p)
		s.picked.Store(tok, pickedFile{path: p, name: name, at: time.Now()})
		writeJSON(w, map[string]any{"token": tok, "name": name, "type": mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))})
	})

	mux.HandleFunc("GET /api/desktop/picked/{token}", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		v, ok := s.picked.LoadAndDelete(r.PathValue("token"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		pf := v.(pickedFile)
		data, err := os.ReadFile(pf.path)
		if err != nil {
			serverError(w, err)
			return
		}
		if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(pf.name))); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Write(data)
	})

	mux.HandleFunc("POST /api/desktop/save", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, maxAsset))
		if err != nil {
			http.Error(w, "lettura non riuscita", http.StatusBadRequest)
			return
		}
		name := filepath.Base(r.URL.Query().Get("name"))
		saved, err := s.desk.SaveFile(name, data)
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, map[string]any{"saved": saved})
	})

	mux.HandleFunc("POST /api/desktop/open-data", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		if err := s.desk.OpenFolder(s.dataDir); err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/desktop/qr/{i}", func(w http.ResponseWriter, r *http.Request) {
		urls := s.RemoteURLs()
		i, err := strconv.Atoi(r.PathValue("i"))
		if err != nil || i < 0 || i >= len(urls) {
			http.NotFound(w, r)
			return
		}
		png, err := qrcode.Encode(urls[i], qrcode.Medium, 360)
		if err != nil {
			serverError(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(png)
	})

	mux.HandleFunc("POST /api/desktop/open-regia", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		if s.lanPort == 0 {
			http.Error(w, "server locale non attivo", http.StatusServiceUnavailable)
			return
		}
		u := fmt.Sprintf("http://127.0.0.1:%d/?k=%s#telecomando", s.lanPort, s.token)
		if err := s.desk.OpenURL(u); err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
}

// LANHandler serve pagina e dati al browser (telefono o seconda finestra), protetti dal codice segreto.
func (s *Server) LANHandler(assets http.Handler) http.Handler {
	api := s.api(false)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := false
		if k := r.URL.Query().Get("k"); k != "" && k == s.token {
			http.SetCookie(w, &http.Cookie{Name: "mappa", Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 400 * 24 * 3600})
			ok = true
		} else if c, err := r.Cookie("mappa"); err == nil && c.Value == s.token {
			ok = true
		}
		if !ok {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width"><body style="font:18px system-ui;padding:24px;background:#121c1d;color:#efe4c8"><h1>Mappa dell'Avventura</h1><p>Per usare il telefono come telecomando apri l'indirizzo completo che trovi nell'app: Regia → Classi → App sul computer.</p>`)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/_blob/") {
			api.ServeHTTP(w, r)
			return
		}
		assets.ServeHTTP(w, r)
	})
}

// StartLAN avvia il server per il telecomando sulla prima porta libera tra 8765 e 8775.
func (s *Server) StartLAN(assets http.Handler) {
	h := s.LANHandler(assets)
	for port := 8765; port <= 8775; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			continue
		}
		s.lanPort = port
		go func() {
			srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
			if err := srv.Serve(ln); err != nil {
				log.Printf("server del telecomando fermo: %v", err)
			}
		}()
		return
	}
	log.Printf("nessuna porta libera per il telecomando")
}

// RemoteURLs restituisce gli indirizzi da aprire sul telefono (uno per ogni rete del computer).
func (s *Server) RemoteURLs() []string {
	out := []string{}
	if s.lanPort == 0 {
		return out
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, fmt.Sprintf("http://%s:%d/?k=%s#telecomando", ipn.IP.String(), s.lanPort, s.token))
		}
	}
	return out
}

// writeWhole invia un file tutto in una volta, con la sua lunghezza.
// Nella finestra del Mac (WKWebView) le risposte spezzate in tanti pezzi possono arrivare a meta':
// cosi' l'immagine della mappa e le altre immagini arrivano sempre intere.
func writeWhole(w http.ResponseWriter, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

var staticTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8",
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp", ".svg": "image/svg+xml",
	".woff2": "font/woff2", ".woff": "font/woff", ".json": "application/json", ".txt": "text/plain; charset=utf-8",
}

// StaticHandler serve i file della pagina (index.html, shim.js, mappa, font), ognuno in un solo invio.
func StaticHandler(files fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "metodo non permesso", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ct := staticTypes[strings.ToLower(path.Ext(name))]
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Cache-Control", "no-cache")
		writeWhole(w, ct, data)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("errore: %v", err)
	http.Error(w, "errore interno", http.StatusInternalServerError)
}
