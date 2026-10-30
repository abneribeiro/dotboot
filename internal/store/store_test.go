package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestIDs(t *testing.T) {
	id := NewID()
	if !ValidID(id) || id == NewID() {
		t.Fatalf("NewID = %q", id)
	}
	for _, bad := range []string{"", "../etc/passwd", "ABCDEF0123456789", "0123456789abcdef0", "0123456789abcde/"} {
		if ValidID(bad) {
			t.Errorf("ValidID(%q) devia ser falso", bad)
		}
	}
}

func TestSessoes(t *testing.T) {
	s := openTemp(t)
	now := time.Now()
	older := Session{ID: NewID(), Title: "Antiga", Stems: []string{"vozes", "instrumental"}, Created: now.Add(-time.Hour)}
	newer := Session{ID: NewID(), Title: "Nova", Stems: []string{"vozes", "baixo"}, Created: now}
	for _, sess := range []Session{older, newer} {
		if err := os.MkdirAll(s.SessionDir(sess.ID), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	// Uma pasta sem meta.json (um trabalho que não acabou) não aparece.
	if err := os.MkdirAll(s.SessionDir(NewID()), 0o755); err != nil {
		t.Fatal(err)
	}

	list, err := s.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Title != "Nova" || list[1].Title != "Antiga" {
		t.Fatalf("Sessions = %+v, quero a mais recente primeiro", list)
	}

	got, err := s.Session(newer.ID)
	if err != nil || got.Title != "Nova" {
		t.Fatalf("Session = %+v, %v", got, err)
	}
	if path, err := s.TrackPath(got, "baixo"); err != nil || path != filepath.Join(s.SessionDir(newer.ID), "baixo.flac") {
		t.Errorf("TrackPath(baixo) = %q, %v", path, err)
	}
	if _, err := s.TrackPath(got, "guitarra"); !errors.Is(err, ErrNotFound) {
		t.Errorf("uma pista que a sessão não tem devia dar ErrNotFound: %v", err)
	}
	if _, err := s.Session("../../etc"); !errors.Is(err, ErrNotFound) {
		t.Errorf("id inválido devia dar ErrNotFound: %v", err)
	}
	if _, err := s.Session(NewID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("sessão inexistente devia dar ErrNotFound: %v", err)
	}

	if err := s.DeleteSession(older.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Sessions(); len(list) != 1 {
		t.Errorf("depois de apagar ficou %d sessão(ões)", len(list))
	}
}

func TestDownloads(t *testing.T) {
	s := openTemp(t)
	d := Download{ID: NewID(), Kind: "audio", Title: "Música", Files: []File{{Name: "Música [abc].mp3", Size: 3}}}
	if err := os.MkdirAll(s.DownloadDir(d.ID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDownload(d); err != nil {
		t.Fatal(err)
	}
	got, err := s.Download(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DownloadFile(got, "Música [abc].mp3"); err != nil {
		t.Errorf("DownloadFile do ficheiro registado: %v", err)
	}
	for _, name := range []string{"meta.json", "../meta.json", ".dotboot-archive.txt"} {
		if _, err := s.DownloadFile(got, name); !errors.Is(err, ErrNotFound) {
			t.Errorf("DownloadFile(%q) devia dar ErrNotFound: %v", name, err)
		}
	}
	if list, _ := s.Downloads(); len(list) != 1 || list[0].ID != d.ID {
		t.Errorf("Downloads = %+v", list)
	}
}

func TestUploadsELimpeza(t *testing.T) {
	s := openTemp(t)
	id := NewID()
	path := s.UploadPath(id, "A Minha Música.MP3")
	if filepath.Base(path) != id+".mp3" {
		t.Errorf("UploadPath = %q, quero o id com a extensão em minúsculas", path)
	}
	if filepath.Base(s.UploadPath(id, "sem-extensao")) != id {
		t.Error("um nome sem extensão fica só com o id")
	}
	if filepath.Base(s.UploadPath(id, "x.mp3/../../y")) != id {
		t.Error("uma extensão estranha é ignorada")
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if found, err := s.FindUpload(id); err != nil || found != path {
		t.Errorf("FindUpload = %q, %v", found, err)
	}
	if _, err := s.FindUpload("*"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindUpload com um padrão devia dar ErrNotFound: %v", err)
	}

	// Clean: o upload antigo e a sessão sem meta saem; a sessão completa fica.
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	orphan := s.SessionDir(NewID())
	done := Session{ID: NewID(), Created: time.Now()}
	for _, dir := range []string{orphan, s.SessionDir(done.ID)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveSession(done); err != nil {
		t.Fatal(err)
	}
	s.Clean(24 * time.Hour)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("o upload antigo devia ter sido apagado")
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Error("a sessão incompleta devia ter sido apagada")
	}
	if list, _ := s.Sessions(); !slices.ContainsFunc(list, func(v Session) bool { return v.ID == done.ID }) {
		t.Error("a sessão completa tem de ficar")
	}
	if s.Usage() <= 0 {
		t.Error("Usage devia contar o meta.json")
	}
}
