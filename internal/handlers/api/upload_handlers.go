package api

import (
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"

	_ "golang.org/x/image/webp"

	"github.com/gin-gonic/gin"

	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/common"
	"bbs-go/internal/pkg/ginx"
	"bbs-go/internal/pkg/locales"
	"bbs-go/internal/services"
)

const (
	maxImageDimension = 16000
	maxImagePixels    = 64_000_000
	multipartOverhead = 1 << 20
	mimeSniffBytes    = 512
)

var allowedImageTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/gif":  {},
	"image/webp": {},
}

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
