package middleware

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// VoiceMediaMiddleware forces audio-safe response headers for forum voice
// objects. Go's default mime table reports .webm as video/webm and .3gp as
// video/3gpp; some browsers and Android players reject those types in an audio
// pipeline even though the files contain audio only.
func VoiceMediaMiddleware(ctx *gin.Context) {
	if ctx.Request.Method != http.MethodGet && ctx.Request.Method != http.MethodHead {
		ctx.Next()
		return
	}
	path := ctx.Request.URL.Path
	if !strings.HasPrefix(path, "/res/uploads/voice/") &&
		!strings.HasPrefix(path, "/res/uploads/test/voice/") {
		ctx.Next()
		return
	}

	if contentType := voiceMediaContentType(path); contentType != "" {
		ctx.Header("Content-Type", contentType)
	}
	ctx.Header("Content-Disposition", "inline")
	ctx.Header("Accept-Ranges", "bytes")
	ctx.Next()
}

func voiceMediaContentType(path string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".webm":
		return "audio/webm"
	case ".m4a", ".mp4":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".amr":
		return "audio/amr"
	case ".3gp":
		return "audio/3gpp"
	default:
		return ""
	}
}
