//go:build !devserver

package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"mappaavventura/internal/paths"
	"mappaavventura/internal/server"
	"mappaavventura/internal/store"
)

//go:embed all:frontend/dist
var embedded embed.FS

// desk collega le richieste della pagina alle finestre del sistema (scegli file, salva, apri cartella).
type desk struct{ ctx context.Context }

func (d *desk) PickFile(accept string) (string, error) {
	return wr.OpenFileDialog(d.ctx, wr.OpenDialogOptions{Title: "Scegli un file", Filters: filtersFor(accept)})
}

func (d *desk) SaveFile(name string, data []byte) (bool, error) {
	p, err := wr.SaveFileDialog(d.ctx, wr.SaveDialogOptions{Title: "Salva la copia", DefaultFilename: name})
	if err != nil || p == "" {
		return false, err
	}
	return true, os.WriteFile(p, data, 0o644)
}

func (d *desk) OpenFolder(path string) error {
	switch goruntime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("explorer", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func (d *desk) OpenURL(url string) error {
	wr.BrowserOpenURL(d.ctx, url)
	return nil
}

// filtersFor trasforma l'attributo accept della pagina (".m4a,.mp3,audio/*") nei filtri della finestra di sistema.
func filtersFor(accept string) []wr.FileFilter {
	mimes := map[string][]string{
		"image/png": {"*.png"}, "image/jpeg": {"*.jpg", "*.jpeg"}, "image/webp": {"*.webp"},
		"application/json": {"*.json"},
	}
	seen, pats := map[string]bool{}, []string{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			pats = append(pats, p)
		}
	}
	for _, a := range strings.Split(accept, ",") {
		a = strings.TrimSpace(strings.ToLower(a))
		if strings.HasPrefix(a, ".") {
			add("*" + a)
		}
		for _, p := range mimes[a] {
			add(p)
		}
	}
	if len(pats) == 0 {
		return nil
	}
	return []wr.FileFilter{{DisplayName: "File adatti (" + strings.Join(pats, ", ") + ")", Pattern: strings.Join(pats, ";")}}
}

func fail(dir string, err error) {
	msg := fmt.Sprintf("La Mappa non riesce a partire: %v\n", err)
	log.Print(msg)
	if dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "errore-avvio.txt"), []byte(msg), 0o644)
	}
	os.Exit(1)
}

func main() {
	dir, portable, err := paths.DataDir()
	if err != nil {
		fail("", err)
	}
	st, err := store.Open(driverName, filepath.Join(dir, "mappa.db"))
	if err != nil {
		fail(dir, err)
	}
	srv, err := server.New(st, dir, portable, appVersion)
	if err != nil {
		fail(dir, err)
	}
	srv.StartLibrary()
	d := &desk{}
	srv.SetDesktop(d)

	dist, err := fs.Sub(embedded, "frontend/dist")
	if err != nil {
		fail(dir, err)
	}
	static := server.StaticHandler(dist)
	srv.StartLAN(static)

	// Tutto passa dal nostro gestore: pagina, dati, brani e immagini arrivano sempre interi.
	app := http.NewServeMux()
	app.Handle("/api/", srv.AppHandler())
	app.Handle("/_blob/", srv.AppHandler())
	app.Handle("/", static)

	err = wails.Run(&options.App{
		Title:            "Mappa dell'Avventura",
		Width:            1366,
		Height:           900,
		MinWidth:         800,
		MinHeight:        560,
		WindowStartState: options.Maximised,
		BackgroundColour: &options.RGBA{R: 18, G: 28, B: 29, A: 255},
		AssetServer:      &assetserver.Options{Handler: app},
		OnStartup:        func(ctx context.Context) { d.ctx = ctx },
		OnShutdown:       func(ctx context.Context) { st.Close() },
		Windows:          &windows.Options{WebviewIsTransparent: false, WindowIsTranslucent: false},
		Mac: &mac.Options{
			About: &mac.AboutInfo{Title: "Mappa dell'Avventura", Message: "Versione " + appVersion},
		},
	})
	if err != nil {
		fail(dir, err)
	}
}
