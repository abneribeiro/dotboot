package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// message é um evento enviado à página por Server-Sent Events.
type message struct {
	// Type: "jobs" (estado completo, ao ligar), "job" (um trabalho mudou),
	// "log" (uma linha do log de ID), "removed" (ID saiu da lista) e
	// "library" (a biblioteca mudou).
	Type string `json:"type"`
	Jobs []Job  `json:"jobs,omitempty"`
	Job  *Job   `json:"job,omitempty"`
	ID   string `json:"id,omitempty"`
	Line string `json:"line,omitempty"`
}

// subscriberBuffer é quanto um cliente pode ficar para trás antes de ser
// desligado.
const subscriberBuffer = 512

// keepAlive mantém a ligação viva através de proxies que fecham ligações
// paradas.
const keepAlive = 20 * time.Second

// hub distribui as mensagens pelos clientes ligados.
type hub struct {
	mu     sync.Mutex
	subs   map[chan []byte]struct{}
	closed bool
}

func newHub() *hub {
	return &hub{subs: make(map[chan []byte]struct{})}
}

// broadcast envia a mensagem a todos, sem nunca bloquear: um cliente que
// não acompanha (o buffer cheio) é desligado — o EventSource do browser volta
// a ligar-se sozinho e recebe o estado completo, por isso nada se perde.
func (h *hub) broadcast(m message) {
	data, err := json.Marshal(m)
	if err != nil {
		return // os tipos de message são sempre serializáveis
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- data:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

func (h *hub) subscribe() (chan []byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, false
	}
	ch := make(chan []byte, subscriberBuffer)
	h.subs[ch] = struct{}{}
	return ch, true
}

func (h *hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}

// close desliga todos os clientes: o http.Server.Shutdown espera pelos
// pedidos ativos, e um stream SSE nunca acabaria sozinho.
func (h *hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
}

// serveEvents é o GET /api/events: começa pelo estado completo da fila e
// segue com cada mudança.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming não suportado", http.StatusInternalServerError)
		return
	}
	ch, ok := s.hub.subscribe()
	if !ok {
		http.Error(w, errClosed.Error(), http.StatusServiceUnavailable)
		return
	}
	defer s.hub.unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	snapshot, _ := json.Marshal(message{Type: "jobs", Jobs: s.queue.list()}) // sempre serializável
	fmt.Fprintf(w, "data: %s\n\n", snapshot)
	flusher.Flush()

	tick := time.NewTicker(keepAlive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
