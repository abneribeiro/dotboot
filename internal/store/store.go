// Package store guarda o que o dotboot produz na pasta de dados: as
// separações (sessões do estúdio), os downloads e os uploads. Cada sessão e
// cada download é uma pasta com um id aleatório e um meta.json; a biblioteca
// é a lista dessas pastas. Nada daqui sai para as pastas do utilizador — os
// ficheiros chegam-lhe pelo browser, quando os descarrega.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ErrNotFound é uma sessão, um download ou um ficheiro que não existe.
var ErrNotFound = errors.New("não encontrado")

const (
	sessionsDir  = "sessoes"
	downloadsDir = "downloads"
	uploadsDir   = "uploads"
	metaFile     = "meta.json"
)

// idPattern valida os ids que chegam nos URLs da API: só hexadecimal, o que
// impede de raiz caminhos como "../".
var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// extPattern aceita as extensões dos ficheiros enviados (.mp3, .flac…).
var extPattern = regexp.MustCompile(`^\.[a-z0-9]{1,5}$`)

// NewID devolve um id aleatório (16 caracteres hexadecimais).
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // nunca falha (crypto/rand.Read)
	return hex.EncodeToString(b)
}

// ValidID reporta se id tem o formato de NewID.
func ValidID(id string) bool {
	return idPattern.MatchString(id)
}

// Session é uma música separada, pronta a abrir no estúdio.
type Session struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Source é o URL ou o nome do ficheiro enviado.
	Source string `json:"source"`
	// Model é o preset usado, e Stems as pistas, pela ordem do preset.
	Model    string    `json:"model"`
	Stems    []string  `json:"stems"`
	Duration float64   `json:"duration"`
	Section  string    `json:"section,omitempty"`
	Created  time.Time `json:"created"`
}

// File é um ficheiro de um download.
type File struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Download é um pedido de vídeo ou de áudio concluído.
type Download struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Title   string    `json:"title"`
	URLs    []string  `json:"urls"`
	Files   []File    `json:"files"`
	Created time.Time `json:"created"`
}

// Store é a pasta de dados.
type Store struct {
	root string
}

// Open prepara a pasta de dados (cria-a se não existir).
func Open(root string) (*Store, error) {
	for _, dir := range []string{sessionsDir, downloadsDir, uploadsDir} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return nil, fmt.Errorf("não foi possível preparar a pasta de dados %s: %w", root, err)
		}
	}
	return &Store{root: root}, nil
}

// Root devolve a pasta de dados.
func (s *Store) Root() string { return s.root }

// SessionDir é a pasta de uma sessão.
func (s *Store) SessionDir(id string) string {
	return filepath.Join(s.root, sessionsDir, id)
}

// DownloadDir é a pasta de um download.
func (s *Store) DownloadDir(id string) string {
	return filepath.Join(s.root, downloadsDir, id)
}

// UploadPath é onde fica um ficheiro enviado. A extensão original mantém-se
// (o ffmpeg usa-a como pista do formato).
func (s *Store) UploadPath(id, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if !extPattern.MatchString(ext) {
		ext = ""
	}
	return filepath.Join(s.root, uploadsDir, id+ext)
}

// FindUpload devolve o ficheiro enviado com este id.
func (s *Store) FindUpload(id string) (string, error) {
	if !ValidID(id) {
		return "", ErrNotFound
	}
	matches, _ := filepath.Glob(filepath.Join(s.root, uploadsDir, id+"*")) // o padrão é sempre válido
	if len(matches) == 0 {
		return "", ErrNotFound
	}
	return matches[0], nil
}

// writeMeta grava v como meta.json de dir, de forma atómica: uma sessão só
// aparece na biblioteca quando está completa.
func writeMeta(dir string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, metaFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("não foi possível gravar %s: %w", metaFile, err)
	}
	return os.Rename(tmp, filepath.Join(dir, metaFile))
}

func readMeta(dir string, v any) error {
	data, err := os.ReadFile(filepath.Join(dir, metaFile))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// list lê o meta.json de cada pasta de parent, ignorando as incompletas.
func list[T any](parent string, created func(T) time.Time) ([]T, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, err
	}
	out := []T{}
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		var v T
		if err := readMeta(filepath.Join(parent, e.Name()), &v); err != nil {
			continue
		}
		out = append(out, v)
	}
	// Os mais recentes primeiro.
	slices.SortFunc(out, func(a, b T) int { return created(b).Compare(created(a)) })
	return out, nil
}

// SaveSession grava a sessão; a pasta tem de existir (é lá que o trabalho
// escreveu as pistas).
func (s *Store) SaveSession(sess Session) error {
	return writeMeta(s.SessionDir(sess.ID), sess)
}

// Sessions devolve as sessões, as mais recentes primeiro.
func (s *Store) Sessions() ([]Session, error) {
	return list(filepath.Join(s.root, sessionsDir), func(v Session) time.Time { return v.Created })
}

// Session devolve uma sessão.
func (s *Store) Session(id string) (Session, error) {
	var sess Session
	if !ValidID(id) {
		return sess, ErrNotFound
	}
	err := readMeta(s.SessionDir(id), &sess)
	return sess, err
}

// TrackPath devolve o ficheiro de uma pista de uma sessão. Só aceita as
// pistas que a sessão tem.
func (s *Store) TrackPath(sess Session, stem string) (string, error) {
	if !slices.Contains(sess.Stems, stem) {
		return "", ErrNotFound
	}
	return filepath.Join(s.SessionDir(sess.ID), stem+".flac"), nil
}

// DeleteSession apaga uma sessão.
func (s *Store) DeleteSession(id string) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	return os.RemoveAll(s.SessionDir(id))
}

// SaveDownload grava um download; a pasta tem de existir.
func (s *Store) SaveDownload(d Download) error {
	return writeMeta(s.DownloadDir(d.ID), d)
}

// Downloads devolve os downloads, os mais recentes primeiro.
func (s *Store) Downloads() ([]Download, error) {
	return list(filepath.Join(s.root, downloadsDir), func(v Download) time.Time { return v.Created })
}

// Download devolve um download.
func (s *Store) Download(id string) (Download, error) {
	var d Download
	if !ValidID(id) {
		return d, ErrNotFound
	}
	err := readMeta(s.DownloadDir(id), &d)
	return d, err
}

// DownloadFile devolve o caminho de um ficheiro de um download. Só aceita os
// ficheiros que o download registou.
func (s *Store) DownloadFile(d Download, name string) (string, error) {
	if !slices.ContainsFunc(d.Files, func(f File) bool { return f.Name == name }) {
		return "", ErrNotFound
	}
	return filepath.Join(s.DownloadDir(d.ID), name), nil
}

// DeleteDownload apaga um download.
func (s *Store) DeleteDownload(id string) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	return os.RemoveAll(s.DownloadDir(id))
}

// Usage soma o espaço ocupado pela pasta de dados.
func (s *Store) Usage() int64 {
	var total int64
	_ = filepath.WalkDir(s.root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // um ficheiro que desapareceu a meio não conta
		}
		if info, err := d.Info(); err == nil && !d.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// Clean apaga o que ficou de trabalhos que não terminaram (pastas sem
// meta.json) e os uploads com mais de maxAge. Corre ao arrancar, quando
// ainda nenhum trabalho está a usar estas pastas.
func (s *Store) Clean(maxAge time.Duration) {
	for _, parent := range []string{sessionsDir, downloadsDir} {
		entries, _ := os.ReadDir(filepath.Join(s.root, parent)) // uma pasta ilegível fica como está
		for _, e := range entries {
			dir := filepath.Join(s.root, parent, e.Name())
			if _, err := os.Stat(filepath.Join(dir, metaFile)); errors.Is(err, fs.ErrNotExist) {
				_ = os.RemoveAll(dir) // lixo de um trabalho interrompido
			}
		}
	}
	entries, _ := os.ReadDir(filepath.Join(s.root, uploadsDir)) // idem
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > maxAge {
			_ = os.Remove(filepath.Join(s.root, uploadsDir, e.Name())) // idem
		}
	}
}
