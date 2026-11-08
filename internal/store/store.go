// Package store salva i dati della Mappa:
//   - mappa.db (SQLite): i documenti della pagina (regioni, classi, impostazioni...) e l'elenco dei file;
//   - la cartella asset/ accanto al database: i brani e le immagini, come file normali.
package store

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AssetFolder e' il nome della cartella dei brani e delle immagini, accanto a mappa.db.
// Dentro, i file sono divisi per tipologia: brani/, immagini/, altro/.
const AssetFolder = "asset"

// kindFolder sceglie la sottocartella in base al tipo di file.
func kindFolder(contentType string) string {
	switch {
	case strings.HasPrefix(contentType, "audio/"):
		return "brani"
	case strings.HasPrefix(contentType, "image/"):
		return "immagini"
	default:
		return "altro"
	}
}

// ErrNotFound indica un documento o un file che non esiste.
var ErrNotFound = errors.New("non trovato")

// Doc e' un documento di una collezione, come lo vede la pagina.
type Doc struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

// Asset e' un file caricato dalla pagina (brano o immagine), salvato nella cartella asset/.
//
// Enc vale "b64" per i brani: la pagina li carica come testo "data:...;base64,..." e se li aspetta
// indietro cosi'. L'app li salva comunque come file audio veri (.m4a, .mp3, .wav) e li riconverte al volo.
type Asset struct {
	ID          string
	ContentType string
	Enc         string
	Path        string
	Size        int64
	Created     time.Time
}

// Store e' l'archivio: database SQLite piu' cartella dei file.
type Store struct {
	db       *sql.DB
	assetDir string
	fileMu   sync.RWMutex // protegge i file della cartella asset mentre vengono letti, scritti o rinominati
	mu       sync.Mutex
	seq      int64
	vers     map[string]int64
}

const schema = `
CREATE TABLE IF NOT EXISTS docs (
	col        TEXT    NOT NULL,
	id         TEXT    NOT NULL,
	data       TEXT    NOT NULL,
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (col, id)
);
CREATE TABLE IF NOT EXISTS assets (
	id           TEXT    PRIMARY KEY,
	content_type TEXT    NOT NULL,
	enc          TEXT    NOT NULL DEFAULT '',
	file         TEXT    NOT NULL,
	size         INTEGER NOT NULL,
	created_at   INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (
	k TEXT PRIMARY KEY,
	v TEXT NOT NULL
);`

// Open apre (o crea) il database. driver e' il nome del driver SQLite registrato.
// I brani e le immagini stanno nella cartella asset/ accanto al file del database.
func Open(driver, path string) (*Store, error) {
	assetDir := filepath.Join(filepath.Dir(path), AssetFolder)
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open(driver, path)
	if err != nil {
		return nil, err
	}
	// Una sola connessione: niente blocchi tra scritture, e il file resta coerente anche su chiavetta.
	db.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = DELETE",
		"PRAGMA synchronous = FULL",
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	st := &Store{db: db, assetDir: assetDir, seq: time.Now().UnixMilli(), vers: map[string]int64{}}
	if err := st.migrateBlobs(); err != nil {
		db.Close()
		return nil, fmt.Errorf("spostamento dei file nella cartella asset: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creazione tabelle: %w", err)
	}
	if err := st.sortIntoFolders(); err != nil {
		db.Close()
		return nil, fmt.Errorf("divisione dei file per tipologia: %w", err)
	}
	// Le versioni ripartono da un numero nuovo a ogni avvio, cosi' le pagine aperte rileggono tutto.
	return st, nil
}

// migrateBlobs sposta nella cartella asset/ i file salvati dentro il database dalla prima versione dell'app.
func (s *Store) migrateBlobs() error {
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('assets') WHERE name = 'data'`).Scan(&n); err != nil || n == 0 {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE assets RENAME TO assets_old`); err != nil {
		return err
	}
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT id, content_type, data, created_at FROM assets_old`)
	if err != nil {
		return err
	}
	type old struct {
		id, ct  string
		data    []byte
		created int64
	}
	var all []old
	for rows.Next() {
		var o old
		if err := rows.Scan(&o.id, &o.ct, &o.data, &o.created); err != nil {
			rows.Close()
			return err
		}
		all = append(all, o)
	}
	rows.Close()
	for _, o := range all {
		if err := s.writeAsset(o.id, o.ct, o.data, o.created); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`DROP TABLE assets_old`); err != nil {
		return err
	}
	_, err = s.db.Exec(`VACUUM`)
	return err
}

// Close chiude il database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) bump(col string) {
	s.mu.Lock()
	s.seq++
	s.vers[col] = s.seq
	s.mu.Unlock()
}

// Versions restituisce, per ogni collezione cambiata da quando l'app e' partita, il suo numero di versione.
func (s *Store) Versions() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.vers)+1)
	for k, v := range s.vers {
		out[k] = v
	}
	return out
}

// List restituisce tutti i documenti di una collezione.
func (s *Store) List(col string) ([]Doc, error) {
	rows, err := s.db.Query(`SELECT id, data FROM docs WHERE col = ? ORDER BY id`, col)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Doc{}
	for rows.Next() {
		var id, data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		out = append(out, Doc{ID: id, Data: json.RawMessage(data)})
	}
	return out, rows.Err()
}

// Get restituisce un documento.
func (s *Store) Get(col, id string) (json.RawMessage, error) {
	var data string
	err := s.db.QueryRow(`SELECT data FROM docs WHERE col = ? AND id = ?`, col, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// Put sostituisce un documento. data deve essere un oggetto JSON.
func (s *Store) Put(col, id string, data []byte) error {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return fmt.Errorf("il documento deve essere un oggetto JSON")
	}
	_, err := s.db.Exec(`INSERT INTO docs (col, id, data, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (col, id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		col, id, string(data), time.Now().UnixMilli())
	if err == nil {
		s.bump(col)
	}
	return err
}

// Delete elimina un documento (non e' un errore se non esiste).
func (s *Store) Delete(col, id string) error {
	_, err := s.db.Exec(`DELETE FROM docs WHERE col = ? AND id = ?`, col, id)
	if err == nil {
		s.bump(col)
	}
	return err
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

var extFor = map[string]string{
	"audio/mp4": ".m4a", "audio/x-m4a": ".m4a", "audio/aac": ".aac", "audio/mpeg": ".mp3", "audio/mp3": ".mp3",
	"audio/wav": ".wav", "audio/x-wav": ".wav", "audio/wave": ".wav", "audio/ogg": ".ogg", "audio/webm": ".webm",
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif", "image/svg+xml": ".svg",
	"application/json": ".json", "text/plain": ".txt",
}

func extension(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	if e, ok := extFor[ct]; ok {
		return e
	}
	if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

// decodeDataURL riconosce i brani che la pagina invia come testo "data:audio/...;base64,...".
func decodeDataURL(data []byte) (string, []byte, bool) {
	if !bytes.HasPrefix(data, []byte("data:")) {
		return "", nil, false
	}
	i := bytes.Index(data, []byte(";base64,"))
	if i < 0 || i > 200 {
		return "", nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(data[i+8:])))
	if err != nil {
		return "", nil, false
	}
	return string(data[5:i]), raw, true
}

// writeAsset scrive il file nella cartella asset/ e lo registra nel database.
func (s *Store) writeAsset(id, contentType string, data []byte, created int64) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	enc := ""
	if strings.HasPrefix(contentType, "text/plain") {
		if ct, raw, ok := decodeDataURL(data); ok {
			contentType, data, enc = ct, raw, "b64"
		}
	}
	name := kindFolder(contentType) + "/" + id + extension(contentType)
	final := filepath.Join(s.assetDir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO assets (id, content_type, enc, file, size, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET content_type = excluded.content_type, enc = excluded.enc, file = excluded.file, size = excluded.size`,
		id, contentType, enc, name, len(data), created)
	if err != nil {
		os.Remove(final)
	}
	return err
}

// PutAsset salva un file e ne restituisce l'id.
func (s *Store) PutAsset(contentType string, data []byte) (string, error) {
	id := newID()
	return id, s.writeAsset(id, contentType, data, time.Now().UnixMilli())
}

// GetAsset restituisce dove sta un file e di che tipo e'.
func (s *Store) GetAsset(id string) (*Asset, error) {
	a := &Asset{ID: id}
	var created int64
	var file string
	err := s.db.QueryRow(`SELECT content_type, enc, file, size, created_at FROM assets WHERE id = ?`, id).
		Scan(&a.ContentType, &a.Enc, &file, &a.Size, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.Path = s.assetPath(file)
	a.Created = time.UnixMilli(created)
	return a, nil
}

// assetPath trasforma il nome registrato ("brani/abc.m4a") nel percorso del file, senza mai uscire dalla cartella asset.
func (s *Store) assetPath(file string) string {
	clean := filepath.Clean(filepath.FromSlash(file))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		clean = filepath.Base(clean)
	}
	return filepath.Join(s.assetDir, clean)
}

// sortIntoFolders sposta nelle sottocartelle brani/ e immagini/ i file salvati dalla versione 1.1.0,
// che li teneva tutti insieme nella cartella asset/.
func (s *Store) sortIntoFolders() error {
	rows, err := s.db.Query(`SELECT id, content_type, file FROM assets WHERE file NOT LIKE '%/%'`)
	if err != nil {
		return err
	}
	type mv struct{ id, ct, file string }
	var todo []mv
	for rows.Next() {
		var m mv
		if err := rows.Scan(&m.id, &m.ct, &m.file); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, m)
	}
	rows.Close()
	for _, m := range todo {
		name := kindFolder(m.ct) + "/" + filepath.Base(m.file)
		from, to := s.assetPath(m.file), s.assetPath(name)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
			return err
		}
		if _, err := s.db.Exec(`UPDATE assets SET file = ? WHERE id = ?`, name, m.id); err != nil {
			return err
		}
	}
	return nil
}

// AssetInfo descrive un file senza il suo contenuto.
type AssetInfo struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
}

// ListAssets elenca i file salvati.
func (s *Store) ListAssets() ([]AssetInfo, error) {
	rows, err := s.db.Query(`SELECT id, content_type, size FROM assets ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssetInfo{}
	for rows.Next() {
		var a AssetInfo
		if err := rows.Scan(&a.ID, &a.ContentType, &a.SizeBytes); err != nil {
			return nil, err
		}
		a.URL = "/_blob/" + a.ID
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAsset elimina un file dalla cartella e dal database.
func (s *Store) DeleteAsset(id string) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	a, err := s.GetAsset(id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM assets WHERE id = ?`, id); err != nil {
		return err
	}
	if err := os.Remove(a.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Meta legge un valore interno dell'app ("" se manca).
func (s *Store) Meta(k string) string {
	var v string
	_ = s.db.QueryRow(`SELECT v FROM meta WHERE k = ?`, k).Scan(&v)
	return v
}

// SetMeta scrive un valore interno dell'app.
func (s *Store) SetMeta(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO meta (k, v) VALUES (?, ?) ON CONFLICT (k) DO UPDATE SET v = excluded.v`, k, v)
	return err
}

// Token restituisce il codice segreto che protegge il telecomando, creandolo la prima volta.
func (s *Store) Token() (string, error) {
	if t := s.Meta("remote_token"); t != "" {
		return t, nil
	}
	t := newID()[:12]
	return t, s.SetMeta("remote_token", t)
}
