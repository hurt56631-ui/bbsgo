package services

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/mlogclub/simple/common/strs"
	"github.com/mlogclub/simple/common/urls"
	"github.com/mlogclub/simple/sqls"

	"bbs-go/internal/models/dto"
	"bbs-go/internal/pkg/bbsurls"
	"bbs-go/internal/pkg/respath"
	"bbs-go/internal/pkg/uploader"
)

const (
	forumImageUploadMaxBytes int64 = 10 * 1024 * 1024
	forumVoiceUploadMaxBytes int64 = 5 * 1024 * 1024
	forumVideoUploadMaxBytes int64 = 10 * 1024 * 1024
)

var UploadService = newUploadService()

type uploadService struct {
	uploaderMap map[dto.UploadMethod]uploader.Uploader
	once        sync.Once
}

func newUploadService() *uploadService {
	return &uploadService{
		uploaderMap: make(map[dto.UploadMethod]uploader.Uploader),
	}
}

func (s *uploadService) putObject(key string, body io.Reader, opts *uploader.PutOptions) (string, error) {
	u, err := s.getUploader()
	if err != nil {
		return "", err
	}
	cfg := SysConfigService.GetUploadConfig()
	return u.PutObject(cfg, key, body, opts)
}

// PutObject 按 key 流式上传；opts 可设置 ContentType、ContentDisposition、ContentLength。
func (s *uploadService) PutObject(key string, body io.Reader, opts *uploader.PutOptions) (string, error) {
	return s.putObject(key, body, opts)
}

func (s *uploadService) ObjectURL(key string) string {
	cfg := SysConfigService.GetUploadConfig()
	if strs.IsBlank(string(cfg.EnableUploadMethod)) {
		cfg.EnableUploadMethod = dto.Local
	}

	switch cfg.EnableUploadMethod {
	case dto.AliyunOss:
		return bbsurls.UrlJoin(cfg.AliyunOss.Host, key)
	case dto.TencentCos:
		return fmt.Sprintf("https://%s.cos.%s.myqcloud.com/%s", cfg.TencentCos.Bucket, cfg.TencentCos.Region, key)
	case dto.AwsS3:
		return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.AwsS3.Bucket, cfg.AwsS3.Region, key)
	default:
		return respath.UploadsURLPrefix + key
	}
}

// PutImage 上传图片（已有完整字节）；key 使用内容 MD5，供 CopyImage 等场景。
func (s *uploadService) PutImage(data []byte, contentType string) (string, error) {
	if int64(len(data)) > forumImageUploadMaxBytes {
		return "", fmt.Errorf("image exceeds 10 MB upload limit")
	}
	contentType = uploader.NormalizeImageContentType(contentType)
	key := uploader.GenerateImageKey(data, contentType)
	opts := &uploader.PutOptions{ContentType: contentType, ContentLength: int64(len(data))}
	return s.putObject(key, bytes.NewReader(data), opts)
}

// PutImageStream 流式上传图片；key 使用 UUID，无需先读完整 body。
func (s *uploadService) PutImageStream(body io.Reader, contentLength int64, contentType string) (string, error) {
	if contentLength < 0 {
		return "", fmt.Errorf("image content length is required")
	}
	if contentLength > forumImageUploadMaxBytes {
		return "", fmt.Errorf("image exceeds 10 MB upload limit")
	}
	contentType = uploader.NormalizeImageContentType(contentType)
	key := uploader.GenerateImageKeyByContentType(contentType)
	opts := &uploader.PutOptions{ContentType: contentType, ContentLength: contentLength}
	return s.putObject(key, body, opts)
}

// PutVoiceStream 流式上传网页语音消息。语音采用独立 voice/ 前缀，
// 这样删除评论/帖子时可以沿用论坛现有的对象清理队列。
func (s *uploadService) PutVoiceStream(body io.Reader, contentLength int64, contentType string) (string, error) {
	if contentLength <= 0 {
		return "", fmt.Errorf("voice content length is required")
	}
	if contentLength > forumVoiceUploadMaxBytes {
		return "", fmt.Errorf("voice exceeds 5 MB upload limit")
	}
	key := uploader.GenerateVoiceKeyByContentType(contentType)
	opts := &uploader.PutOptions{ContentType: contentType, ContentLength: contentLength}
	return s.putObject(key, body, opts)
}

// PutVideoStream stores an already client-compressed forum comment video.
// The API accepts only validated MP4 and keeps video objects under their own
// namespace so the existing durable delete queue can clean them independently.
func (s *uploadService) PutVideoStream(body io.Reader, contentLength int64, contentType string) (string, error) {
	if contentLength <= 0 {
		return "", fmt.Errorf("video content length is required")
	}
	if contentLength > forumVideoUploadMaxBytes {
		return "", fmt.Errorf("video exceeds 10 MB upload limit")
	}
	key := uploader.GenerateVideoKeyByContentType(contentType)
	opts := &uploader.PutOptions{ContentType: contentType, ContentLength: contentLength}
	videoURL, err := s.putObject(key, body, opts)
	if err != nil {
		return "", err
	}

	// Upload and comment creation are two HTTP requests. Pre-enqueue a delayed
	// cleanup task so a process death/network failure between them cannot leak a
	// video forever. When the task becomes due, StorageDeleteService checks live
	// topic/comment references first; a successfully attached video is retained.
	cfg := SysConfigService.GetUploadConfig()
	method := cfg.EnableUploadMethod
	if strs.IsBlank(string(method)) {
		method = dto.Local
	}
	target := storageDeleteTarget{Backend: string(method), ObjectKey: key}
	if err := StorageDeleteService.EnqueueTargetsAfter(sqls.DB(), []storageDeleteTarget{target}, 10*time.Minute); err != nil {
		// If durability cannot be established, roll the just-written object back.
		// Returning an error lets the client retry without silently consuming disk.
		if deleteErr := s.DeleteObject(method, key); deleteErr != nil {
			return "", fmt.Errorf("queue video cleanup: %v; rollback delete: %w", err, deleteErr)
		}
		return "", fmt.Errorf("queue video cleanup: %w", err)
	}
	return videoURL, nil
}

// IsOwnedVideoURL accepts only MP4 objects managed by this forum's configured
// Local/OSS/COS/S3 storage. External direct-video URLs must not be smuggled into
// comment imageList, where clients treat them as trusted native video media.
func (s *uploadService) IsOwnedVideoURL(raw string) bool {
	_, key, ok := s.ResolveOwnedObject(raw)
	if !ok {
		return false
	}
	key = strings.Trim(strings.TrimSpace(strings.ReplaceAll(key, "\\", "/")), "/")
	lower := strings.ToLower(key)
	if !(strings.HasPrefix(lower, "video/") || strings.HasPrefix(lower, "test/video/")) {
		return false
	}
	return strings.EqualFold(path.Ext(lower), ".mp4")
}

func (s *uploadService) CopyImage(url string) (string, error) {
	u, err := s.getUploader()
	if err != nil {
		return "", err
	}
	u1 := urls.ParseUrl(url).GetURL()
	u2 := urls.ParseUrl(SysConfigService.GetBaseURL()).GetURL()
	if u1.Host == u2.Host {
		return url, nil
	}
	cfg := SysConfigService.GetUploadConfig()
	return u.CopyImage(cfg, url)
}

func (s *uploadService) DeleteObject(method dto.UploadMethod, key string) error {
	rawKey := strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	if hasParentPathSegment(rawKey) {
		return fmt.Errorf("invalid storage object key")
	}
	key = strings.TrimPrefix(path.Clean("/"+rawKey), "/")
	if key == "" || key == "." || strings.HasPrefix(key, "../") {
		return nil
	}
	u, err := s.getUploaderByMethod(method)
	if err != nil {
		return err
	}
	return u.DeleteObject(SysConfigService.GetUploadConfig(), key)
}

// ResolveOwnedObject converts a forum-owned public URL/path back to the
// storage method + object key. It checks every configured backend, not only the
// currently selected uploader: an installation may switch from local storage
// to OSS/COS/S3 while older posts still reference the previous backend.
// External URLs are deliberately rejected so deleting a forum post can never
// remove a third-party resource merely because it appeared in Markdown/HTML.
func (s *uploadService) ResolveOwnedObject(raw string) (dto.UploadMethod, string, bool) {
	cfg := SysConfigService.GetUploadConfig()
	current := cfg.EnableUploadMethod
	if strs.IsBlank(string(current)) {
		current = dto.Local
	}

	methods := []dto.UploadMethod{current, dto.Local, dto.AliyunOss, dto.TencentCos, dto.AwsS3}
	seen := make(map[dto.UploadMethod]struct{}, len(methods))
	for _, method := range methods {
		if _, ok := seen[method]; ok {
			continue
		}
		seen[method] = struct{}{}
		if !uploadBackendRecognizable(cfg, method) {
			continue
		}
		if method == dto.Local && !isOwnedLocalURL(raw) {
			continue
		}
		if key, ok := resolveOwnedObjectKey(cfg, method, raw); ok {
			return method, key, true
		}
	}
	return current, "", false
}

func hasParentPathSegment(value string) bool {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func isForumManagedObjectKey(key string) bool {
	key = strings.Trim(strings.TrimSpace(strings.ReplaceAll(key, "\\", "/")), "/")
	return strings.HasPrefix(key, "images/") || strings.HasPrefix(key, "attachments/") || strings.HasPrefix(key, "voice/") ||
		strings.HasPrefix(key, "video/") || strings.HasPrefix(key, "test/images/") || strings.HasPrefix(key, "test/attachments/") || strings.HasPrefix(key, "test/voice/") || strings.HasPrefix(key, "test/video/")
}

func uploadBackendRecognizable(cfg dto.UploadConfig, method dto.UploadMethod) bool {
	switch method {
	case dto.Local:
		return true
	case dto.AliyunOss:
		// Host is enough to recognize ownership. Missing credentials should keep
		// the durable delete task retrying instead of silently losing cleanup.
		return !strs.IsBlank(cfg.AliyunOss.Host)
	case dto.TencentCos:
		return !strs.IsBlank(cfg.TencentCos.Bucket) && !strs.IsBlank(cfg.TencentCos.Region)
	case dto.AwsS3:
		return !strs.IsBlank(cfg.AwsS3.Bucket) && !strs.IsBlank(cfg.AwsS3.Region)
	default:
		return false
	}
}

func isOwnedLocalURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Host == "" {
		return strings.Contains(parsed.Path, respath.UploadsURLPrefix) || strings.Contains(raw, respath.UploadsURLPrefix)
	}
	base, err := url.Parse(strings.TrimSpace(SysConfigService.GetBaseURL()))
	return err == nil && base != nil && base.Host != "" && strings.EqualFold(base.Host, parsed.Host)
}

func resolveOwnedObjectKey(cfg dto.UploadConfig, method dto.UploadMethod, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}

	cleanKey := func(value string) (string, bool) {
		value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
		if hasParentPathSegment(value) {
			return "", false
		}
		value = strings.TrimPrefix(path.Clean("/"+value), "/")
		if value == "" || value == "." || strings.HasPrefix(value, "../") {
			return "", false
		}
		if !isForumManagedObjectKey(value) {
			return "", false
		}
		return value, true
	}
	stripHostPrefix := func(base string) (string, bool) {
		baseURL, e := url.Parse(strings.TrimSpace(base))
		if e != nil || baseURL.Host == "" || parsed.Host == "" || !strings.EqualFold(baseURL.Host, parsed.Host) {
			return "", false
		}
		basePath := strings.TrimSuffix(baseURL.Path, "/")
		objectPath := parsed.Path
		if basePath != "" && basePath != "/" {
			if objectPath != basePath && !strings.HasPrefix(objectPath, basePath+"/") {
				return "", false
			}
			objectPath = strings.TrimPrefix(objectPath, basePath)
		}
		return cleanKey(objectPath)
	}

	switch method {
	case dto.Local:
		objectPath := parsed.Path
		if parsed.Host == "" {
			objectPath = raw
		}
		prefix := respath.UploadsURLPrefix
		idx := strings.Index(objectPath, prefix)
		if idx < 0 {
			return "", false
		}
		return cleanKey(objectPath[idx+len(prefix):])
	case dto.AliyunOss:
		return stripHostPrefix(cfg.AliyunOss.Host)
	case dto.TencentCos:
		expected := fmt.Sprintf("%s.cos.%s.myqcloud.com", cfg.TencentCos.Bucket, cfg.TencentCos.Region)
		if parsed.Host == "" || !strings.EqualFold(parsed.Host, expected) {
			return "", false
		}
		return cleanKey(parsed.Path)
	case dto.AwsS3:
		expected := fmt.Sprintf("%s.s3.%s.amazonaws.com", cfg.AwsS3.Bucket, cfg.AwsS3.Region)
		if parsed.Host == "" || !strings.EqualFold(parsed.Host, expected) {
			return "", false
		}
		return cleanKey(parsed.Path)
	default:
		return "", false
	}
}

func (s *uploadService) getUploaderByMethod(method dto.UploadMethod) (uploader.Uploader, error) {
	s.once.Do(func() {
		s.uploaderMap[dto.Local] = &uploader.LocalUploader{}
		s.uploaderMap[dto.AliyunOss] = &uploader.AliyunOssUploader{}
		s.uploaderMap[dto.TencentCos] = &uploader.TencentCosUploader{}
		s.uploaderMap[dto.AwsS3] = &uploader.AwsS3Uploader{}
	})
	if strs.IsBlank(string(method)) {
		method = dto.Local
	}
	u, ok := s.uploaderMap[method]
	if !ok {
		return nil, fmt.Errorf("error: Upload method: %s not found", method)
	}
	return u, nil
}

func (s *uploadService) getUploader() (uploader.Uploader, error) {
	cfg := SysConfigService.GetUploadConfig()
	return s.getUploaderByMethod(cfg.EnableUploadMethod)
}
