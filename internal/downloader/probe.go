package downloader

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// probe corre o ffprobe e devolve a primeira linha do output.
func probe(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ffprobe falhou: %w", err)
	}
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]), nil
}

// ProbeDuration devolve a duração do ficheiro em segundos.
func ProbeDuration(ctx context.Context, path string) (float64, error) {
	out, err := probe(ctx, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path)
	if err != nil {
		return 0, err
	}
	d, err := strconv.ParseFloat(out, 64)
	if err != nil {
		return 0, fmt.Errorf("duração ilegível do ffprobe: %q", out)
	}
	return d, nil
}

// FFmpegFraction interpreta uma linha do "-progress pipe:1" do ffmpeg e
// devolve a fração já processada de um ficheiro com durationSec segundos.
// Só as linhas out_time_us contam (o out_time_ms do ffmpeg também está, por
// razões históricas, em microssegundos).
func FFmpegFraction(line string, durationSec float64) (float64, bool) {
	value, ok := strings.CutPrefix(strings.TrimSpace(line), "out_time_us=")
	if !ok || durationSec <= 0 {
		return 0, false
	}
	us, err := strconv.ParseFloat(value, 64)
	if err != nil || us < 0 {
		return 0, false
	}
	return min(us/1e6/durationSec, 1), true
}
