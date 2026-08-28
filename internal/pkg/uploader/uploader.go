package uploader

import (
	"io"
	"mime"
	"strings"
	"time"

	"bbs-go/internal/models/dto"
	"bbs-go/internal/pkg/config"

	"github.com/go-resty/resty/v2"
	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/common/digests"
	"github.com/mlogclub/simple/common/strs"
)

var (
	imagePrefix      = "images"
	attachmentPrefix = "attachments"
	voicePrefix      = "voice"
	videoPrefix      = "video"
)

// PutOptions 对象上传时的可选参数；nil 表示不设置。
type PutOptions struct {
	ContentType        string // 如 image/jpeg、application/pdf
	ContentDisposition string // 如 attachment; filename="xxx.pdf"
	ContentLength      int64  // 流式上传时 body 长度（S3 等需此值），<=0 表示未知
}

// Uploader 存储接口：按 key 写入/删除对象，并支持 CopyImage。业务 key 策略仍由上层负责。
type Uploader interface {
	// PutObject 按 key 流式写入；opts.ContentLength 为 body 长度，S3 等需此值。
	PutObject(cfg dto.UploadConfig, key string, body io.Reader, opts *PutOptions) (string, error)
	// DeleteObject 按存储 key 物理删除对象；不存在对象按成功处理。
	DeleteObject(cfg dto.UploadConfig, key string) error
	// CopyImage 从 originUrl 拉取图片并上传（内部使用 GenerateImageKey 生成 key）。
	CopyImage(cfg dto.UploadConfig, originUrl string) (string, error)
}

// ---- Key 生成（与存储实现解耦，由调用方或 CopyImage 组合使用） ----

// GenerateImageKey 按内容 MD5 生成图片 key，需完整数据；用于 CopyImage 等已有字节的场景。
func GenerateImageKey(data []byte, contentType string) string {
	ext := getImageExt(contentType)
	if strs.IsBlank(ext) {
		ext = ".jpg"
	}
	return generateKeyWithPrefix(imagePrefix, digests.MD5Bytes(data), ext)
}

// GenerateImageKeyByContentType 按 Content-Type 生成图片 key（UUID），无需 body，用于流式上传。
func GenerateImageKeyByContentType(contentType string) string {
	ext := getImageExt(contentType)
	if strs.IsBlank(ext) {
		ext = ".jpg"
	}
	return generateKeyWithPrefix(imagePrefix, strs.UUID(), ext)
}

// GenerateAttachmentKey 生成附件 key（UUID + 扩展名）。
func GenerateAttachmentKey(uuid, ext string) string {
	return generateKeyWithPrefix(attachmentPrefix, uuid, ext)
}

// GenerateVoiceKeyByContentType 生成语音消息 key（UUID + 音频扩展名）。
func GenerateVoiceKeyByContentType(contentType string) string {
	ext := getVoiceExt(contentType)
	if strs.IsBlank(ext) {
		ext = ".webm"
	}
	return generateKeyWithPrefix(voicePrefix, strs.UUID(), ext)
}

// GenerateVideoKeyByContentType generates a forum video key. Comment video uploads
// are normalized by Android to MP4/H.264/AAC before they reach the forum API.
func GenerateVideoKeyByContentType(contentType string) string {
	ext := getVideoExt(contentType)
	if strs.IsBlank(ext) {
		ext = ".mp4"
	}
	return generateKeyWithPrefix(videoPrefix, strs.UUID(), ext)
}

// NormalizeImageContentType 空时返回 image/jpeg，便于统一默认。
func NormalizeImageContentType(ct string) string {
	if strs.IsBlank(ct) {
		return "image/jpeg"
	}
	return ct
}

// generateKeyWithPrefix 生成存储 Key
// 非生产环境会在 prefix 前加 "test/" 前缀。
//
//	prefix 为前缀，如 images、attachments
//	filename 为文件名, eg: md5、uuid
//	ext 为扩展名, eg: .jpg、.pdf
func generateKeyWithPrefix(prefix string, filename, ext string) string {
	datePath := dates.Format(time.Now(), "2006/01/02/")
	if strs.IsBlank(prefix) {
		return datePath + filename + ext
	}
	// 非生产环境加 test/ 前缀
	if !config.IsProd() {
		prefix = "test/" + prefix
	}
	cleanPrefix := strings.Trim(strings.TrimSpace(prefix), "/")
	return cleanPrefix + "/" + datePath + filename + ext
}

func getVideoExt(contentType string) string {
	if strs.IsBlank(contentType) {
		return ""
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "video/mp4", "application/mp4":
		return ".mp4"
	default:
		return ""
	}
}

func getVoiceExt(contentType string) string {
	if strs.IsBlank(contentType) {
		return ""
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "audio/webm":
		return ".webm"
	case "audio/ogg", "application/ogg":
		return ".ogg"
	case "audio/mp4", "audio/x-m4a":
		return ".m4a"
	case "audio/aac", "audio/aacp":
		return ".aac"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	default:
		return ""
	}
}

func getImageExt(contentType string) string {
	if strs.IsBlank(contentType) {
		return ""
	}

	// 先解析 Content-Type，去掉参数（如 charset=utf-8）
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "" {
		return ""
	}

	// 处理一些非标准的 MIME 类型
	switch mediaType {
	case "image/jfif":
		return ".jpg"
	case "image/pjpeg":
		return ".jpg"
	default:
		exts, _ := mime.ExtensionsByType(mediaType)
		if len(exts) > 0 {
			// 对于 image/jpeg，优先使用 .jpg 而不是 .jpe
			if mediaType == "image/jpeg" {
				for _, e := range exts {
					if e == ".jpg" {
						return ".jpg"
					}
				}
			}
			return exts[0]
		}
	}
	return ""
}

func download(url string) ([]byte, string, error) {
	rsp, err := resty.New().R().Get(url)
	if err != nil {
		return nil, "", err
	}
	return rsp.Body(), rsp.Header().Get("Content-Type"), nil
}
