package web

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/abneribeiro/dotboot/internal/jobs"
	"github.com/abneribeiro/dotboot/internal/stems"
	"github.com/abneribeiro/dotboot/internal/store"
)

// maxGain é o maior volume aceite numa exportação (o fader do estúdio vai
// até 200%).
const maxGain = 2

func (s *Server) listSessions(w http.ResponseWriter, _ *http.Request) {
	list, err := s.store.Sessions()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.Session(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Session(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.DeleteSession(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	s.hub.broadcast(message{Type: "library"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getPeaks(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.Session(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(s.store.SessionDir(sess.ID), jobs.PeaksFile))
}

// getTrack serve uma pista com suporte a Range: o <audio> do browser pede
// pedaços ao saltar para outro ponto da música.
func (s *Server) getTrack(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.Session(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	path, err := s.store.TrackPath(sess, r.PathValue("stem"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeStoreError(w, store.ErrNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "audio/flac")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// exportTrack é uma pista pedida numa exportação.
type exportTrack struct {
	Stem string
	Gain float64
}

// parseTracks lê o parâmetro tracks=baixo:1,bateria:0.5 (o ganho é opcional
// e vale 1). Só aceita as pistas da sessão, sem repetições.
func parseTracks(param string, sess store.Session) ([]exportTrack, error) {
	var out []exportTrack
	for _, item := range strings.Split(param, ",") {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		stem, gainText, hasGain := strings.Cut(item, ":")
		if !slices.Contains(sess.Stems, stem) {
			return nil, fmt.Errorf("a separação não tem a pista %q", stem)
		}
		if slices.ContainsFunc(out, func(t exportTrack) bool { return t.Stem == stem }) {
			return nil, fmt.Errorf("a pista %q está repetida", stem)
		}
		gain := 1.0
		if hasGain {
			g, err := strconv.ParseFloat(gainText, 64)
			if err != nil || g < 0 || g > maxGain {
				return nil, fmt.Errorf("volume inválido para %s: %q (0 a %d)", stem, gainText, maxGain)
			}
			gain = g
		}
		out = append(out, exportTrack{Stem: stem, Gain: gain})
	}
	if len(out) == 0 {
		return nil, errors.New("escolhe pelo menos uma pista")
	}
	return out, nil
}

// safeName tira de um título o que não pode estar num nome de ficheiro em
// nenhum sistema (o Windows é o mais exigente).
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r < 32, strings.ContainsRune(`<>:"/\|?*`, r):
			return '_'
		}
		return r
	}, s)
	return strings.TrimRight(strings.TrimSpace(s), ".")
}

// exportName é o nome sugerido de uma mistura: diz o que leva, ou "todas as
// pistas" quando são todas com o volume original (é a música inteira).
func exportName(sess store.Session, tracks []exportTrack, ext string) string {
	names := make([]string, len(tracks))
	allOriginal := len(tracks) == len(sess.Stems)
	for i, t := range tracks {
		names[i] = t.Stem
		allOriginal = allOriginal && t.Gain == 1
	}
	what := strings.Join(names, " + ")
	if allOriginal {
		what = "todas as pistas"
	}
	return fmt.Sprintf("%s (%s).%s", safeName(sess.Title), what, ext)
}

// setAttachment pede ao browser para guardar a resposta com este nome. O
// mime.FormatMediaType codifica nomes com acentos (RFC 2231).
func setAttachment(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
}

// exportSession é o GET /api/sessions/{id}/export: mode=mix junta as pistas
// num único ficheiro, mode=zip entrega-as em separado num ZIP. format é wav,
// flac ou mp3 (320k).
func (s *Server) exportSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.Session(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	q := r.URL.Query()
	tracks, err := parseTracks(q.Get("tracks"), sess)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	format := q.Get("format")
	if !slices.Contains(stems.Formats, format) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("formato inválido: %q (usa %s)", format,
			strings.Join(stems.Formats, ", ")))
		return
	}

	switch q.Get("mode") {
	case "mix":
		s.exportMix(w, r, sess, tracks, format)
	case "zip":
		s.exportZip(w, r, sess, tracks, format)
	default:
		writeError(w, http.StatusBadRequest, `mode tem de ser "mix" ou "zip"`)
	}
}

// renderTrack escreve uma ou mais pistas num ficheiro temporário no formato
// pedido, já com os volumes.
func (s *Server) renderTrack(r *http.Request, sess store.Session, tracks []exportTrack, format, title string) (string, error) {
	inputs := make([]stems.Track, len(tracks))
	for i, t := range tracks {
		path, err := s.store.TrackPath(sess, t.Stem)
		if err != nil {
			return "", err
		}
		inputs[i] = stems.Track{Path: path, Gain: t.Gain}
	}
	tmp, err := os.CreateTemp(s.cfg.TempDir, "dotboot-export-*."+format)
	if err != nil {
		return "", err
	}
	tmp.Close()
	if err := stems.Render(r.Context(), inputs, tmp.Name(), format, title); err != nil {
		_ = os.Remove(tmp.Name()) // o parcial não serve
		return "", err
	}
	return tmp.Name(), nil
}

func (s *Server) exportMix(w http.ResponseWriter, r *http.Request, sess store.Session, tracks []exportTrack, format string) {
	name := exportName(sess, tracks, format)
	path, err := s.renderTrack(r, sess, tracks, format, strings.TrimSuffix(name, "."+format))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(path)
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	setAttachment(w, name)
	http.ServeContent(w, r, name, time.Now(), f)
}

// exportZip junta as pistas num ZIP, cada uma no seu ficheiro. Uma pista em
// FLAC com o volume original vai tal e qual; as outras são convertidas uma a
// uma. O ZIP não comprime (o áudio já vem comprimido, e o WAV quase não
// ganha), o que o deixa sair em streaming logo desde a primeira pista.
func (s *Server) exportZip(w http.ResponseWriter, r *http.Request, sess store.Session, tracks []exportTrack, format string) {
	title := safeName(sess.Title)
	w.Header().Set("Content-Type", "application/zip")
	setAttachment(w, title+" (pistas).zip")

	zw := zip.NewWriter(w)
	for _, t := range tracks {
		src, err := s.store.TrackPath(sess, t.Stem)
		if err != nil {
			return
		}
		if format != jobs.SessionFormat || t.Gain != 1 {
			if src, err = s.renderTrack(r, sess, []exportTrack{t}, format, sess.Title+" ("+t.Stem+")"); err != nil {
				// Os cabeçalhos já foram enviados: o ZIP fica truncado e o
				// browser dá o download como falhado.
				s.log.Warn("exportação ZIP falhou", "sessao", sess.ID, "pista", t.Stem, "erro", err)
				return
			}
			defer os.Remove(src)
		}
		if err := addToZip(zw, src, fmt.Sprintf("%s - %s.%s", title, t.Stem, format)); err != nil {
			return
		}
	}
	_ = zw.Close() // o cliente pode ter desligado
}

func addToZip(zw *zip.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dst, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, f)
	return err
}

func (s *Server) listDownloads(w http.ResponseWriter, _ *http.Request) {
	list, err := s.store.Downloads()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) deleteDownload(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Download(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.DeleteDownload(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	s.hub.broadcast(message{Type: "library"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getDownloadFile(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.Download(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	path, err := s.store.DownloadFile(d, r.PathValue("name"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if r.URL.Query().Get("inline") == "" {
		setAttachment(w, filepath.Base(path))
	}
	http.ServeFile(w, r, path)
}

// downloadAll entrega um download inteiro: o próprio ficheiro quando é só
// um, ou um ZIP com todos (uma playlist).
func (s *Server) downloadAll(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.Download(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if len(d.Files) == 1 {
		path, err := s.store.DownloadFile(d, d.Files[0].Name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		setAttachment(w, d.Files[0].Name)
		http.ServeFile(w, r, path)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	setAttachment(w, safeName(d.Title)+".zip")
	zw := zip.NewWriter(w)
	for _, f := range d.Files {
		path, err := s.store.DownloadFile(d, f.Name)
		if err != nil {
			return
		}
		if err := addToZip(zw, path, f.Name); err != nil {
			return
		}
	}
	_ = zw.Close() // o cliente pode ter desligado
}
