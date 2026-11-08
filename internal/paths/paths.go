// Package paths decide dove l'app tiene il database.
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// PortableFolder e' il nome della cartella dati "da chiavetta".
const PortableFolder = "MappaDati"

// DataDir restituisce la cartella dei dati e dice se e' quella portatile.
//
// Ordine di ricerca:
//  1. la variabile d'ambiente MAPPA_DATI, se impostata;
//  2. una cartella "MappaDati" accanto al programma (o accanto all'app .app sul Mac): e' la modalita' chiavetta;
//  3. la cartella delle impostazioni dell'utente: ~/Library/Application Support/MappaAvventura sul Mac,
//     %AppData%\MappaAvventura su Windows.
func DataDir() (string, bool, error) {
	if d := os.Getenv("MAPPA_DATI"); d != "" {
		return d, false, os.MkdirAll(d, 0o755)
	}
	if base := appFolder(); base != "" {
		p := filepath.Join(base, PortableFolder)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, true, nil
		}
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", false, err
	}
	d := filepath.Join(cfg, "MappaAvventura")
	return d, false, os.MkdirAll(d, 0o755)
}

// appFolder e' la cartella che contiene il programma (.exe) o il pacchetto .app.
func appFolder() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	dir := filepath.Dir(exe)
	if i := strings.Index(dir, ".app"+string(filepath.Separator)+"Contents"); i >= 0 {
		return filepath.Dir(dir[:i+4])
	}
	return dir
}
