package api

import (
	"bytes"
	"testing"
)

func TestDetectVoiceContentType(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "webm", data: []byte{0x1a, 0x45, 0xdf, 0xa3, 0x00}, want: "audio/webm"},
		{name: "ogg", data: []byte("OggS........"), want: "audio/ogg"},
		{name: "m4a", data: []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}, want: "audio/mp4"},
		{name: "wav", data: []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'A', 'V', 'E'}, want: "audio/wav"},
		{name: "mp3-id3", data: []byte{'I', 'D', '3', 4, 0}, want: "audio/mpeg"},
		{name: "aac-adts", data: []byte{0xff, 0xf1, 0x50, 0x80}, want: "audio/aac"},
		{name: "mp3-frame", data: []byte{0xff, 0xfb, 0x90, 0x64}, want: "audio/mpeg"},
		{name: "unknown", data: []byte("not audio"), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectVoiceContentType(tt.data); got != tt.want {
				t.Fatalf("detectVoiceContentType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateUploadedVoiceStream(t *testing.T) {
	data := append([]byte{0x1a, 0x45, 0xdf, 0xa3}, make([]byte, 80)...)
	reader := bytes.NewReader(data)
	got, err := validateUploadedVoiceStream(reader)
	if err != nil {
		t.Fatalf("validateUploadedVoiceStream() error = %v", err)
	}
	if got != "audio/webm" {
		t.Fatalf("validateUploadedVoiceStream() = %q, want audio/webm", got)
	}
	if pos, _ := reader.Seek(0, 1); pos != 0 {
		t.Fatalf("reader position = %d, want 0", pos)
	}
}

func TestValidateUploadedVoiceStreamAAC(t *testing.T) {
	data := append([]byte{0xff, 0xf1, 0x50, 0x80}, make([]byte, 80)...)
	reader := bytes.NewReader(data)
	got, err := validateUploadedVoiceStream(reader)
	if err != nil {
		t.Fatalf("validateUploadedVoiceStream() error = %v", err)
	}
	if got != "audio/aac" {
		t.Fatalf("validateUploadedVoiceStream() = %q, want audio/aac", got)
	}
}

func TestTangSengVoicePreviewURL(t *testing.T) {
	got, err := tangSengVoicePreviewURL("http://tangseng:8090", "common/forum/u/a.m4a")
	if err != nil {
		t.Fatalf("tangSengVoicePreviewURL() error = %v", err)
	}
	want := "http://tangseng:8090/v1/file/preview/common/forum/u/a.m4a"
	if got != want {
		t.Fatalf("tangSengVoicePreviewURL() = %q, want %q", got, want)
	}
}

func TestVoiceContentTypeFromObjectPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"common/forum/u/a.m4a", "audio/mp4"},
		{"common/forum/u/a.AAC", "audio/aac"},
		{"common/forum/u/a.mp3", "audio/mpeg"},
		{"common/forum/u/a.webm", "audio/webm"},
		{"common/forum/u/a.ogg", "audio/ogg"},
		{"common/forum/u/a.wav", "audio/wav"},
		{"common/forum/u/a.amr", "audio/amr"},
		{"common/forum/u/a.bin", ""},
	}
	for _, tt := range tests {
		if got := voiceContentTypeFromObjectPath(tt.path); got != tt.want {
			t.Fatalf("voiceContentTypeFromObjectPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestVoicePreviewContentType(t *testing.T) {
	tests := []struct {
		upstream string
		path     string
		want     string
	}{
		{"application/octet-stream", "common/forum/u/a.m4a", "audio/mp4"},
		{"video/mp4", "common/forum/u/a.m4a", "audio/mp4"},
		{"application/ogg", "common/forum/u/a.ogg", "audio/ogg"},
		{"audio/x-m4a", "common/forum/u/a.m4a", "audio/x-m4a"},
		{"text/plain", "common/forum/u/a.bin", "text/plain"},
	}
	for _, tt := range tests {
		if got := voicePreviewContentType(tt.upstream, tt.path); got != tt.want {
			t.Fatalf("voicePreviewContentType(%q, %q) = %q, want %q", tt.upstream, tt.path, got, tt.want)
		}
	}
}
