// Package web é o servidor do dotboot: a API JSON, os eventos em tempo real
// (SSE) e a página (SvelteKit) embutida no binário. Valida os pedidos e
// entrega-os ao motor (internal/jobs); o que fica pronto vai para a
// biblioteca (internal/store).
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/doctor"
	"github.com/abneribeiro/dotboot/internal/jobs"
	"github.com/abneribeiro/dotboot/internal/stems"
	"github.com/abneribeiro/dotboot/internal/store"
)

const (
	// maxUpload é o maior ficheiro de música aceite (um WAV de uma hora tem
	// ~600MB).
	maxUpload = 2 << 30

	// maxJSON limita o corpo dos pedidos JSON.
	maxJSON = 1 << 20

	// cookiesFile é onde fica o ficheiro de cookies enviado nas Definições.
	cookiesFile = "cookies.txt"

	// shutdownTimeout é quanto se espera, ao sair, que os trabalhos
	// cancelados terminem — o yt-dlp e o ffmpeg recebem SIGINT e têm até 10s
	// (WaitDelay) para fechar os ficheiros.
	shutdownTimeout = 15 * time.Second
)

// Server é o servidor web.
type Server struct {
	cfg     *config.Config
	store   *store.Store
	version string
	hub     *hub
	queue   *queue
	log     *slog.Logger
}

// New cria o servidor. O store já tem de estar aberto.
func New(cfg *config.Config, st *store.Store, version string, log *slog.Logger) *Server {
	h := newHub()
	return &Server{cfg: cfg, store: st, version: version, hub: h, queue: newQueue(h), log: log}
}

// Handler devolve as rotas do servidor.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.getInfo)
	mux.HandleFunc("GET /api/presets", s.getPresets)
	mux.HandleFunc("GET /api/doctor", s.getDoctor)
	mux.HandleFunc("GET /api/events", s.serveEvents)

	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("POST /api/jobs", s.createJob)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.cancelJob)
	mux.HandleFunc("POST /api/jobs/{id}/retry", s.retryJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.removeJob)

	mux.HandleFunc("POST /api/uploads", s.upload)

	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.getSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.deleteSession)
	mux.HandleFunc("GET /api/sessions/{id}/peaks", s.getPeaks)
	mux.HandleFunc("GET /api/sessions/{id}/tracks/{stem}", s.getTrack)
	mux.HandleFunc("GET /api/sessions/{id}/export", s.exportSession)

	mux.HandleFunc("GET /api/downloads", s.listDownloads)
	mux.HandleFunc("DELETE /api/downloads/{id}", s.deleteDownload)
	mux.HandleFunc("GET /api/downloads/{id}/all", s.downloadAll)
	mux.HandleFunc("GET /api/downloads/{id}/files/{name}", s.getDownloadFile)

	mux.HandleFunc("POST /api/settings/cookies", s.putCookies)
	mux.HandleFunc("DELETE /api/settings/cookies", s.deleteCookies)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "rota desconhecida")
	})
	mux.Handle("/", staticHandler())
	return s.guard(mux)
}

// Shutdown cancela os trabalhos e espera por eles, e desliga os clientes
// SSE para o http.Server poder fechar.
func (s *Server) Shutdown() {
	s.hub.close()
	s.queue.shutdown(shutdownTimeout)
}

// guard protege o servidor de outras páginas abertas no mesmo browser:
//   - o Host tem de ser um nome local, o que trava o DNS rebinding (um site
//     cujo domínio passa a apontar para 127.0.0.1);
//   - os pedidos que mudam estado não podem vir de outra origem (CSRF), e os
//     com corpo têm de ser JSON ou multipart, que um formulário de outro site
//     não consegue enviar sem pedir licença ao servidor (CORS).
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !localHost(r.Host) {
			writeError(w, http.StatusForbidden, "host não permitido: abre o dotboot por http://localhost")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
				writeError(w, http.StatusForbidden, "pedido de outra origem")
				return
			}
			if r.ContentLength != 0 || r.Header.Get("Content-Type") != "" {
				mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if mt != "application/json" && mt != "multipart/form-data" {
					writeError(w, http.StatusUnsupportedMediaType, "usa JSON ou multipart")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// localHost reporta se o cabeçalho Host aponta para esta máquina.
func localHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // o cliente pode ter desligado
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeStoreError traduz os erros do store.
func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "não encontrado")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("pedido inválido: %w", err)
	}
	return nil
}

// env é o ambiente de um trabalho. Os cookies enviados nas Definições valem
// quando DOTBOOT_COOKIES_FILE não está definido.
func (s *Server) env() jobs.Env {
	env := jobs.EnvFrom(s.cfg)
	if env.CookiesFile == "" {
		if path := filepath.Join(s.store.Root(), cookiesFile); fileExists(path) {
			env.CookiesFile = path
		}
	}
	return env
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// --- informação ---

type infoResponse struct {
	Version     string `json:"version"`
	DataDir     string `json:"dataDir"`
	Usage       int64  `json:"usage"`
	Cookies     bool   `json:"cookies"`
	CookiesEnv  bool   `json:"cookiesEnv"`
	CookiesFile string `json:"cookiesFile,omitempty"`
}

func (s *Server) getInfo(w http.ResponseWriter, _ *http.Request) {
	env := s.env()
	writeJSON(w, http.StatusOK, infoResponse{
		Version:     s.version,
		DataDir:     s.store.Root(),
		Usage:       s.store.Usage(),
		Cookies:     env.CookiesFile != "",
		CookiesEnv:  s.cfg.CookiesFile != "",
		CookiesFile: env.CookiesFile,
	})
}

type presetResponse struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Stems       []string `json:"stems"`
	Speed       float64  `json:"speed"`
	Default     bool     `json:"default"`
}

func (s *Server) getPresets(w http.ResponseWriter, _ *http.Request) {
	var out []presetResponse
	for _, p := range stems.Presets() {
		out = append(out, presetResponse{Name: p.Name, Description: p.Description, Stems: p.Stems,
			Speed: p.Speed, Default: p.Name == stems.DefaultPreset})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getDoctor(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, doctor.Collect(r.Context(), s.cfg))
}

// --- trabalhos ---

// jobRequest é o corpo do POST /api/jobs.
type jobRequest struct {
	Kind string `json:"kind"`
	// URLs para vídeo e áudio; URL ou Upload (o id devolvido pelo POST
	// /api/uploads, com o nome original em UploadName) para stems.
	URLs       []string `json:"urls"`
	URL        string   `json:"url"`
	Upload     string   `json:"upload"`
	UploadName string   `json:"uploadName"`
	Quality    string   `json:"quality"`
	Format     string   `json:"format"`
	Model      string   `json:"model"`
	Start      string   `json:"start"`
	End        string   `json:"end"`
}

func (s *Server) listJobs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.queue.list())
}

type jobDetail struct {
	Job
	Logs []string `json:"logs"`
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	j, logs, ok := s.queue.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "trabalho desconhecido")
		return
	}
	writeJSON(w, http.StatusOK, jobDetail{Job: j, Logs: logs})
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var (
		title string
		run   runFunc
		err   error
	)
	switch req.Kind {
	case kindVideo, kindAudio:
		title, run, err = s.downloadJob(req)
	case kindStems:
		title, run, err = s.stemsJob(req)
	default:
		err = fmt.Errorf("tipo de trabalho desconhecido: %q", req.Kind)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	j, err := s.queue.add(req.Kind, title, run)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, j)
}

// cleanURLs tira linhas vazias e repetidas.
func cleanURLs(urls []string) []string {
	var out []string
	for _, u := range urls {
		if u = strings.TrimSpace(u); u != "" && !slices.Contains(out, u) {
			out = append(out, u)
		}
	}
	return out
}

// downloadJob valida um pedido de vídeo ou de áudio. A pasta do download é
// fixada aqui, e não em cada execução: repetir um trabalho que falhou retoma
// os .part e salta o que já está no archive.
func (s *Server) downloadJob(req jobRequest) (string, runFunc, error) {
	format := req.Format
	if req.Kind == kindAudio && format == "" {
		format = jobs.FormatMP3
	}
	if audioFormat := slices.Contains(jobs.AudioFormats, format); format != "" && (req.Kind == kindAudio) != audioFormat {
		return "", nil, fmt.Errorf("o formato %q não serve para %s", format, req.Kind)
	}
	id := store.NewID()
	dir := s.store.DownloadDir(id)
	d, err := jobs.NewDownload(jobs.DownloadInput{
		URLs: cleanURLs(req.URLs), OutputDir: dir, Quality: req.Quality, Format: format,
		Start: strings.TrimSpace(req.Start), End: strings.TrimSpace(req.End),
	})
	if err != nil {
		return "", nil, err
	}
	title := d.URLs[0]
	if len(d.URLs) > 1 {
		title += fmt.Sprintf(" (+%d)", len(d.URLs)-1)
	}

	run := func(ctx context.Context, report jobs.Reporter) (outcome, error) {
		res, err := d.Run(ctx, s.env(), report)
		if len(res.Files) == 0 {
			return outcome{}, err
		}
		if saveErr := s.saveDownload(id, req.Kind, d.URLs, res.Files); saveErr != nil {
			return outcome{}, saveErr
		}
		return outcome{DownloadID: id}, err
	}
	return title, run, nil
}

// trailingID apanha o " [id]" e o recorte que o outputTemplate põe no nome.
var trailingID = regexp.MustCompile(` \[[^\]]+\]( \([0-9.]+-[0-9.]+\))?$`)

// fileTitle é o nome de um ficheiro descarregado sem extensão nem id.
func fileTitle(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return trailingID.ReplaceAllString(base, "$1")
}

// saveDownload regista (ou acrescenta, numa repetição) os ficheiros de um
// download na biblioteca.
func (s *Server) saveDownload(id, kind string, urls, files []string) error {
	d, err := s.store.Download(id)
	if err != nil {
		d = store.Download{ID: id, Kind: kind, URLs: urls, Created: time.Now()}
	}
	for _, f := range files {
		name := filepath.Base(f)
		info, err := os.Stat(f)
		if err != nil || filepath.Dir(f) != s.store.DownloadDir(id) {
			continue
		}
		d.Files = slices.DeleteFunc(d.Files, func(x store.File) bool { return x.Name == name })
		d.Files = append(d.Files, store.File{Name: name, Size: info.Size()})
	}
	if len(d.Files) == 0 {
		return fmt.Errorf("o download não produziu ficheiros")
	}
	d.Title = fileTitle(d.Files[0].Name)
	if len(d.Files) > 1 {
		d.Title += fmt.Sprintf(" (+%d)", len(d.Files)-1)
	}
	return s.store.SaveDownload(d)
}

// stemsJob valida um pedido de separação. Cada execução cria uma sessão
// nova; uma que falhe é apagada.
func (s *Server) stemsJob(req jobRequest) (string, runFunc, error) {
	input, name, source := strings.TrimSpace(req.URL), "", strings.TrimSpace(req.URL)
	if req.Upload != "" {
		if input != "" {
			return "", nil, errors.New("indica um URL ou um ficheiro, não os dois")
		}
		path, err := s.store.FindUpload(req.Upload)
		if err != nil {
			return "", nil, errors.New("o ficheiro enviado já não existe: envia-o de novo")
		}
		input, name, source = path, filepath.Base(req.UploadName), filepath.Base(req.UploadName)
	}
	in := jobs.StemsInput{Input: input, Name: name, Model: req.Model,
		Start: strings.TrimSpace(req.Start), End: strings.TrimSpace(req.End)}
	// Valida já, com uma pasta qualquer: os erros chegam ao formulário.
	in.OutputDir = s.store.SessionDir("validacao")
	st, err := jobs.NewStems(in)
	if err != nil {
		return "", nil, err
	}
	title := source
	section := ""
	if st.Section != nil {
		section = st.Section.Start + "–" + st.Section.End
	}

	run := func(ctx context.Context, report jobs.Reporter) (outcome, error) {
		id := store.NewID()
		run := *st
		run.OutputDir = s.store.SessionDir(id)
		res, err := run.Run(ctx, s.env(), report)
		if err != nil {
			_ = os.RemoveAll(run.OutputDir) // uma sessão a meio não serve para nada
			return outcome{}, err
		}
		sess := store.Session{ID: id, Title: res.Title, Source: source, Model: st.Preset.Name,
			Stems: st.Preset.Stems, Duration: sessionDuration(run.OutputDir), Section: section, Created: time.Now()}
		if err := s.store.SaveSession(sess); err != nil {
			_ = os.RemoveAll(run.OutputDir) // idem
			return outcome{}, err
		}
		if req.Upload != "" {
			_ = os.Remove(input) // o original já não é preciso; um que fique sai no Clean
		}
		return outcome{SessionID: id}, nil
	}
	return title, run, nil
}

// sessionDuration lê a duração da forma de onda gravada pelo trabalho.
func sessionDuration(dir string) float64 {
	data, err := os.ReadFile(filepath.Join(dir, jobs.PeaksFile))
	if err != nil {
		return 0
	}
	var peaks map[string]stems.Peaks
	if json.Unmarshal(data, &peaks) != nil {
		return 0
	}
	var d float64
	for _, p := range peaks {
		d = max(d, p.Duration)
	}
	return d
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	if !s.queue.cancel(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "trabalho desconhecido")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.queue.retry(r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "trabalho desconhecido")
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeJSON(w, http.StatusCreated, j)
	}
}

func (s *Server) removeJob(w http.ResponseWriter, r *http.Request) {
	if !s.queue.remove(r.PathValue("id")) {
		writeError(w, http.StatusConflict, "só se tiram da lista trabalhos que já terminaram")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- uploads ---

type uploadResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// upload recebe um ficheiro de música em streaming (multipart, campo
// "file"), sem o guardar em memória nem num temporário intermédio.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "envia o ficheiro como multipart/form-data")
		return
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			writeError(w, http.StatusBadRequest, `falta o campo "file"`)
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if part.FormName() != "file" {
			continue
		}
		name := filepath.Base(part.FileName())
		id := store.NewID()
		path := s.store.UploadPath(id, name)
		size, err := saveFile(path, part, 0o644)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeError(w, http.StatusRequestEntityTooLarge, "o ficheiro passa de 2 GB")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, uploadResponse{ID: id, Name: name, Size: size})
		return
	}
}

// saveFile copia r para path; um erro a meio não deixa o ficheiro parcial.
func saveFile(path string, r io.Reader, perm os.FileMode) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path) // o parcial não serve
		return 0, err
	}
	return n, nil
}

// --- definições ---

// putCookies guarda o ficheiro de cookies (formato Netscape) na pasta de
// dados, só legível pelo utilizador: são credenciais.
func (s *Server) putCookies(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `falta o ficheiro de cookies (campo "file")`)
		return
	}
	defer file.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	if !strings.Contains(string(head[:n]), "\t") && !strings.Contains(string(head[:n]), "Netscape") {
		writeError(w, http.StatusBadRequest, "não parece um ficheiro de cookies no formato Netscape (cookies.txt)")
		return
	}
	path := filepath.Join(s.store.Root(), cookiesFile)
	if _, err := saveFile(path, io.MultiReader(strings.NewReader(string(head[:n])), file), 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteCookies(w http.ResponseWriter, _ *http.Request) {
	err := os.Remove(filepath.Join(s.store.Root(), cookiesFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
