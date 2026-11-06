package downloader

import "testing"

func TestParseProbe(t *testing.T) {
	tests := []struct {
		name string
		json string
		want mediaInfo
	}{
		{
			name: "video with audio",
			json: `{"streams":[
				{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p","width":1920,"height":1080,
				 "disposition":{"attached_pic":0}},
				{"codec_type":"audio","codec_name":"aac","disposition":{"attached_pic":0}}],
				"format":{"duration":"212.345000"}}`,
			want: mediaInfo{VCodec: "h264", PixFmt: "yuv420p", Width: 1920, Height: 1080, ACodec: "aac",
				Duration: 212.345},
		},
		{
			name: "10-bit VP9 with Opus",
			json: `{"streams":[
				{"codec_type":"video","codec_name":"vp9","pix_fmt":"yuv420p10le","width":3840,"height":2160},
				{"codec_type":"audio","codec_name":"opus"}],"format":{"duration":"10"}}`,
			want: mediaInfo{VCodec: "vp9", PixFmt: "yuv420p10le", Width: 3840, Height: 2160, ACodec: "opus",
				Duration: 10},
		},
		{
			name: "silent video",
			json: `{"streams":[{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p",
				"width":720,"height":1280}],"format":{"duration":"5.0"}}`,
			want: mediaInfo{VCodec: "h264", PixFmt: "yuv420p", Width: 720, Height: 1280, Duration: 5},
		},
		{
			// The cover of an MP3 is an mjpeg "video" stream: it is not video.
			name: "audio with cover art",
			json: `{"streams":[
				{"codec_type":"audio","codec_name":"mp3"},
				{"codec_type":"video","codec_name":"mjpeg","width":600,"height":600,
				 "disposition":{"attached_pic":1}}],"format":{"duration":"180.0"}}`,
			want: mediaInfo{ACodec: "mp3", Duration: 180},
		},
		{
			name: "unknown duration",
			json: `{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"N/A"}}`,
			want: mediaInfo{ACodec: "aac"},
		},
		{
			name: "no streams",
			json: `{}`,
			want: mediaInfo{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProbe([]byte(tt.json))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("parseProbe = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseProbeRejectsGarbage(t *testing.T) {
	if _, err := parseProbe([]byte("h264\nyuv420p")); err == nil {
		t.Error("non-JSON output should be an error")
	}
}
