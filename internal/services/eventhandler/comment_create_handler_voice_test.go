package eventhandler

import "testing"

func TestStripTrailingVoiceMarker(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantText  string
		wantVoice bool
	}{
		{name: "pure voice", content: "voice:/res/uploads/voice/2026/a.webm|8|", wantText: "", wantVoice: true},
		{name: "mixed", content: "hello\nvoice:/res/uploads/voice/2026/a.webm|8|", wantText: "hello", wantVoice: true},
		{name: "trailing blank", content: "hello\nvoice:/res/uploads/voice/2026/a.webm|8|\n", wantText: "hello", wantVoice: true},
		{name: "normal text", content: "hello\nworld", wantText: "hello\nworld", wantVoice: false},
		{name: "empty marker", content: "hello\nvoice:|8|", wantText: "hello\nvoice:|8|", wantVoice: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotVoice := stripTrailingVoiceMarker(tt.content)
			if gotText != tt.wantText || gotVoice != tt.wantVoice {
				t.Fatalf("stripTrailingVoiceMarker(%q) = (%q, %v), want (%q, %v)", tt.content, gotText, gotVoice, tt.wantText, tt.wantVoice)
			}
		})
	}
}
