package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// AssetRow e' un file registrato, come sta nel database.
type AssetRow struct {
	ID          string
	ContentType string
	Enc         string
	File        string // percorso dentro asset/, con le barre "/" (es. "brani/Portalume.wav")
	Size        int64
	Created     int64
}

// AssetDir e' la cartella asset/ accanto al database.
func (s *Store) AssetDir() string { return s.assetDir }

// AssetRows elenca tutti i file registrati.
func (s *Store) AssetRows() ([]AssetRow, error) {
	rows, err := s.db.Query(`SELECT id, content_type, enc, file, size, created_at FROM assets ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AssetRow
	for rows.Next() {
		var r AssetRow
		if err := rows.Scan(&r.ID, &r.ContentType, &r.Enc, &r.File, &r.Size, &r.Created); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReadAsset legge un file registrato (mentre nessuno lo sta rinominando).
func (s *Store) ReadAsset(id string) (*Asset, []byte, error) {
	s.fileMu.RLock()
	defer s.fileMu.RUnlock()
	a, err := s.GetAsset(id)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(a.Path)
	return a, data, err
}

// RegisterFile registra un file che si trova gia' nella cartella asset (rel e' tipo "brani/Canzone.m4a").
func (s *Store) RegisterFile(rel, contentType, enc string, size int64) (string, error) {
	id := newID()
	_, err := s.db.Exec(`INSERT INTO assets (id, content_type, enc, file, size, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, contentType, enc, rel, size, time.Now().UnixMilli())
	return id, err
}

// FileHash calcola l'impronta di un file, per riconoscere i doppioni.
func FileHash(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashAsset calcola l'impronta di un file registrato.
func (s *Store) HashAsset(r AssetRow) (string, error) {
	s.fileMu.RLock()
	defer s.fileMu.RUnlock()
	return FileHash(s.assetPath(r.File))
}

// CleanName rende un titolo adatto a diventare il nome di un file.
func CleanName(label string) string {
	var b strings.Builder
	for _, r := range label {
		switch {
		case r < 32 || strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	out = strings.Trim(out, " .-")
	for utf8.RuneCountInString(out) > 80 {
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	return strings.TrimSpace(out)
}

// ApplyNames rinomina i file in modo che ognuno porti il titolo che ha nella mappa.
// labels associa all'id di un file il suo titolo (senza estensione). I file senza titolo restano come sono.
// Se due file avrebbero lo stesso nome, al secondo si aggiunge " (2)", al terzo " (3)" e cosi' via.
func (s *Store) ApplyNames(labels map[string]string) (int, error) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	rows, err := s.AssetRows()
	if err != nil {
		return 0, err
	}
	registered := map[string]bool{}
	for _, r := range rows {
		registered[strings.ToLower(r.File)] = true
	}
	// Nomi gia' occupati: file presenti nella cartella ma non registrati (non vanno mai sovrascritti).
	used := map[string]map[string]bool{}
	occupy := func(folder, name string) {
		if used[folder] == nil {
			used[folder] = map[string]bool{}
		}
		used[folder][strings.ToLower(name)] = true
	}
	taken := func(folder, name string) bool { return used[folder][strings.ToLower(name)] }
	folders := map[string]bool{}
	for _, r := range rows {
		folders[path.Dir(r.File)] = true
	}
	for f := range folders {
		entries, _ := os.ReadDir(filepath.Join(s.assetDir, filepath.FromSlash(f)))
		for _, e := range entries {
			if !e.IsDir() && !registered[strings.ToLower(f+"/"+e.Name())] {
				occupy(f, e.Name())
			}
		}
	}
	// Prima i file senza titolo tengono il loro nome, poi gli altri prendono il titolo.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Created < rows[j].Created })
	type move struct {
		r      AssetRow
		target string
	}
	var moves []move
	for _, r := range rows {
		if CleanName(labels[r.ID]) == "" {
			occupy(path.Dir(r.File), path.Base(r.File))
		}
	}
	for _, r := range rows {
		base := CleanName(labels[r.ID])
		if base == "" {
			continue
		}
		folder, ext := path.Dir(r.File), strings.ToLower(path.Ext(r.File))
		name := base + ext
		for n := 2; taken(folder, name); n++ {
			name = fmt.Sprintf("%s (%d)%s", base, n, ext)
		}
		occupy(folder, name)
		if target := folder + "/" + name; target != r.File {
			moves = append(moves, move{r, target})
		}
	}
	// Due passaggi (prima nomi provvisori, poi definitivi), cosi' due file possono anche scambiarsi il nome.
	var staged []move
	for _, m := range moves {
		tmp := path.Dir(m.r.File) + "/." + m.r.ID + ".rinomina"
		if err := os.Rename(s.assetPath(m.r.File), s.assetPath(tmp)); err != nil {
			continue // file mancante o bloccato: lo lascio com'e'
		}
		if _, err := s.db.Exec(`UPDATE assets SET file = ? WHERE id = ?`, tmp, m.r.ID); err != nil {
			os.Rename(s.assetPath(tmp), s.assetPath(m.r.File))
			return 0, err
		}
		m.r.File = tmp
		staged = append(staged, m)
	}
	done := 0
	for _, m := range staged {
		if err := os.Rename(s.assetPath(m.r.File), s.assetPath(m.target)); err != nil {
			return done, err
		}
		if _, err := s.db.Exec(`UPDATE assets SET file = ? WHERE id = ?`, m.target, m.r.ID); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}
