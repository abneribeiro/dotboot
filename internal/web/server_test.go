package web

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/store"
)

// Os binários falsos seguem o padrão de internal/jobs: scripts sh num PATH
// de teste que imitam o output e os ficheiros das ferramentas reais.

// fakeYtdlp cria o ficheiro do -o e regista-o no --print-to-file.
const fakeYtdlp = `
out=""; print=""; ext=mp4
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    --print-to-file) print="$3"; shift 2 ;;
    --audio-format) ext="$2"; shift ;;
  esac
  shift
done
file=$(echo "$out" | sed "s/%(title)s/Vídeo de teste/; s/%(id)s/abc/; s/%(ext)s/$ext/")
echo "dotboot-progress 512 1024 NA 2048.0 3"
printf 'conteudo' > "$file"
[ -n "$print" ] && echo "$file" >> "$print"
exit 0
`

const fakeFFprobe = `
case "$*" in
  *pix_fmt*) echo yuv420p ;;
  *a:0*) echo aac ;;
  *v:0*) echo h264 ;;
  *duration*) echo 10.0 ;;
esac
`

// fakeSeparator escreve um WAV por pista do modelo 4s.
const fakeSeparator = `
dir=""
while [ $# -gt 0 ]; do
  [ "$1" = "--output_dir" ] && dir="$2"
  shift
done
printf ' 50%%|██   | 2/4\r100%%|█████| 4/4\n' >&2
for s in vozes bateria baixo outros; do printf 'pista' > "$dir/$s.wav"; done
`

// fakeFFmpeg: PCM para a forma de onda quando a saída é "-", e um ficheiro
// com o nome das entradas nos outros casos (para a mistura ser verificável).
const fakeFFmpeg = `for last; do :; done
if [ "$last" = "-" ]; then printf '\000\100\000\300'; exit 0; fi
echo "$*" > "$last"`

func fakeBin(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

type testServer struct {
	*Server
	http  *httptest.Server
	store *store.Store
	// bin é a pasta dos binários falsos, para um teste trocar um deles.
	bin string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	bin := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	fakeBin(t, bin, "yt-dlp", fakeYtdlp)
	fakeBin(t, bin, "ffprobe", fakeFFprobe)
	fakeBin(t, bin, "ffmpeg", fakeFFmpeg)
	fakeBin(t, bin, "audio-separator", fakeSeparator)

	cfg := &config.Config{DataDir: t.TempDir(), TempDir: t.TempDir(), ModelsDir: t.TempDir()}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, st, "teste", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		srv.Shutdown()
		ts.Close()
	})
	return &testServer{Server: srv, http: ts, store: st, bin: bin}
}

func (ts *testServer) do(t *testing.T, method, path string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.http.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (ts *testServer) postJSON(t *testing.T, path string, v any) *http.Response {
	t.Helper()
	data, _ := json.Marshal(v)
	return ts.do(t, http.MethodPost, path, bytes.NewReader(data), "application/json")
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("resposta inválida (%d): %v", resp.StatusCode, err)
	}
	return v
}

// waitJob espera que o trabalho termine e devolve-o.
func (ts *testServer) waitJob(t *testing.T, id string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j, logs, ok := ts.queue.get(id)
		if !ok {
			t.Fatalf("trabalho %s desapareceu", id)
		}
		if j.Status != statusQueued && j.Status != statusRunning {
			if j.Status == statusFailed {
				t.Logf("log:\n%s", strings.Join(logs, "\n"))
			}
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("o trabalho %s não terminou", id)
	return Job{}
}

func TestGuard(t *testing.T) {
	ts := newTestServer(t)
	tests := []struct {
		name   string
		mutate func(*http.Request)
		method string
		ctype  string
		want   int
	}{
		{"GET normal", nil, http.MethodGet, "", http.StatusOK},
		{"Host de outro domínio (DNS rebinding)", func(r *http.Request) { r.Host = "evil.example:8080" },
			http.MethodGet, "", http.StatusForbidden},
		{"POST de outra origem", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
			http.MethodPost, "application/json", http.StatusForbidden},
		{"POST de formulário (text/plain)", nil, http.MethodPost, "text/plain", http.StatusUnsupportedMediaType},
		{"POST da própria página", func(r *http.Request) { r.Header.Set("Origin", "http://"+r.Host) },
			http.MethodPost, "application/json", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/api/info"
			var body io.Reader
			if tt.method == http.MethodPost {
				path, body = "/api/jobs", strings.NewReader(`{"kind":"nada"}`)
			}
			req := httptest.NewRequest(tt.method, path, body)
			req.Host = "127.0.0.1:8080"
			if tt.ctype != "" {
				req.Header.Set("Content-Type", tt.ctype)
			}
			if tt.mutate != nil {
				tt.mutate(req)
			}
			rec := httptest.NewRecorder()
			ts.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, quero %d (%s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestLocalHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost:8080": true, "127.0.0.1:8080": true, "[::1]:8080": true, "dotboot.localhost": true,
		"evil.example": false, "192.168.1.10:8080": false, "localhost.evil.example": false,
	} {
		if got := localHost(host); got != want {
			t.Errorf("localHost(%q) = %v, quero %v", host, got, want)
		}
	}
}

func TestPedidosInvalidos(t *testing.T) {
	ts := newTestServer(t)
	for name, req := range map[string]jobRequest{
		"tipo desconhecido":       {Kind: "pdf"},
		"vídeo sem URL":           {Kind: kindVideo},
		"URL sem esquema":         {Kind: kindVideo, URLs: []string{"youtube.com/x"}},
		"áudio com formato MP4":   {Kind: kindAudio, URLs: []string{"https://a.pt/1"}, Format: "mp4"},
		"vídeo com formato MP3":   {Kind: kindVideo, URLs: []string{"https://a.pt/1"}, Format: "mp3"},
		"stems sem nada":          {Kind: kindStems},
		"stems com URL e upload":  {Kind: kindStems, URL: "https://a.pt/1", Upload: store.NewID()},
		"stems de upload perdido": {Kind: kindStems, Upload: store.NewID()},
		"modelo desconhecido":     {Kind: kindStems, URL: "https://a.pt/1", Model: "12s"},
	} {
		t.Run(name, func(t *testing.T) {
			resp := ts.postJSON(t, "/api/jobs", req)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, quero 400", resp.StatusCode)
			}
			if e := decode[map[string]string](t, resp); e["error"] == "" {
				t.Error("falta a mensagem de erro")
			}
		})
	}
	for _, path := range []string{"/api/sessions/nao-e-hex", "/api/sessions/" + store.NewID(), "/api/downloads/x/all"} {
		if resp := ts.do(t, http.MethodGet, path, nil, ""); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d, quero 404", path, resp.StatusCode)
		}
	}
	if resp := ts.do(t, http.MethodGet, "/api/nada", nil, ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("rota desconhecida da API = %d, quero 404", resp.StatusCode)
	}
}

func TestDownloadDeVideo(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.postJSON(t, "/api/jobs", jobRequest{Kind: kindVideo, URLs: []string{" https://youtu.be/abc ", ""}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	created := decode[Job](t, resp)
	j := ts.waitJob(t, created.ID)
	if j.Status != statusDone || j.DownloadID == "" || j.Fraction != 1 {
		t.Fatalf("trabalho = %+v", j)
	}

	list := decode[[]store.Download](t, ts.do(t, http.MethodGet, "/api/downloads", nil, ""))
	if len(list) != 1 || list[0].Title != "Vídeo de teste" || len(list[0].Files) != 1 || list[0].Kind != kindVideo {
		t.Fatalf("downloads = %+v", list)
	}
	name := list[0].Files[0].Name
	file := ts.do(t, http.MethodGet, "/api/downloads/"+j.DownloadID+"/files/"+name, nil, "")
	body, _ := io.ReadAll(file.Body)
	if file.StatusCode != http.StatusOK || string(body) != "conteudo" {
		t.Errorf("ficheiro = %d %q", file.StatusCode, body)
	}
	if cd := file.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	// O archive e os outros ficheiros internos da pasta não se servem.
	if r := ts.do(t, http.MethodGet, "/api/downloads/"+j.DownloadID+"/files/meta.json", nil, ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("meta.json servido com %d", r.StatusCode)
	}

	// Um só ficheiro: o "descarregar tudo" dá o próprio ficheiro.
	all := ts.do(t, http.MethodGet, "/api/downloads/"+j.DownloadID+"/all", nil, "")
	if body, _ := io.ReadAll(all.Body); string(body) != "conteudo" {
		t.Errorf("/all com um ficheiro = %q", body)
	}
	// Vários (uma repetição que trouxe mais um): um ZIP com todos.
	second := filepath.Join(ts.store.DownloadDir(j.DownloadID), "Outro [def].mp4")
	if err := os.WriteFile(second, []byte("outro"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ts.saveDownload(j.DownloadID, kindVideo, nil, []string{second}); err != nil {
		t.Fatal(err)
	}
	zr := readZip(t, ts.do(t, http.MethodGet, "/api/downloads/"+j.DownloadID+"/all", nil, ""))
	if got := zipNames(zr); !slices.Equal(got, []string{name, "Outro [def].mp4"}) {
		t.Errorf("ZIP = %v", got)
	}
	if d, _ := ts.store.Download(j.DownloadID); d.Title != "Vídeo de teste (+1)" {
		t.Errorf("título com dois ficheiros = %q", d.Title)
	}

	if r := ts.do(t, http.MethodDelete, "/api/downloads/"+j.DownloadID, nil, ""); r.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE = %d", r.StatusCode)
	}
	if list := decode[[]store.Download](t, ts.do(t, http.MethodGet, "/api/downloads", nil, "")); len(list) != 0 {
		t.Errorf("depois de apagar ficou %+v", list)
	}
}

func readZip(t *testing.T, resp *http.Response) *zip.Reader {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("ZIP inválido (%d): %v", resp.StatusCode, err)
	}
	return zr
}

func zipNames(zr *zip.Reader) []string {
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

// upload envia um ficheiro e devolve o id.
func (ts *testServer) upload(t *testing.T, name, content string) uploadResponse {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write([]byte(content))
	_ = mw.Close()
	resp := ts.do(t, http.MethodPost, "/api/uploads", &buf, mw.FormDataContentType())
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload = %d", resp.StatusCode)
	}
	return decode[uploadResponse](t, resp)
}

func TestSeparacaoEEstudio(t *testing.T) {
	ts := newTestServer(t)
	up := ts.upload(t, "A Minha Música.mp3", "mp3")
	if up.Name != "A Minha Música.mp3" || up.Size != 3 {
		t.Fatalf("upload = %+v", up)
	}

	resp := ts.postJSON(t, "/api/jobs", jobRequest{Kind: kindStems, Upload: up.ID, UploadName: up.Name, Model: "4s"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	j := ts.waitJob(t, decode[Job](t, resp).ID)
	if j.Status != statusDone || j.SessionID == "" {
		t.Fatalf("trabalho = %+v", j)
	}
	if _, err := ts.store.FindUpload(up.ID); err == nil {
		t.Error("o upload devia ter sido apagado depois de separado")
	}

	sess := decode[store.Session](t, ts.do(t, http.MethodGet, "/api/sessions/"+j.SessionID, nil, ""))
	if sess.Title != "A Minha Música" || sess.Model != "4s" || !slices.Equal(sess.Stems, []string{"vozes", "bateria", "baixo", "outros"}) {
		t.Fatalf("sessão = %+v", sess)
	}
	if sess.Duration != 0.00025 {
		t.Errorf("duração = %v, quero 2 amostras a 8 kHz", sess.Duration)
	}
	base := "/api/sessions/" + sess.ID

	peaks := decode[map[string]json.RawMessage](t, ts.do(t, http.MethodGet, base+"/peaks", nil, ""))
	if len(peaks) != 4 {
		t.Errorf("peaks com %d pistas", len(peaks))
	}

	// O <audio> pede pedaços ao saltar: o Range tem de funcionar.
	req, _ := http.NewRequest(http.MethodGet, ts.http.URL+base+"/tracks/baixo", nil)
	req.Header.Set("Range", "bytes=0-3")
	track, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer track.Body.Close()
	if track.StatusCode != http.StatusPartialContent || track.Header.Get("Content-Type") != "audio/flac" {
		t.Errorf("pista = %d %s", track.StatusCode, track.Header.Get("Content-Type"))
	}
	if r := ts.do(t, http.MethodGet, base+"/tracks/guitarra", nil, ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("pista que o modelo não tem = %d", r.StatusCode)
	}

	// Baixo + bateria numa só faixa, com o baixo a meio volume.
	mix := ts.do(t, http.MethodGet, base+"/export?mode=mix&format=wav&tracks=baixo:0.5,bateria", nil, "")
	body, _ := io.ReadAll(mix.Body)
	if mix.StatusCode != http.StatusOK {
		t.Fatalf("mix = %d %s", mix.StatusCode, body)
	}
	_, params, _ := mime.ParseMediaType(mix.Header.Get("Content-Disposition"))
	if params["filename"] != "A Minha Música (baixo + bateria).wav" {
		t.Errorf("filename = %q, quero o nome com o que a mistura leva", params["filename"])
	}
	args := string(body)
	if !strings.Contains(args, "baixo.flac") || !strings.Contains(args, "bateria.flac") ||
		!strings.Contains(args, "volume=0.500") || strings.Contains(args, "vozes.flac") {
		t.Errorf("argumentos do ffmpeg = %s", args)
	}

	// Pistas em separado: o FLAC sem mudanças vai tal e qual, o resto passa
	// pelo ffmpeg.
	zr := readZip(t, ts.do(t, http.MethodGet, base+"/export?mode=zip&format=flac&tracks=vozes,baixo:1.5", nil, ""))
	names := zipNames(zr)
	if !slices.Equal(names, []string{"A Minha Música - vozes.flac", "A Minha Música - baixo.flac"}) {
		t.Errorf("ZIP = %v", names)
	}

	for _, q := range []string{
		"mode=mix&format=wav&tracks=",
		"mode=mix&format=ogg&tracks=baixo",
		"mode=mix&format=wav&tracks=guitarra",
		"mode=mix&format=wav&tracks=baixo:9",
		"mode=mix&format=wav&tracks=baixo,baixo",
		"mode=tudo&format=wav&tracks=baixo",
	} {
		if r := ts.do(t, http.MethodGet, base+"/export?"+q, nil, ""); r.StatusCode != http.StatusBadRequest {
			t.Errorf("export?%s = %d, quero 400", q, r.StatusCode)
		}
	}

	if r := ts.do(t, http.MethodDelete, base, nil, ""); r.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE = %d", r.StatusCode)
	}
	if r := ts.do(t, http.MethodGet, base, nil, ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("sessão apagada = %d", r.StatusCode)
	}
}

func TestSeparacaoQueFalhaNaoDeixaSessao(t *testing.T) {
	ts := newTestServer(t)
	fakeBin(t, ts.bin, "audio-separator", `echo "RuntimeError: modelo corrompido" >&2; exit 1`)

	up := ts.upload(t, "musica.wav", "wav")
	resp := ts.postJSON(t, "/api/jobs", jobRequest{Kind: kindStems, Upload: up.ID, UploadName: up.Name, Model: "4s"})
	j := ts.waitJob(t, decode[Job](t, resp).ID)
	if j.Status != statusFailed || !strings.Contains(j.Error, "audio-separator falhou") {
		t.Fatalf("trabalho = %+v", j)
	}
	if list, _ := ts.store.Sessions(); len(list) != 0 {
		t.Errorf("ficou uma sessão: %+v", list)
	}
	entries, _ := os.ReadDir(filepath.Join(ts.store.Root(), "sessoes"))
	if len(entries) != 0 {
		t.Errorf("ficou a pasta da sessão falhada: %v", entries)
	}
	// O upload fica, para se poder repetir.
	if _, err := ts.store.FindUpload(up.ID); err != nil {
		t.Errorf("o upload devia ficar para repetir: %v", err)
	}
	_, logs, _ := ts.queue.get(j.ID)
	if !slices.ContainsFunc(logs, func(l string) bool { return strings.Contains(l, "modelo corrompido") }) {
		t.Errorf("a causa devia estar no log: %v", logs)
	}

	// Repetir com o separador já bom cria a sessão.
	fakeBin(t, ts.bin, "audio-separator", fakeSeparator)
	retry := ts.do(t, http.MethodPost, "/api/jobs/"+j.ID+"/retry", nil, "")
	if retry.StatusCode != http.StatusCreated {
		t.Fatalf("retry = %d", retry.StatusCode)
	}
	if again := ts.waitJob(t, decode[Job](t, retry).ID); again.Status != statusDone || again.SessionID == "" {
		t.Fatalf("repetição = %+v", again)
	}
}

func TestCancelarERemover(t *testing.T) {
	ts := newTestServer(t)
	fakeBin(t, ts.bin, "yt-dlp", `exec sleep 10`)

	first := decode[Job](t, ts.postJSON(t, "/api/jobs", jobRequest{Kind: kindVideo, URLs: []string{"https://a.pt/1"}}))
	second := decode[Job](t, ts.postJSON(t, "/api/jobs", jobRequest{Kind: kindAudio, URLs: []string{"https://a.pt/2"}}))
	// Um de cada vez: o segundo espera.
	if j, _, _ := ts.queue.get(second.ID); j.Status != statusQueued {
		t.Fatalf("o segundo devia estar em fila: %s", j.Status)
	}
	if r := ts.do(t, http.MethodDelete, "/api/jobs/"+first.ID, nil, ""); r.StatusCode != http.StatusConflict {
		t.Errorf("tirar da lista um trabalho a correr = %d, quero 409", r.StatusCode)
	}

	for _, id := range []string{second.ID, first.ID} {
		if r := ts.do(t, http.MethodPost, "/api/jobs/"+id+"/cancel", nil, ""); r.StatusCode != http.StatusNoContent {
			t.Fatalf("cancel = %d", r.StatusCode)
		}
	}
	for _, id := range []string{first.ID, second.ID} {
		if j := ts.waitJob(t, id); j.Status != statusCanceled {
			t.Errorf("%s = %s, quero cancelado", id, j.Status)
		}
	}
	if r := ts.do(t, http.MethodDelete, "/api/jobs/"+first.ID, nil, ""); r.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE = %d", r.StatusCode)
	}
	if list := decode[[]Job](t, ts.do(t, http.MethodGet, "/api/jobs", nil, "")); len(list) != 1 {
		t.Errorf("jobs = %+v", list)
	}
}

func TestEventos(t *testing.T) {
	ts := newTestServer(t)
	resp := ts.do(t, http.MethodGet, "/api/events", nil, "")
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	events := make(chan message, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var m message
				if json.Unmarshal([]byte(data), &m) == nil {
					events <- m
				}
			}
		}
		close(events)
	}()

	if first := <-events; first.Type != "jobs" {
		t.Fatalf("o primeiro evento devia ser o estado completo, foi %q", first.Type)
	}
	created := decode[Job](t, ts.postJSON(t, "/api/jobs", jobRequest{Kind: kindAudio, URLs: []string{"https://a.pt/1"}}))

	var sawRunning, sawDone, sawLibrary bool
	timeout := time.After(10 * time.Second)
	for !(sawDone && sawLibrary) {
		select {
		case m, ok := <-events:
			if !ok {
				t.Fatal("o stream fechou")
			}
			switch {
			case m.Type == "job" && m.Job.ID == created.ID && m.Job.Status == statusRunning:
				sawRunning = true
			case m.Type == "job" && m.Job.ID == created.ID && m.Job.Status == statusDone:
				sawDone = true
			case m.Type == "library":
				sawLibrary = true
			}
		case <-timeout:
			t.Fatalf("faltaram eventos: running=%v done=%v library=%v", sawRunning, sawDone, sawLibrary)
		}
	}
	if !sawRunning {
		t.Error("faltou o evento do trabalho a correr")
	}
}

func TestCookies(t *testing.T) {
	ts := newTestServer(t)
	send := func(content string) int {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", "cookies.txt")
		_, _ = fw.Write([]byte(content))
		_ = mw.Close()
		return ts.do(t, http.MethodPost, "/api/settings/cookies", &buf, mw.FormDataContentType()).StatusCode
	}
	if code := send("isto não são cookies"); code != http.StatusBadRequest {
		t.Errorf("cookies inválidos = %d, quero 400", code)
	}
	if code := send("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSID\tx\n"); code != http.StatusNoContent {
		t.Fatalf("cookies = %d", code)
	}
	info, err := os.Stat(filepath.Join(ts.store.Root(), cookiesFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cookies gravados com %v (%v), quero 0600", info.Mode().Perm(), err)
	}
	if got := decode[infoResponse](t, ts.do(t, http.MethodGet, "/api/info", nil, "")); !got.Cookies || got.CookiesEnv {
		t.Errorf("info = %+v", got)
	}
	if ts.env().CookiesFile == "" {
		t.Error("os trabalhos deviam usar os cookies enviados")
	}
	ts.do(t, http.MethodDelete, "/api/settings/cookies", nil, "")
	if ts.env().CookiesFile != "" {
		t.Error("depois de apagar, os trabalhos não usam cookies")
	}
}

func TestPaginaSemBuild(t *testing.T) {
	ts := newTestServer(t)
	for _, path := range []string{"/", "/estudio/abc"} {
		resp := ts.do(t, http.MethodGet, path, nil, "")
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "dotboot") {
			t.Errorf("%s = %d %q", path, resp.StatusCode, body)
		}
	}
}

func TestFileTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Lil Peep - Falling Down [-jRKsiAOAA8].mp4": "Lil Peep - Falling Down",
		"Vídeo [abc] (01.30-02.00).mp4":             "Vídeo (01.30-02.00)",
		"Sem id.mp3":                                "Sem id",
		"Título [com colchetes] no meio [xyz].mp3":  "Título [com colchetes] no meio",
	} {
		if got := fileTitle(in); got != want {
			t.Errorf("fileTitle(%q) = %q, quero %q", in, got, want)
		}
	}
}

func TestExportName(t *testing.T) {
	sess := store.Session{Title: `AC/DC: "Back"`, Stems: []string{"vozes", "baixo"}}
	if got := exportName(sess, []exportTrack{{"baixo", 1}}, "wav"); got != "AC_DC_ _Back_ (baixo).wav" {
		t.Errorf("exportName = %q", got)
	}
	if got := exportName(sess, []exportTrack{{"vozes", 1}, {"baixo", 1}}, "mp3"); got != "AC_DC_ _Back_ (todas as pistas).mp3" {
		t.Errorf("exportName = %q", got)
	}
	if got := exportName(sess, []exportTrack{{"vozes", 0.5}, {"baixo", 1}}, "mp3"); got != "AC_DC_ _Back_ (vozes + baixo).mp3" {
		t.Errorf("exportName com volumes = %q", got)
	}
}
