//go:build devserver

// Versione di prova senza finestra: apre la mappa nel browser su http://127.0.0.1:8080
// Si avvia con: make prova
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"mappaavventura/internal/paths"
	"mappaavventura/internal/server"
	"mappaavventura/internal/store"
)

func main() {
	dir, portable, err := paths.DataDir()
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(driverName, filepath.Join(dir, "mappa.db"))
	if err != nil {
		log.Fatal(err)
	}
	srv, err := server.New(st, dir, portable, appVersion+"-prova")
	if err != nil {
		log.Fatal(err)
	}
	srv.StartLibrary()
	if os.Getenv("MAPPA_DESKTOP_FINTO") == "1" {
		srv.SetDesktop(fakeDesk{dir: dir})
	}
	files := server.StaticHandler(os.DirFS("frontend/dist"))
	srv.StartLAN(files)
	mux := http.NewServeMux()
	mux.Handle("/api/", srv.AppHandler())
	mux.Handle("/_blob/", srv.AppHandler())
	mux.Handle("/", files)
	addr := "127.0.0.1:8080"
	if a := os.Getenv("MAPPA_ADDR"); a != "" {
		addr = a
	}
	log.Printf("Mappa di prova su http://%s  (dati in %s)", addr, dir)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// fakeDesk simula le finestre di sistema, solo per le prove automatiche.
type fakeDesk struct{ dir string }

func (f fakeDesk) PickFile(accept string) (string, error) {
	log.Printf("scegli file (accept=%s)", accept)
	return os.Getenv("MAPPA_FILE_PROVA"), nil
}
func (f fakeDesk) SaveFile(name string, data []byte) (bool, error) {
	log.Printf("salva %s (%d byte)", name, len(data))
	return true, os.WriteFile(filepath.Join(f.dir, name), data, 0o644)
}
func (f fakeDesk) OpenFolder(p string) error { log.Printf("apri cartella %s", p); return nil }
func (f fakeDesk) OpenURL(u string) error    { log.Printf("apri indirizzo %s", u); return nil }
