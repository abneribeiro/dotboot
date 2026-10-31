package downloader

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
)

func TestMP4Compatible(t *testing.T) {
	tests := []struct {
		name   string
		ext    string
		vcodec string
		acodec string
		pixFmt string
		want   bool
	}{
		{"target: 8-bit h264 + aac in mp4", ".mp4", "h264", "aac", "yuv420p", true},
		{"full range plays too", ".mp4", "h264", "aac", "yuvj420p", true},
		{"silent video", ".mp4", "h264", "", "yuv420p", true},
		{"uppercase extension", ".MP4", "h264", "aac", "yuv420p", true},

		{"right codecs, wrong container", ".webm", "h264", "aac", "yuv420p", false},
		{"mkv from an incompatible merge", ".mkv", "h264", "aac", "yuv420p", false},
		{"youtube's case: av1 + opus in mp4", ".mp4", "av1", "opus", "yuv420p", false},
		{"vp9 in mp4", ".mp4", "vp9", "aac", "yuv420p", false},
		{"h264 with opus audio", ".mp4", "h264", "opus", "yuv420p", false},
		{"10-bit h264: iPhones cannot decode High10", ".mp4", "h264", "aac", "yuv420p10le", false},
		{"hevc is not universal", ".mp4", "hevc", "aac", "yuv420p", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mp4Compatible(tt.ext, tt.vcodec, tt.acodec, tt.pixFmt); got != tt.want {
				t.Errorf("mp4Compatible(%q, %q, %q, %q) = %v, want %v",
					tt.ext, tt.vcodec, tt.acodec, tt.pixFmt, got, tt.want)
			}
		})
	}
}

// A remux (everything copied) is the difference between seconds and
// minutes: it is the path for a .webm that already carries H.264/AAC.
func TestCompatArgsRemuxesWithoutReencoding(t *testing.T) {
	got := compatArgs("/tmp/v.webm", "/tmp/v.mp4", "h264", "aac", "yuv420p")
	want := []string{
		"-i", "/tmp/v.webm",
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c:v", "copy",
		"-c:a", "copy",
		"-movflags", "+faststart",
		"-y", "/tmp/v.mp4",
	}
	if !slices.Equal(got, want) {
		t.Errorf("compatArgs (remux) =\n%v\nwant\n%v", got, want)
	}
}

func TestCompatArgsReencodesOnlyTheWrongStream(t *testing.T) {
	tests := []struct {
		name      string
		vcodec    string
		acodec    string
		pixFmt    string
		wantVideo string
		wantAudio string
	}{
		{"audio only", "h264", "opus", "yuv420p", "copy", "aac"},
		{"video only", "av1", "aac", "yuv420p", "libx264", "copy"},
		{"both", "vp9", "opus", "yuv420p", "libx264", "aac"},
		{"10-bit h264 must be re-encoded", "h264", "aac", "yuv420p10le", "libx264", "copy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compatArgs("/tmp/in", "/tmp/out.mp4", tt.vcodec, tt.acodec, tt.pixFmt)
			if v := argValue(got, "-c:v"); v != tt.wantVideo {
				t.Errorf("-c:v = %q, want %q", v, tt.wantVideo)
			}
			if a := argValue(got, "-c:a"); a != tt.wantAudio {
				t.Errorf("-c:a = %q, want %q", a, tt.wantAudio)
			}
			// A re-encode must force 8-bit 4:2:0 (a 10-bit source would become
			// High10, unreadable on iPhones), even dimensions (libx264 rejects
			// odd ones) and the High profile. A remux touches none of this.
			reencode := tt.wantVideo == "libx264"
			for flag, want := range map[string]string{
				"-pix_fmt": mp4PixFmt, "-vf": evenDimensions, "-profile:v": compatProfile,
			} {
				if (argValue(got, flag) == want) != reencode {
					t.Errorf("%s = %q, re-encode = %v: %v", flag, argValue(got, flag), reencode, got)
				}
			}
			if argValue(got, "-movflags") != "+faststart" || got[len(got)-1] != "/tmp/out.mp4" {
				t.Errorf("missing +faststart or the output is not last: %v", got)
			}
		})
	}
}

// argValue returns the value after a flag, or "" when it is missing.
func argValue(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

// box builds an MP4 box with the given type and payload size.
func box(typ string, payload int) []byte {
	b := make([]byte, 8+payload)
	binary.BigEndian.PutUint32(b, uint32(8+payload))
	copy(b[4:8], typ)
	return b
}

func TestMoovFirst(t *testing.T) {
	large := make([]byte, 16) // an mdat with a 64-bit size
	binary.BigEndian.PutUint32(large, 1)
	copy(large[4:8], "mdat")
	binary.BigEndian.PutUint64(large[8:], 16)

	tests := []struct {
		name    string
		file    [][]byte
		want    bool
		wantErr bool
	}{
		{"faststart", [][]byte{box("ftyp", 24), box("moov", 100), box("mdat", 500)}, true, false},
		{"index at the end", [][]byte{box("ftyp", 24), box("free", 0), box("mdat", 500), box("moov", 100)}, false, false},
		{"64-bit mdat", [][]byte{box("ftyp", 24), large, box("moov", 10)}, false, false},
		{"neither moov nor mdat", [][]byte{box("ftyp", 24)}, false, true},
		{"invalid box", [][]byte{box("ftyp", 24), {0, 0, 0, 2, 'x', 'x', 'x', 'x'}}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := moovFirstIn(bytes.NewReader(bytes.Join(tt.file, nil)))
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("moovFirstIn = %v, %v; want %v (error: %v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
