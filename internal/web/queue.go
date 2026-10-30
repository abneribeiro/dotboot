package web

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/abneribeiro/dotboot/internal/jobs"
	"github.com/abneribeiro/dotboot/internal/store"
)

// Tipos de trabalho.
const (
	kindVideo = "video"
	kindAudio = "audio"
	kindStems = "stems"
)

// Estados de um trabalho.
const (
	statusQueued   = "queued"
	statusRunning  = "running"
	statusDone     = "done"
	statusFailed   = "failed"
	statusCanceled = "canceled"
)

// maxLogLines limita o log guardado de cada trabalho: uma playlist longa
// produz dezenas de milhares de linhas.
const maxLogLines = 500

// progressInterval é o intervalo mínimo entre duas atualizações de progresso
// enviadas à página: o yt-dlp e o tqdm escrevem várias linhas por segundo.
const progressInterval = 150 * time.Millisecond

// outcome é o que um trabalho deixou na biblioteca.
type outcome struct {
	SessionID  string
	DownloadID string
}

// runFunc é o trabalho em si: um Download.Run ou Stems.Run já configurado,
// mais o registo do resultado no store.
type runFunc func(ctx context.Context, report jobs.Reporter) (outcome, error)

// Job é um trabalho na fila, tal como a página o vê.
type Job struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Status string `json:"status"`

	Stage string `json:"stage"`
	// Fraction entre 0 e 1, ou -1 sem informação (barra indeterminada).
	Fraction float64 `json:"fraction"`
	// Speed em bytes/s e ETA em segundos (-1 = desconhecido).
	Speed float64 `json:"speed"`
	ETA   float64 `json:"eta"`
	Item  string  `json:"item"`
	Index int     `json:"index"`
	Count int     `json:"count"`

	// Error é a primeira linha do erro; Partial marca uma playlist em que
	// só alguns itens falharam (o trabalho conta como concluído).
	Error   string `json:"error,omitempty"`
	Partial bool   `json:"partial,omitempty"`

	SessionID  string `json:"sessionId,omitempty"`
	DownloadID string `json:"downloadId,omitempty"`

	Created  time.Time  `json:"created"`
	Started  *time.Time `json:"started,omitempty"`
	Finished *time.Time `json:"finished,omitempty"`
}

// job é o estado interno: o Job mais o que não vai para a página.
type job struct {
	Job
	logs     []string
	run      runFunc
	cancel   context.CancelFunc
	lastSent time.Time
}

func (j *job) active() bool {
	return j.Status == statusQueued || j.Status == statusRunning
}

// apply incorpora um evento no estado do trabalho e diz se mudou algo além
// do progresso (uma etapa, um item novo), que se envia logo.
func (j *job) apply(e jobs.Event) (important bool) {
	if e.Stage != "" {
		j.Stage = string(e.Stage)
		// Uma etapa nova começa sem progresso conhecido, até chegar o primeiro.
		j.Fraction, j.Speed, j.ETA = -1, 0, -1
		important = true
	}
	if e.Progress {
		j.Fraction, j.Speed, j.ETA = e.Fraction, e.Speed, e.ETA.Seconds()
		if e.ETA < 0 {
			j.ETA = -1
		}
	}
	if e.Item != "" && e.Item != j.Item {
		j.Item = e.Item
		important = true
	}
	if e.Count > 0 && (e.Index != j.Index || e.Count != j.Count) {
		j.Index, j.Count = e.Index, e.Count
		important = true
	}
	if e.Log != "" {
		j.logs = append(j.logs, e.Log)
		if len(j.logs) > maxLogLines {
			j.logs = j.logs[len(j.logs)-maxLogLines:]
		}
	}
	return important
}

// queue corre os trabalhos um de cada vez (a separação de stems e as
// conversões usam o CPU todo, e o yt-dlp em paralelo só incomoda os sites)
// e publica cada mudança no hub.
type queue struct {
	mu   sync.Mutex
	jobs []*job
	hub  *hub
	wg   sync.WaitGroup
	// closed impede trabalhos novos depois do shutdown.
	closed bool
}

func newQueue(h *hub) *queue {
	return &queue{hub: h}
}

// errClosed é um trabalho pedido durante o encerramento.
var errClosed = errors.New("o dotboot está a encerrar")

// add põe um trabalho na fila e arranca-o se não houver nenhum a correr.
func (q *queue) add(kind, title string, run runFunc) (Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return Job{}, errClosed
	}
	j := &job{Job: Job{ID: store.NewID(), Kind: kind, Title: title, Status: statusQueued,
		Fraction: -1, ETA: -1, Created: time.Now()}, run: run}
	q.jobs = append(q.jobs, j)
	q.publish(j)
	q.startNext()
	return j.Job, nil
}

// list devolve uma cópia dos trabalhos, pela ordem de criação.
func (q *queue) list() []Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Job, len(q.jobs))
	for i, j := range q.jobs {
		out[i] = j.Job
	}
	return out
}

// get devolve um trabalho e o seu log.
func (q *queue) get(id string) (Job, []string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	j := q.byID(id)
	if j == nil {
		return Job{}, nil, false
	}
	return j.Job, slices.Clone(j.logs), true
}

func (q *queue) byID(id string) *job {
	for _, j := range q.jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// publish envia o estado do trabalho à página. Chamado com q.mu.
func (q *queue) publish(j *job) {
	j.lastSent = time.Now()
	q.hub.broadcast(message{Type: "job", Job: &j.Job})
}

// startNext arranca o primeiro trabalho em fila, se nenhum estiver a correr.
// Chamado com q.mu.
func (q *queue) startNext() {
	if q.closed || slices.ContainsFunc(q.jobs, func(j *job) bool { return j.Status == statusRunning }) {
		return
	}
	i := slices.IndexFunc(q.jobs, func(j *job) bool { return j.Status == statusQueued })
	if i < 0 {
		return
	}
	j := q.jobs[i]
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	j.Status, j.cancel, j.Started = statusRunning, cancel, &now
	q.publish(j)

	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		defer cancel()
		out, err := j.run(ctx, func(e jobs.Event) { q.event(j, e) })
		q.finish(j, out, err)
	}()
}

// event recebe um evento do trabalho em curso.
func (q *queue) event(j *job, e jobs.Event) {
	q.mu.Lock()
	defer q.mu.Unlock()
	important := j.apply(e)
	if e.Log != "" {
		q.hub.broadcast(message{Type: "log", ID: j.ID, Line: e.Log})
	}
	if important || (e.Progress && time.Since(j.lastSent) >= progressInterval) {
		q.publish(j)
	}
}

// finish regista o fim de um trabalho e arranca o seguinte.
func (q *queue) finish(j *job, out outcome, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	j.Finished = &now
	j.SessionID, j.DownloadID = out.SessionID, out.DownloadID
	j.Error, j.Partial = "", false
	switch {
	case errors.Is(err, context.Canceled):
		j.Status = statusCanceled
	case err != nil && !errors.Is(err, jobs.ErrPartial):
		j.Status, j.Error = statusFailed, firstLine(err.Error())
	default:
		// Um resultado parcial (playlist com um vídeo privado) conta como
		// concluído; o erro fica visível.
		j.Status = statusDone
		if err != nil {
			j.Partial, j.Error = true, firstLine(err.Error())
		}
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		// O erro completo (com a causa do yt-dlp ou do ffmpeg) fica no log.
		j.logs = append(j.logs, "❌ "+err.Error())
		q.hub.broadcast(message{Type: "log", ID: j.ID, Line: "❌ " + err.Error()})
	}
	j.Fraction = -1
	if j.Status == statusDone {
		j.Fraction = 1
		q.hub.broadcast(message{Type: "library"})
	}
	q.publish(j)
	q.startNext()
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// cancel cancela um trabalho: em fila sai logo; a correr recebe SIGINT e
// termina pelo caminho normal.
func (q *queue) cancel(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	j := q.byID(id)
	if j == nil {
		return false
	}
	switch j.Status {
	case statusQueued:
		now := time.Now()
		j.Status, j.Finished = statusCanceled, &now
		q.publish(j)
	case statusRunning:
		j.cancel()
	}
	return true
}

// retry põe de novo na fila um trabalho que falhou ou foi cancelado.
func (q *queue) retry(id string) (Job, error) {
	q.mu.Lock()
	j := q.byID(id)
	q.mu.Unlock()
	if j == nil {
		return Job{}, store.ErrNotFound
	}
	return q.add(j.Kind, j.Title, j.run)
}

// remove tira da lista um trabalho que já terminou.
func (q *queue) remove(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	i := slices.IndexFunc(q.jobs, func(j *job) bool { return j.ID == id })
	if i < 0 || q.jobs[i].active() {
		return false
	}
	q.jobs = slices.Delete(q.jobs, i, i+1)
	q.hub.broadcast(message{Type: "removed", ID: id})
	return true
}

// shutdown cancela tudo e espera (até timeout) que os processos externos
// terminem — sair antes deixava um yt-dlp ou um ffmpeg órfão a correr.
func (q *queue) shutdown(timeout time.Duration) {
	q.mu.Lock()
	q.closed = true
	for _, j := range q.jobs {
		switch j.Status {
		case statusQueued:
			j.Status = statusCanceled
		case statusRunning:
			j.cancel()
		}
	}
	q.mu.Unlock()

	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
