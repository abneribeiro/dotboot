package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/abneribeiro/dotboot/internal/jobs"
)

// jobKind distingue os tipos de trabalho, para o ícone e o rótulo.
type jobKind int

const (
	kindVideo jobKind = iota
	kindAudio
)

func (k jobKind) label() string {
	switch k {
	case kindAudio:
		return "🎵 Áudio"
	default:
		return "🎬 Vídeo"
	}
}

type jobStatus int

const (
	statusQueued jobStatus = iota
	statusRunning
	statusDone
	statusFailed
	statusCanceled
)

// runFunc é o trabalho em si: um Download.Run já configurado.
type runFunc func(ctx context.Context, report jobs.Reporter) (jobs.Result, error)

// maxLogLines limita o log guardado de cada trabalho: uma playlist longa
// produz dezenas de milhares de linhas.
const maxLogLines = 500

// job é um trabalho na fila da TUI, com o estado que os eventos vão
// atualizando.
type job struct {
	id     int
	kind   jobKind
	title  string
	run    runFunc
	status jobStatus

	stage    jobs.Stage
	fraction float64
	speed    float64
	eta      time.Duration
	item     string
	index    int
	count    int
	logs     []string

	result   jobs.Result
	err      error
	cancel   context.CancelFunc
	started  time.Time
	finished time.Time
}

// apply incorpora um evento no estado do trabalho.
func (j *job) apply(e jobs.Event) {
	if e.Stage != "" {
		j.stage = e.Stage
		// Uma etapa nova começa sem progresso conhecido, até chegar o primeiro.
		j.fraction, j.speed, j.eta = -1, 0, -1
	}
	if e.Progress {
		j.fraction, j.speed, j.eta = e.Fraction, e.Speed, e.ETA
	}
	if e.Item != "" {
		j.item = e.Item
	}
	if e.Count > 0 {
		j.index, j.count = e.Index, e.Count
	}
	if e.Log != "" {
		j.logs = append(j.logs, e.Log)
		if len(j.logs) > maxLogLines {
			j.logs = j.logs[len(j.logs)-maxLogLines:]
		}
	}
}

func (j *job) active() bool {
	return j.status == statusQueued || j.status == statusRunning
}

// errSummary devolve a primeira linha do erro, para caber num cartão.
func (j *job) errSummary() string {
	if j.err == nil {
		return ""
	}
	line, _, _ := strings.Cut(j.err.Error(), "\n")
	return line
}

// Mensagens que os trabalhos enviam à TUI pelo canal de eventos.
type (
	jobEventMsg struct {
		id    int
		event jobs.Event
	}
	jobDoneMsg struct {
		id     int
		result jobs.Result
		err    error
	}
)

// queue gere os trabalhos: corre um de cada vez (as conversões usam o CPU
// todo, e o yt-dlp em paralelo só incomoda os sites) e guarda os restantes
// em fila.
type queue struct {
	jobs   []*job
	nextID int
	events chan tea.Msg
	wg     *sync.WaitGroup
}

func newQueue() *queue {
	return &queue{events: make(chan tea.Msg, 256), wg: &sync.WaitGroup{}}
}

// listen espera pelo próximo evento de um trabalho. O Update volta a
// chamá-lo depois de cada um, para nunca deixar de ouvir.
func (q *queue) listen() tea.Msg {
	return <-q.events
}

// add põe um trabalho na fila e arranca-o se não houver nenhum a correr.
func (q *queue) add(kind jobKind, title string, run runFunc) *job {
	q.nextID++
	j := &job{id: q.nextID, kind: kind, title: title, run: run, fraction: -1, eta: -1}
	q.jobs = append(q.jobs, j)
	q.startNext()
	return j
}

func (q *queue) byID(id int) *job {
	for _, j := range q.jobs {
		if j.id == id {
			return j
		}
	}
	return nil
}

func (q *queue) running() *job {
	for _, j := range q.jobs {
		if j.status == statusRunning {
			return j
		}
	}
	return nil
}

func (q *queue) counts() (running, queued int) {
	for _, j := range q.jobs {
		switch j.status {
		case statusRunning:
			running++
		case statusQueued:
			queued++
		}
	}
	return running, queued
}

// startNext arranca o primeiro trabalho em fila, se nenhum estiver a correr.
func (q *queue) startNext() {
	if q.running() != nil {
		return
	}
	for _, j := range q.jobs {
		if j.status != statusQueued {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		j.status, j.cancel, j.started = statusRunning, cancel, time.Now()
		q.wg.Add(1)
		go func(j *job) {
			defer q.wg.Done()
			defer cancel()
			res, err := j.run(ctx, func(e jobs.Event) {
				q.events <- jobEventMsg{id: j.id, event: e}
			})
			q.events <- jobDoneMsg{id: j.id, result: res, err: err}
		}(j)
		return
	}
}

// finish regista o fim de um trabalho e arranca o seguinte.
func (q *queue) finish(msg jobDoneMsg) {
	j := q.byID(msg.id)
	if j == nil {
		return
	}
	j.result, j.err, j.finished = msg.result, msg.err, time.Now()
	switch {
	case errors.Is(msg.err, context.Canceled):
		j.status = statusCanceled
	case msg.err != nil && !errors.Is(msg.err, jobs.ErrPartial):
		j.status = statusFailed
	default:
		// Um resultado parcial (playlist com um vídeo privado) conta como
		// concluído; o erro fica visível no cartão.
		j.status = statusDone
	}
	q.startNext()
}

// cancel cancela um trabalho: em fila sai logo; a correr recebe SIGINT e
// termina com o jobDoneMsg normal.
func (q *queue) cancel(j *job) {
	switch j.status {
	case statusQueued:
		j.status, j.err, j.finished = statusCanceled, context.Canceled, time.Now()
	case statusRunning:
		j.cancel()
	}
}

// retry põe de novo na fila um trabalho que falhou ou foi cancelado.
func (q *queue) retry(j *job) *job {
	return q.add(j.kind, j.title, j.run)
}

// shutdown cancela tudo e espera (até timeout) que os processos externos
// terminem — sair antes deixava um yt-dlp ou um ffmpeg órfão a correr.
func (q *queue) shutdown(timeout time.Duration) {
	for _, j := range q.jobs {
		if j.active() {
			q.cancel(j)
		}
	}
	// Os trabalhos ainda podem querer enviar eventos: esvazia-se o canal
	// enquanto se espera, para nenhum ficar bloqueado.
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()
	deadline := time.After(timeout)
	for {
		select {
		case <-done:
			return
		case <-q.events:
		case <-deadline:
			return
		}
	}
}
