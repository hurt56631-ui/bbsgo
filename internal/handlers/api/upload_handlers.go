package api

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/gin-gonic/gin"

	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/common"
	"bbs-go/internal/pkg/ginx"
	"bbs-go/internal/pkg/locales"
	"bbs-go/internal/services"
)

const (
	maxImageDimension   = 16000
	maxImagePixels      = 64_000_000
	multipartOverhead   = 1 << 20
	mimeSniffBytes      = 512
	voiceUploadMaxMB    = 5
	voiceUploadMaxBytes = voiceUploadMaxMB * 1024 * 1024
)

var allowedImageTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/gif":  {},
	"image/webp": {},
}

var voicePreviewHTTPClient = &http.Client{Timeout: 60 * time.Second}

func UploadHandle(ctx *gin.Context) {
	user := common.GetCurrentUser(ctx)
	if err := services.UserService.CheckPostStatus(user); err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}

	ctx.Request.Body = http.MaxBytesReader(
		ctx.Writer,
		ctx.Request.Body,
		constants.UploadMaxBytes+multipartOverhead,
	)

	file, header, err := ctx.Request.FormFile("image")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			ginx.WriteJSON(ctx, ginx.ErrorMessage(locales.Getf("upload.image_too_large", constants.UploadMaxM)))
			return
		}
		ginx.WriteJSON(ctx, err)
		return
	}
	defer file.Close()

	if header.Size <= 0 || header.Size > constants.UploadMaxBytes {
		ginx.WriteJSON(ctx, ginx.ErrorMessage(locales.Getf("upload.image_too_large", constants.UploadMaxM)))
		return
	}

	contentType, width, height, err := validateUploadedImageStream(file)
	if err != nil {
		ginx.WriteJSON(ctx, ginx.ErrorMessage(err.Error()))
		return
	}

	slog.Info("upload image",
		slog.String("filename", header.Filename),
		slog.Int64("size", header.Size),
		slog.String("contentType", contentType),
		slog.Int("width", width),
		slog.Int("height", height),
	)

	// The file is rewound by validateUploadedImageStream, so cloud/local
	// uploaders can consume it directly without a second full-size byte slice.
	url, err := services.UploadService.PutImageStream(file, header.Size, contentType)
	if err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}
	ginx.WriteJSON(ctx, map[string]any{
		"url":         url,
		"contentType": contentType,
		"size":        header.Size,
		"width":       width,
		"height":      height,
	})
}

func UploadVoiceHandle(ctx *gin.Context) {
	user := common.GetCurrentUser(ctx)
	if err := services.UserService.CheckPostStatus(user); err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}

	ctx.Request.Body = http.MaxBytesReader(
		ctx.Writer,
		ctx.Request.Body,
		voiceUploadMaxBytes+multipartOverhead,
	)

	file, header, err := ctx.Request.FormFile("audio")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			ginx.WriteJSON(ctx, ginx.ErrorMessage(locales.Getf("upload.voice_too_large", voiceUploadMaxMB)))
			return
		}
		ginx.WriteJSON(ctx, err)
		return
	}
	defer file.Close()

	if header.Size <= 0 || header.Size > voiceUploadMaxBytes {
		ginx.WriteJSON(ctx, ginx.ErrorMessage(locales.Getf("upload.voice_too_large", voiceUploadMaxMB)))
		return
	}

	contentType, err := validateUploadedVoiceStream(file)
	if err != nil {
		ginx.WriteJSON(ctx, ginx.ErrorMessage(err.Error()))
		return
	}

	slog.Info("upload voice",
		slog.String("filename", header.Filename),
		slog.Int64("size", header.Size),
		slog.String("contentType", contentType),
	)

	voiceURL, err := services.UploadService.PutVoiceStream(file, header.Size, contentType)
	if err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}
	ginx.WriteJSON(ctx, map[string]any{
		// Android's forum voice protocol accepts an absolute HTTP(S) path. Return
		// the canonical forum URL when baseURL is configured so web-created voice
		// comments are portable to the app instead of persisting /res/... paths.
		"url":         absoluteVoicePublicURL(services.SysConfigService.GetBaseURL(), voiceURL),
		"contentType": contentType,
		"size":        header.Size,
	})
}

func absoluteVoicePublicURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") {
		return parsed.String()
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return raw
	}
	reference, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(reference).String()
}

// VoicePreviewHandle proxies legacy Android/TangSeng forum voice objects.
// The browser never needs direct access to the internal TangSeng API host.
func VoicePreviewHandle(ctx *gin.Context) {
	src := strings.TrimSpace(ctx.Query("src"))
	if src == "" {
		ctx.Status(http.StatusBadRequest)
		return
	}
	objectPath, ok := services.NormalizeTangSengForumVoicePath("voice:" + src)
	if !ok {
		ctx.Status(http.StatusBadRequest)
		return
	}
	endpoint, err := tangSengVoicePreviewURL(os.Getenv("BBSGO_TANGSENG_API_URL"), objectPath)
	if err != nil {
		ctx.Status(http.StatusNotFound)
		return
	}
	req, err := http.NewRequestWithContext(ctx.Request.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		ctx.Status(http.StatusBadGateway)
		return
	}
	if rangeHeader := strings.TrimSpace(ctx.GetHeader("Range")); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := voicePreviewHTTPClient.Do(req)
	if err != nil {
		ctx.Status(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	contentType := voicePreviewContentType(resp.Header.Get("Content-Type"), objectPath)
	if contentType != "" {
		ctx.Header("Content-Type", contentType)
	}
	for _, name := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified", "Cache-Control"} {
		if value := resp.Header.Get(name); value != "" {
			ctx.Header(name, value)
		}
	}
	ctx.Header("Content-Disposition", "inline")
	ctx.Status(resp.StatusCode)
	_, _ = io.Copy(ctx.Writer, resp.Body)
}

func voicePreviewContentType(contentType, objectPath string) string {
	contentType = strings.TrimSpace(contentType)
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	fallback := voiceContentTypeFromObjectPath(objectPath)
	if fallback != "" && (mediaType == "" || mediaType == "application/octet-stream" || mediaType == "binary/octet-stream" || !strings.HasPrefix(mediaType, "audio/")) {
		return fallback
	}
	return contentType
}

func voiceContentTypeFromObjectPath(objectPath string) string {
	switch strings.ToLower(path.Ext(strings.TrimSpace(objectPath))) {
	case ".webm":
		return "audio/webm"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".m4a", ".mp4":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
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

func tangSengVoicePreviewURL(base, objectPath string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("invalid TangSeng API URL")
	}
	prefix := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(prefix, "/v1") {
		prefix += "/v1"
	}
	u.Path = prefix + "/file/preview/" + strings.TrimPrefix(objectPath, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func validateUploadedVoiceStream(file io.ReadSeeker) (string, error) {
	if file == nil {
		return "", errors.New(locales.Get("upload.empty_voice"))
	}
	header := make([]byte, 64)
	n, readErr := io.ReadFull(file, header)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", errors.New(locales.Get("upload.invalid_voice_data"))
	}
	if n == 0 {
		return "", errors.New(locales.Get("upload.empty_voice"))
	}
	detected := detectVoiceContentType(header[:n])
	if detected == "" {
		return "", errors.New(locales.Get("upload.unsupported_voice_format"))
	}
	// The file signature is authoritative. Browsers occasionally report a
	// generic or container MIME that differs from the actual MediaRecorder
	// output, so rejecting on multipart Content-Type would create false
	// negatives without adding security.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", errors.New(locales.Get("upload.invalid_voice_data"))
	}
	return detected, nil
}

func detectVoiceContentType(header []byte) string {
	if len(header) >= 4 && bytes.Equal(header[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		return "audio/webm"
	}
	if len(header) >= 4 && bytes.Equal(header[:4], []byte("OggS")) {
		return "audio/ogg"
	}
	if len(header) >= 12 && bytes.Equal(header[4:8], []byte("ftyp")) {
		return "audio/mp4"
	}
	if len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WAVE")) {
		return "audio/wav"
	}
	if len(header) >= 3 && bytes.Equal(header[:3], []byte("ID3")) {
		return "audio/mpeg"
	}
	// ADTS AAC sync word. Check this before generic MPEG frame sync because
	// both start with 0xff and otherwise AAC can be misclassified as MP3.
	if len(header) >= 2 && header[0] == 0xff && header[1]&0xf6 == 0xf0 {
		return "audio/aac"
	}
	if len(header) >= 2 && header[0] == 0xff && header[1]&0xe0 == 0xe0 {
		return "audio/mpeg"
	}
	return ""
}

func validateUploadedImageStream(file io.ReadSeeker) (contentType string, width, height int, err error) {
	if file == nil {
		return "", 0, 0, errors.New(locales.Get("upload.empty_image"))
	}

	header := make([]byte, mimeSniffBytes)
	n, readErr := io.ReadFull(file, header)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", 0, 0, errors.New(locales.Get("upload.invalid_image_data"))
	}
	if n == 0 {
		return "", 0, 0, errors.New(locales.Get("upload.empty_image"))
	}
	contentType = http.DetectContentType(header[:n])
	if _, ok := allowedImageTypes[contentType]; !ok {
		return "", 0, 0, errors.New(locales.Get("upload.unsupported_image_format"))
	}

	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return "", 0, 0, errors.New(locales.Get("upload.invalid_image_data"))
	}
	cfg, format, decodeErr := image.DecodeConfig(io.LimitReader(file, constants.UploadMaxBytes))
	if decodeErr != nil {
		return "", 0, 0, errors.New(locales.Get("upload.invalid_image_data"))
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxImageDimension || cfg.Height > maxImageDimension {
		return "", 0, 0, errors.New(locales.Get("upload.invalid_image_dimensions"))
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return "", 0, 0, errors.New(locales.Get("upload.image_dimensions_too_large"))
	}
	if !formatMatchesContentType(format, contentType) {
		return "", 0, 0, errors.New(locales.Get("upload.image_format_mismatch"))
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return "", 0, 0, errors.New(locales.Get("upload.invalid_image_data"))
	}
	return contentType, cfg.Width, cfg.Height, nil
}

func formatMatchesContentType(format, contentType string) bool {
	switch format {
	case "jpeg":
		return contentType == "image/jpeg"
	case "png":
		return contentType == "image/png"
	case "gif":
		return contentType == "image/gif"
	case "webp":
		return contentType == "image/webp"
	default:
		return false
	}
}
