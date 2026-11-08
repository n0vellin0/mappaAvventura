package server

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mappaavventura/internal/store"
)

// Brani che l'app sa riconoscere quando li trovi nella cartella asset/brani.
var audioExt = map[string]string{
	".m4a": "audio/mp4", ".mp4": "audio/mp4", ".aac": "audio/aac", ".mp3": "audio/mpeg",
	".wav": "audio/wav", ".ogg": "audio/ogg", ".webm": "audio/webm",
}

// Collezioni che decidono i nomi dei file: quando cambiano, i file vengono rinominati.
var namingCols = map[string]bool{"tracks": true, "planets": true, "settings": true, "regions": true, "sides": true}

type fileSig struct {
	size int64
	mod  time.Time
}

// StartLibrary tiene in ordine la cartella dei file:
//   - ogni 3 secondi cerca brani nuovi copiati a mano in asset/brani e li aggiunge alla Playlist;
//   - rinomina i file con il titolo che hanno nella mappa (brano, ritratto, sfondo...).
func (s *Server) StartLibrary() {
	go func() {
		ignored := map[string]fileSig{}
		s.importNew(ignored)
		s.syncNames()
		t := time.NewTicker(3 * time.Second)
		for range t.C {
			n := s.importNew(ignored)
			if n > 0 || s.namesDirty.Swap(false) {
				s.syncNames()
			}
		}
	}()
}

func (s *Server) markNames(col string) {
	if namingCols[col] {
		s.namesDirty.Store(true)
	}
}

// importNew registra i brani nuovi trovati in asset/brani e restituisce quanti ne ha aggiunti.
func (s *Server) importNew(ignored map[string]fileSig) int {
	dir := filepath.Join(s.st.AssetDir(), "brani")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	rows, err := s.st.AssetRows()
	if err != nil {
		return 0
	}
	registered := map[string]bool{}
	for _, r := range rows {
		registered[strings.ToLower(r.File)] = true
	}
	added := 0
	for _, e := range entries {
		name := e.Name()
		ct := audioExt[strings.ToLower(filepath.Ext(name))]
		if e.IsDir() || ct == "" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		rel := "brani/" + name
		if registered[strings.ToLower(rel)] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		sig := fileSig{info.Size(), info.ModTime()}
		if old, ok := ignored[rel]; ok && old == sig {
			continue
		}
		// Un file appena copiato potrebbe essere ancora a meta': aspetto che non cambi per qualche secondo.
		if time.Since(info.ModTime()) < 4*time.Second {
			continue
		}
		if dup := s.duplicateOf(filepath.Join(dir, name), info.Size(), rows); dup != "" {
			log.Printf("%s e' uguale a %s, gia' nella mappa: non lo aggiungo", rel, dup)
			ignored[rel] = sig
			continue
		}
		id, err := s.st.RegisterFile(rel, ct, "b64", info.Size())
		if err != nil {
			log.Printf("registrazione di %s non riuscita: %v", rel, err)
			continue
		}
		title := strings.TrimSuffix(name, filepath.Ext(name))
		track := map[string]any{"name": title, "order": s.nextTrackOrder(), "audioId": id, "audioEnc": "b64", "audioName": name, "regions": []string{}}
		body, _ := json.Marshal(track)
		if err := s.st.Put("tracks", "t"+id[:12], body); err != nil {
			log.Printf("aggiunta di %s alla Playlist non riuscita: %v", rel, err)
			continue
		}
		log.Printf("nuovo brano nella Playlist: %s", title)
		added++
	}
	return added
}

func (s *Server) duplicateOf(p string, size int64, rows []store.AssetRow) string {
	var h string
	for _, r := range rows {
		if r.Size != size || !strings.HasPrefix(r.ContentType, "audio/") {
			continue
		}
		if h == "" {
			var err error
			if h, err = store.FileHash(p); err != nil {
				return ""
			}
		}
		if other, err := s.st.HashAsset(r); err == nil && other == h {
			return r.File
		}
	}
	return ""
}

func (s *Server) nextTrackOrder() float64 {
	docs, _ := s.st.List("tracks")
	max := 0.0
	for _, d := range docs {
		var v struct {
			Order float64 `json:"order"`
		}
		if json.Unmarshal(d.Data, &v) == nil && v.Order > max {
			max = v.Order
		}
	}
	return max + 1
}

// syncNames da' a ogni file il nome che ha nella mappa.
func (s *Server) syncNames() {
	labels := s.assetLabels()
	n, err := s.st.ApplyNames(labels)
	if err != nil {
		log.Printf("rinomina dei file non riuscita: %v", err)
	} else if n > 0 {
		log.Printf("rinominati %d file con il loro titolo", n)
	}
}

type obj = map[string]any

func str(m obj, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func num(m obj, k string) float64 {
	if v, ok := m[k].(float64); ok {
		return v
	}
	return 999
}

func (s *Server) docs(col string) []obj {
	list, _ := s.st.List(col)
	out := make([]obj, 0, len(list))
	for _, d := range list {
		var m obj
		if json.Unmarshal(d.Data, &m) == nil && m != nil {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return num(out[i], "order") < num(out[j], "order") })
	return out
}

// assetLabels decide il titolo di ogni file partendo da dove e' usato nella mappa.
func (s *Server) assetLabels() map[string]string {
	labels := map[string]string{}
	set := func(id, label string) {
		if id != "" && strings.TrimSpace(label) != "" && labels[id] == "" {
			labels[id] = label
		}
	}
	for _, t := range s.docs("tracks") {
		title := str(t, "name")
		if title == "" {
			title = strings.TrimSuffix(str(t, "audioName"), filepath.Ext(str(t, "audioName")))
		}
		set(str(t, "audioId"), title)
	}
	for _, p := range s.docs("planets") {
		set(str(p, "audioId"), "Pianeta - "+str(p, "name"))
	}
	if raw, err := s.st.Get("settings", "carillon"); err == nil {
		var c obj
		if json.Unmarshal(raw, &c) == nil {
			set(str(c, "audioId"), "Canzone dell'allievo")
		}
	}
	if raw, err := s.st.Get("settings", "sounds"); err == nil {
		var snd map[string]obj
		if json.Unmarshal(raw, &snd) == nil {
			for _, k := range []struct{ key, label string }{{"dorme", "Torna a dormire"}, {"agita", "Si agita"}, {"quasi", "Sta per svegliarsi"}, {"sveglio", "Sveglio"}} {
				set(str(snd[k.key], "audioId"), "Dragone - "+k.label)
			}
		}
	}
	for _, r := range s.docs("regions") {
		name := str(r, "name")
		if ch, ok := r["character"].(map[string]any); ok {
			who := str(ch, "name")
			if who == "" {
				who = name
			}
			set(str(ch, "imageId"), "Ritratto - "+who)
		}
		set(str(r, "bgId"), "Sfondo - "+name)
		set(str(r, "loreImgId"), "Pergamena - "+name)
	}
	for _, sd := range s.docs("sides") {
		set(str(sd, "bgId"), "Sfondo - "+str(sd, "name"))
	}
	return labels
}
