package downloader

import (
	"os"
	"path/filepath"
	"testing"
)

// write cria um ficheiro com size bytes dentro de dir.
func write(t *testing.T, dir, name string, size int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("não foi possível criar %s: %v", name, err)
	}
	return path
}

func TestFinalFileUsaOCaminhoDoYtDlp(t *testing.T) {
	dir := t.TempDir()
	want := write(t, dir, "Video.mp4", 100)
	pathFile := write(t, dir, "final.path", 0)
	if err := os.WriteFile(pathFile, []byte(want+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := finalFile(dir, pathFile)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != want {
		t.Errorf("finalFile = %q, esperado %q", got, want)
	}
}

// Sem o ficheiro de caminho, a varredura tem de ignorar os fragmentos que o
// yt-dlp deixa: "Video.f399.mp4" ordena ANTES de "Video.mp4", por isso uma
// escolha ingénua enviaria o fragmento em vez do vídeo final.
func TestFinalFileIgnoraFragmentos(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Video.f399.mp4", 9000) // fragmento de vídeo, o maior
	write(t, dir, "Video.f251.webm", 800) // fragmento de áudio
	write(t, dir, "Video.mp4.part", 700)  // download incompleto
	write(t, dir, "Video.mp4.ytdl", 50)   // metadados de resume
	want := write(t, dir, "Video.mp4", 500)

	got, err := finalFile(dir, filepath.Join(dir, "final.path"))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != want {
		t.Errorf("finalFile = %q, esperado %q", got, want)
	}
}

// Um caminho obsoleto no ficheiro não pode fazer o download falhar quando o
// ficheiro real está lá.
func TestFinalFileCaiParaAVarreduraQuandoOCaminhoNaoExiste(t *testing.T) {
	dir := t.TempDir()
	want := write(t, dir, "Video.mkv", 400)
	pathFile := filepath.Join(dir, "final.path")
	if err := os.WriteFile(pathFile, []byte(filepath.Join(dir, "Apagado.mp4")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := finalFile(dir, pathFile)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if got != want {
		t.Errorf("finalFile = %q, esperado %q", got, want)
	}
}

func TestFinalFileSemFicheiros(t *testing.T) {
	dir := t.TempDir()
	if _, err := finalFile(dir, filepath.Join(dir, "final.path")); err == nil {
		t.Error("finalFile devia falhar quando não há nenhum ficheiro utilizável")
	}
}

// Só fragmentos = nenhum ficheiro utilizável (merge interrompido).
func TestFinalFileSoComFragmentos(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Video.f399.mp4", 9000)
	write(t, dir, "Video.mp4.part", 700)

	if _, err := finalFile(dir, filepath.Join(dir, "final.path")); err == nil {
		t.Error("finalFile devia falhar quando só existem ficheiros intermédios")
	}
}
