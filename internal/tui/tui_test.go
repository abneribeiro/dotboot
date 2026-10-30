package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/abneribeiro/dotboot/internal/config"
	"github.com/abneribeiro/dotboot/internal/doctor"
	"github.com/abneribeiro/dotboot/internal/jobs"
)

func key(code rune) tea.KeyPressMsg {
	k := tea.KeyPressMsg{Code: code}
	if code >= ' ' && code < 0x7f {
		k.Text = string(code)
	}
	return k
}

var ctrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}

func newTestModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := New(&config.Config{OutputDir: t.TempDir(), TempDir: t.TempDir()}, "v1.2.3")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	return m
}

// view devolve o ecrã sem as cores, para comparar texto.
func view(m *Model) string {
	return ansiStrip(m.View().Content)
}

func ansiStrip(s string) string {
	return lipgloss.NewStyle().Render(stripANSI(s))
}

// stripANSI remove as sequências de escape (o suficiente para os testes).
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestHomeMostraOMenu(t *testing.T) {
	m := newTestModel(t)
	out := view(m)
	for _, want := range []string{"dotboot", "v1.2.3", "Descarregar vídeo", "Extrair áudio",
		"Trabalhos", "Diagnóstico", "Definições", "1-5 atalho", "q sair"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q no menu:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Separar") {
		t.Errorf("a separação de músicas já não existe:\n%s", out)
	}
	if !m.View().AltScreen {
		t.Error("a TUI deve usar o ecrã alternativo")
	}
}

func TestNavegacaoAbreOsFormularios(t *testing.T) {
	tests := []struct {
		keys []tea.KeyPressMsg
		want formKind
	}{
		{[]tea.KeyPressMsg{key(tea.KeyEnter)}, formVideo},
		{[]tea.KeyPressMsg{key(tea.KeyDown), key(tea.KeyEnter)}, formAudio},
		{[]tea.KeyPressMsg{key('5')}, formSettings},
		{[]tea.KeyPressMsg{key(tea.KeyUp), key(tea.KeyEnter)}, formSettings},
	}
	for _, tt := range tests {
		m := newTestModel(t)
		for _, k := range tt.keys {
			m.Update(k)
		}
		if m.screen != screenForm || m.formKind != tt.want || m.form == nil {
			t.Errorf("teclas %v: ecrã %v, formulário %v; esperado o %v", tt.keys, m.screen, m.formKind, tt.want)
		}
		if !strings.Contains(view(m), "esc cancelar") {
			t.Errorf("o formulário devia mostrar as teclas:\n%s", view(m))
		}
		// esc volta ao menu sem criar nada.
		m.Update(key(tea.KeyEscape))
		if m.screen != screenHome || m.form != nil || len(m.queue.jobs) != 0 {
			t.Errorf("esc devia voltar ao menu: ecrã %v", m.screen)
		}
	}
}

// Um pedido inválido reabre o formulário com o erro, sem criar trabalho.
func TestSubmitInvalidoMostraOErro(t *testing.T) {
	m := newTestModel(t)
	m.Update(key('1'))
	m.video.urls = "https://youtu.be/abc"
	m.video.trim, m.video.start, m.video.end = true, "02:00", "01:00"
	m.submit()

	if m.screen != screenForm || !strings.Contains(m.formErr, "anterior ao fim") {
		t.Fatalf("esperava o formulário com o erro; ecrã %v, erro %q", m.screen, m.formErr)
	}
	if len(m.queue.jobs) != 0 {
		t.Error("um pedido inválido não pode criar trabalhos")
	}
	if !strings.Contains(view(m), "anterior ao fim") {
		t.Errorf("o erro devia aparecer no ecrã:\n%s", view(m))
	}
	// Os valores sobrevivem para o utilizador só corrigir o que está mal.
	if m.video.urls != "https://youtu.be/abc" {
		t.Errorf("o URL perdeu-se: %q", m.video.urls)
	}
}

// fakeRun devolve um trabalho controlado pelo teste: espera por release e
// reporta um evento de progresso.
func fakeRun(release <-chan struct{}, err error) runFunc {
	return func(ctx context.Context, report jobs.Reporter) (jobs.Result, error) {
		report(jobs.Event{Stage: jobs.StageDownload, Item: "Vídeo [abc].mp4"})
		report(jobs.Event{Progress: true, Fraction: 0.42, Speed: 1 << 20, ETA: 5 * time.Second, Log: "linha de log"})
		select {
		case <-release:
		case <-ctx.Done():
			return jobs.Result{}, context.Canceled
		}
		return jobs.Result{OutputDir: "/out", Files: []string{"/out/Vídeo [abc].mp4"}}, err
	}
}

// pump entrega à TUI as mensagens dos trabalhos até cond ser verdade.
func pump(t *testing.T, m *Model, cond func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case msg := <-m.queue.events:
			m.Update(msg)
		case <-deadline:
			t.Fatal("as mensagens dos trabalhos não chegaram a tempo")
		}
	}
}

func TestTrabalhoMostraProgressoEResultado(t *testing.T) {
	m := newTestModel(t)
	release := make(chan struct{})
	j := m.queue.add(kindVideo, "https://youtu.be/abc", fakeRun(release, nil))
	m.screen = screenJobs

	pump(t, m, func() bool { return j.fraction == 0.42 })
	out := view(m)
	for _, want := range []string{"Vídeo", "https://youtu.be/abc", "a descarregar", "Vídeo [abc].mp4", "42%", "1.0 MB/s"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q no cartão:\n%s", want, out)
		}
	}

	close(release)
	pump(t, m, func() bool { return j.status == statusDone })
	if !strings.Contains(view(m), "concluído") || !strings.Contains(m.flash, "1 ficheiro(s)") {
		t.Errorf("o trabalho devia aparecer concluído:\n%s\nflash: %q", view(m), m.flash)
	}

	// O detalhe mostra os ficheiros e o log.
	m.Update(key(tea.KeyEnter))
	out = view(m)
	if m.screen != screenDetail || !strings.Contains(out, "Vídeo [abc].mp4") || !strings.Contains(out, "linha de log") {
		t.Errorf("detalhe inesperado:\n%s", out)
	}
}

func TestFilaCorreUmDeCadaVezECancela(t *testing.T) {
	m := newTestModel(t)
	r1, r2 := make(chan struct{}), make(chan struct{})
	a := m.queue.add(kindVideo, "a", fakeRun(r1, nil))
	b := m.queue.add(kindAudio, "b", fakeRun(r2, nil))

	if a.status != statusRunning || b.status != statusQueued {
		t.Fatalf("estados = %v, %v; esperado a correr e na fila", a.status, b.status)
	}

	// Cancelar o que está a correr deixa arrancar o seguinte.
	m.screen, m.jobCursor = screenJobs, 0
	m.Update(key('c'))
	pump(t, m, func() bool { return a.status == statusCanceled && b.status == statusRunning })

	// Repetir um cancelado põe-no de novo na fila.
	m.jobCursor = 0
	m.Update(key('r'))
	if len(m.queue.jobs) != 3 || m.queue.jobs[2].status != statusQueued {
		t.Errorf("r devia repetir o trabalho cancelado: %d trabalhos", len(m.queue.jobs))
	}
	close(r2)
	pump(t, m, func() bool { return b.status == statusDone })
	m.queue.shutdown(time.Second)
}

func TestFalhaAparecaNoCartao(t *testing.T) {
	m := newTestModel(t)
	release := make(chan struct{})
	close(release)
	j := m.queue.add(kindAudio, "song", fakeRun(release, errors.New("ffmpeg falhou: exit status 1")))
	m.screen = screenJobs
	pump(t, m, func() bool { return j.status == statusFailed })
	if out := view(m); !strings.Contains(out, "falhou") || !strings.Contains(out, "ffmpeg falhou") {
		t.Errorf("a falha devia aparecer:\n%s", out)
	}
}

// Sair com trabalhos a correr pede confirmação; o segundo ctrl+c sai.
func TestSairComTrabalhosPedeConfirmacao(t *testing.T) {
	m := newTestModel(t)
	release := make(chan struct{})
	m.queue.add(kindVideo, "a", fakeRun(release, nil))

	if _, cmd := m.Update(ctrlC); cmd != nil || !m.quitArmed {
		t.Fatal("o primeiro ctrl+c só devia avisar")
	}
	if !strings.Contains(view(m), "Carrega outra vez") {
		t.Errorf("faltou o aviso:\n%s", view(m))
	}
	_, cmd := m.Update(ctrlC)
	if cmd == nil {
		t.Fatal("o segundo ctrl+c devia sair")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("o segundo ctrl+c devia devolver tea.Quit")
	}
	m.queue.shutdown(time.Second)

	// Sem trabalhos, sai à primeira.
	m = newTestModel(t)
	if _, cmd := m.Update(key('q')); cmd == nil {
		t.Error("q sem trabalhos devia sair logo")
	}
}

func TestDiagnostico(t *testing.T) {
	m := newTestModel(t)
	m.collect = func(context.Context, *config.Config) []doctor.Check {
		return []doctor.Check{
			{Name: "yt-dlp", Level: doctor.LevelOK, Detail: "2026.10.26"},
			{Name: "JavaScript", Level: doctor.LevelWarn, Detail: "nem deno nem node", Fix: "make deps"},
		}
	}
	_, cmd := m.Update(key('4'))
	if m.screen != screenDoctor || !m.checking {
		t.Fatalf("4 devia abrir o diagnóstico")
	}
	m.Update(cmd())
	out := view(m)
	for _, want := range []string{"2026.10.26", "nem deno nem node", "make deps", "Tudo pronto"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q:\n%s", want, out)
		}
	}
}

func TestDefinicoesGravamEAplicam(t *testing.T) {
	m := newTestModel(t)
	m.Update(key('5'))
	out := filepath.Join(t.TempDir(), "Os Meus Vídeos")
	m.settings = settingsValues{output: out}
	m.submit()

	if m.screen != screenHome || m.cfg.OutputDir != out {
		t.Errorf("definições não aplicadas: ecrã %v, cfg %+v", m.screen, m.cfg)
	}
	data, err := os.ReadFile(config.FilePath())
	if err != nil || !strings.Contains(string(data), "Os Meus Vídeos") {
		t.Errorf("config.env = %q (%v)", data, err)
	}
}

// O ecrã nunca pode passar a largura do terminal, nem num 80x24.
func TestEcraCabeNumTerminalPequeno(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 60, Height: 20}, {Width: 160, Height: 50}} {
		m := newTestModel(t)
		m.Update(size)
		release := make(chan struct{})
		m.queue.add(kindVideo, strings.Repeat("https://youtu.be/muito-longo/", 10), fakeRun(release, nil))
		pump(t, m, func() bool { return m.queue.jobs[0].fraction > 0 })

		for _, scr := range []screen{screenHome, screenJobs, screenDoctor} {
			m.screen = scr
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) > size.Height {
				t.Errorf("%dx%d ecrã %v: %d linhas", size.Width, size.Height, scr, len(lines))
			}
			for _, l := range lines {
				if w := lipgloss.Width(l); w > size.Width {
					t.Errorf("%dx%d ecrã %v: linha com %d colunas: %q", size.Width, size.Height, scr, w, stripANSI(l))
					break
				}
			}
		}
		close(release)
		m.queue.shutdown(time.Second)
	}
}
