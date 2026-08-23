package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/dto"
	"bbs-go/internal/models/req"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	storageDeleteBackendTangSeng = "TangSengFile"
	storageDeleteBatchSize       = 100
	storageReferenceBatchSize    = 50
	forumMediaDeleteMaxPaths     = 100
)

var (
	StorageDeleteService  = newStorageDeleteService()
	inlineMediaURLPattern = regexp.MustCompile(`(?i)(https?://[^\s"'<>\)\]]+|/res/uploads/[A-Za-z0-9_./%+\-]+)`)
)

type storageDeleteTarget struct {
	Backend   string
	ObjectKey string
}

type storageDeleteService struct {
	httpClient *http.Client
}

func newStorageDeleteService() *storageDeleteService {
	return &storageDeleteService{httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (s *storageDeleteService) EnqueueTargets(tx *gorm.DB, targets []storageDeleteTarget) error {
	if tx == nil || len(targets) == 0 {
		return nil
	}
	now := dates.NowTimestamp()
	seen := make(map[string]struct{}, len(targets))
	rows := make([]models.StorageDeleteTask, 0, len(targets))
	for _, target := range targets {
		target.Backend = strings.TrimSpace(target.Backend)
		target.ObjectKey = strings.TrimSpace(target.ObjectKey)
		if target.Backend == "" || target.ObjectKey == "" {
			continue
		}
		// Backend (32) + object key (700) stays below MySQL/InnoDB's
		// utf8mb4 composite-index byte limit. Forum-generated keys are normally
		// far below this; fail the DB transaction rather than silently truncate a
		// rare oversized key and lose the physical cleanup guarantee.
		if len([]rune(target.ObjectKey)) > 700 {
			return fmt.Errorf("storage object key is too long for durable deletion")
		}
		id := target.Backend + "\x00" + target.ObjectKey
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		rows = append(rows, models.StorageDeleteTask{
			Backend:       target.Backend,
			ObjectKey:     target.ObjectKey,
			AttemptCount:  0,
			NextRetryTime: now,
			CreateTime:    now,
			UpdateTime:    now,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, 100).Error
}

// ProcessPending immediately removes due objects and deletes successful outbox
// rows. Multiple forum instances may run this concurrently: all supported
// storage delete operations are idempotent, and deleting the same outbox row
// twice is harmless.
func (s *storageDeleteService) ProcessPending(limit int) error {
	if limit <= 0 || limit > 500 {
		limit = storageDeleteBatchSize
	}
	now := dates.NowTimestamp()
	var tasks []models.StorageDeleteTask
	if err := sqls.DB().Where("next_retry_time <= ?", now).Order("id ASC").Limit(limit).Find(&tasks).Error; err != nil {
		return err
	}
	// Check surviving references in batches. The old per-object implementation
	// could issue up to eight unindexed instr() scans for every image. On a
	// 10,000-image purge that dominates the actual storage deletes. Batching keeps
	// the same conservative shared-object protection while scanning each source
	// table only a small number of times per worker batch.
	referencedTaskIDs, referenceErr := s.referencedTaskIDs(tasks)
	if referenceErr != nil {
		for i := range tasks {
			task := tasks[i]
			s.recordFailure(&task, referenceErr)
		}
		return referenceErr
	}

	var firstErr error
	tangSengTasks := make([]models.StorageDeleteTask, 0, forumMediaDeleteMaxPaths)
	flushTangSeng := func() {
		if len(tangSengTasks) == 0 {
			return
		}
		paths := make([]string, 0, len(tangSengTasks))
		ids := make([]int64, 0, len(tangSengTasks))
		for _, task := range tangSengTasks {
			paths = append(paths, task.ObjectKey)
			ids = append(ids, task.Id)
		}
		if err := s.deleteTangSengFiles(paths); err != nil {
			for i := range tangSengTasks {
				task := tangSengTasks[i]
				s.recordFailure(&task, err)
			}
			if firstErr == nil {
				firstErr = err
			}
		} else if err := sqls.DB().Delete(&models.StorageDeleteTask{}, "id IN ?", ids).Error; err != nil && firstErr == nil {
			firstErr = err
		}
		tangSengTasks = tangSengTasks[:0]
	}

	for i := range tasks {
		task := tasks[i]
		if _, referenced := referencedTaskIDs[task.Id]; referenced {
			// This object is intentionally shared by another surviving record. Do
			// not keep retrying; if that last record is deleted later it will enqueue
			// the same target again.
			_ = sqls.DB().Delete(&models.StorageDeleteTask{}, "id = ?", task.Id).Error
			continue
		}

		if task.Backend == storageDeleteBackendTangSeng {
			tangSengTasks = append(tangSengTasks, task)
			if len(tangSengTasks) >= forumMediaDeleteMaxPaths {
				flushTangSeng()
			}
			continue
		}

		if err := s.deleteTask(task); err != nil {
			s.recordFailure(&task, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := sqls.DB().Delete(&models.StorageDeleteTask{}, "id = ?", task.Id).Error; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	flushTangSeng()
	return firstErr
}

func (s *storageDeleteService) recordFailure(task *models.StorageDeleteTask, err error) {
	if task == nil || task.Id <= 0 {
		return
	}
	attempt := task.AttemptCount + 1
	delay := time.Minute
	for i := 1; i < attempt && delay < time.Hour; i++ {
		delay *= 2
		if delay > time.Hour {
			delay = time.Hour
		}
	}
	now := dates.NowTimestamp()
	next := now + int64(delay/time.Millisecond)
	message := ""
	if err != nil {
		message = err.Error()
	}
	if len(message) > 4000 {
		message = message[:4000]
	}
	_ = sqls.DB().Model(&models.StorageDeleteTask{}).Where("id = ?", task.Id).Updates(map[string]any{
		"attempt_count":   attempt,
		"last_error":      message,
		"next_retry_time": next,
		"update_time":     now,
	}).Error
	slog.Warn("delete forum storage object failed; queued for retry",
		slog.Int64("taskId", task.Id), slog.String("backend", task.Backend),
		slog.String("objectKey", task.ObjectKey), slog.Any("err", err))
}

func (s *storageDeleteService) deleteTask(task models.StorageDeleteTask) error {
	if task.Backend == storageDeleteBackendTangSeng {
		return s.deleteTangSengFile(task.ObjectKey)
	}
	method := dto.UploadMethod(task.Backend)
	switch method {
	case dto.Local, dto.AliyunOss, dto.TencentCos, dto.AwsS3:
		return UploadService.DeleteObject(method, task.ObjectKey)
	default:
		return fmt.Errorf("unknown storage delete backend %q", task.Backend)
	}
}

func (s *storageDeleteService) deleteTangSengFile(objectPath string) error {
	return s.deleteTangSengFiles([]string{objectPath})
}

func (s *storageDeleteService) deleteTangSengFiles(objectPaths []string) error {
	if len(objectPaths) == 0 {
		return nil
	}
	if len(objectPaths) > 100 {
		return fmt.Errorf("too many TangSeng media delete paths: %d", len(objectPaths))
	}
	base := strings.TrimSpace(os.Getenv("BBSGO_TANGSENG_API_URL"))
	secret := strings.TrimSpace(os.Getenv("BBSGO_TALKAMI_HMAC_SECRET"))
	if base == "" {
		return errors.New("BBSGO_TANGSENG_API_URL is not configured")
	}
	if len(secret) < 32 {
		return errors.New("BBSGO_TALKAMI_HMAC_SECRET is not configured")
	}
	endpoint, err := tangSengForumMediaDeleteURL(base)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"paths": objectPaths})
	if err != nil {
		return err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))

	reqHTTP, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("X-Forum-Timestamp", timestamp)
	reqHTTP.Header.Set("X-Forum-Signature", signature)
	resp, err := s.httpClient.Do(reqHTTP)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if readErr != nil {
		return fmt.Errorf("read TangSeng media delete response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("TangSeng media delete returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil || result.Deleted != len(objectPaths) {
		return fmt.Errorf("TangSeng media delete confirmed %d of %d objects", result.Deleted, len(objectPaths))
	}
	return nil
}

func tangSengForumMediaDeleteURL(base string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("invalid BBSGO_TANGSENG_API_URL")
	}
	p := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(p, "/v1") {
		u.Path = p + "/forum/media/delete"
	} else {
		u.Path = p + "/v1/forum/media/delete"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func (s *storageDeleteService) referencedTaskIDs(tasks []models.StorageDeleteTask) (map[int64]struct{}, error) {
	ret := make(map[int64]struct{})
	if len(tasks) == 0 {
		return ret, nil
	}

	tangSengByNeedle := make(map[string][]int64)
	forumByNeedle := make(map[string][]int64)
	for _, task := range tasks {
		needle := strings.TrimSpace(task.ObjectKey)
		if needle == "" || task.Id <= 0 {
			continue
		}
		if task.Backend == storageDeleteBackendTangSeng {
			tangSengByNeedle[needle] = append(tangSengByNeedle[needle], task.Id)
		} else {
			forumByNeedle[needle] = append(forumByNeedle[needle], task.Id)
		}
	}

	markReferenced := func(byNeedle map[string][]int64, fields []referenceField) error {
		if len(byNeedle) == 0 {
			return nil
		}
		needles := make([]string, 0, len(byNeedle))
		for needle := range byNeedle {
			needles = append(needles, needle)
		}
		referenced, err := findReferenceSubstrings(needles, fields)
		if err != nil {
			return err
		}
		for needle := range referenced {
			for _, id := range byNeedle[needle] {
				ret[id] = struct{}{}
			}
		}
		return nil
	}

	if err := markReferenced(tangSengByNeedle, tangSengStorageReferenceFields()); err != nil {
		return nil, err
	}
	if err := markReferenced(forumByNeedle, forumStorageReferenceFields()); err != nil {
		return nil, err
	}
	return ret, nil
}

func tangSengStorageReferenceFields() []referenceField {
	return []referenceField{{&models.Comment{}, "content", true}}
}

func forumStorageReferenceFields() []referenceField {
	return []referenceField{
		{&models.Topic{}, "image_list", true}, {&models.Topic{}, "content", true}, {&models.Topic{}, "hide_content", true},
		{&models.Comment{}, "image_list", true}, {&models.Comment{}, "content", true},
		{&models.Article{}, "cover", true}, {&models.Article{}, "content", true},
		{&models.Attachment{}, "file_url", true},
		// Generic forum uploads share the same managed storage as profile/config
		// images. Protect these cross-feature references too, otherwise a user
		// could embed an existing avatar/logo in a post and make post deletion
		// remove an object that is still live elsewhere.
		{&models.User{}, "avatar", false}, {&models.User{}, "background_image", false},
		{&models.ThirdUser{}, "avatar", false}, {&models.Category{}, "logo", false},
		{&models.Badge{}, "icon", false},
	}
}

// findReferenceSubstrings returns the object keys that still appear in any live
// content field. It groups up to storageReferenceBatchSize keys into one query
// per field, reducing repeated full/table-range scans without weakening the
// existing conservative substring check.
func findReferenceSubstrings(needles []string, fields []referenceField) (map[string]struct{}, error) {
	ret := make(map[string]struct{})
	unique := make([]string, 0, len(needles))
	seen := make(map[string]struct{}, len(needles))
	for _, needle := range needles {
		needle = strings.TrimSpace(needle)
		if needle == "" {
			continue
		}
		if _, ok := seen[needle]; ok {
			continue
		}
		seen[needle] = struct{}{}
		unique = append(unique, needle)
	}

	for start := 0; start < len(unique); start += storageReferenceBatchSize {
		end := start + storageReferenceBatchSize
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[start:end]
		for _, field := range fields {
			parts := make([]string, 0, len(batch))
			args := make([]interface{}, 0, len(batch))
			remaining := 0
			for _, needle := range batch {
				if _, ok := ret[needle]; ok {
					continue
				}
				parts = append(parts, "instr("+field.field+", ?) > 0")
				args = append(args, needle)
				remaining++
			}
			if remaining == 0 {
				break
			}

			var values []string
			query := sqls.DB().Model(field.model).Where("("+strings.Join(parts, " OR ")+")", args...)
			if field.statusAware {
				query = query.Where("status <> ?", constants.StatusDeleted)
			}
			if err := query.Pluck(field.field, &values).Error; err != nil {
				return nil, err
			}
			for _, value := range values {
				for _, needle := range batch {
					if _, ok := ret[needle]; ok {
						continue
					}
					if strings.Contains(value, needle) {
						ret[needle] = struct{}{}
					}
				}
			}
		}
	}
	return ret, nil
}

func (s *storageDeleteService) isStillReferenced(task models.StorageDeleteTask) (bool, error) {
	needle := strings.TrimSpace(task.ObjectKey)
	if needle == "" {
		return false, nil
	}
	if task.Backend == storageDeleteBackendTangSeng {
		return countReferenceSubstring(needle, tangSengStorageReferenceFields())
	}
	return countReferenceSubstring(needle, forumStorageReferenceFields())
}

type referenceField struct {
	model       interface{}
	field       string
	statusAware bool
}

func countReferenceSubstring(needle string, fields []referenceField) (bool, error) {
	for _, field := range fields {
		var count int64
		// instr() exists in both MySQL and SQLite and avoids LIKE wildcard
		// ambiguities for object keys containing '_' or '%'. Deleted rows do not
		// protect storage: a deleted comment must be allowed to release its own
		// image/voice while a still-visible shared reference remains protected.
		query := sqls.DB().Model(field.model).Where("instr("+field.field+", ?) > 0", needle)
		if field.statusAware {
			query = query.Where("status <> ?", constants.StatusDeleted)
		}
		if err := query.Limit(1).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func collectTopicStorageDeleteTargets(tx *gorm.DB, topic *models.Topic, commentIds []int64) ([]storageDeleteTarget, error) {
	if tx == nil || topic == nil || topic.Id <= 0 {
		return nil, nil
	}
	collector := newStorageTargetCollector()
	collector.addImageList(topic.ImageList)
	collector.addInlineContent(topic.Content)
	collector.addInlineContent(topic.HideContent)

	for start := 0; start < len(commentIds); start += 500 {
		end := start + 500
		if end > len(commentIds) {
			end = len(commentIds)
		}
		var comments []models.Comment
		if err := tx.Select("id, content, image_list").Where("id IN ?", commentIds[start:end]).Find(&comments).Error; err != nil {
			return nil, err
		}
		for _, comment := range comments {
			collector.addImageList(comment.ImageList)
			collector.addVoiceContent(comment.Content)
		}
	}

	// Attachments can be numerous on imported/teaching topics. Scan by primary
	// key instead of loading the complete attachment list into memory.
	lastAttachmentID := ""
	for {
		var attachments []models.Attachment
		query := tx.Select("id, file_url").Where("topic_id = ?", topic.Id)
		if lastAttachmentID != "" {
			query = query.Where("id > ?", lastAttachmentID)
		}
		if err := query.Order("id ASC").Limit(500).Find(&attachments).Error; err != nil {
			return nil, err
		}
		if len(attachments) == 0 {
			break
		}
		for _, attachment := range attachments {
			collector.addForumObjectURL(attachment.FileUrl)
		}
		lastAttachmentID = attachments[len(attachments)-1].Id
		if len(attachments) < 500 {
			break
		}
	}
	return collector.targets(), nil
}

func collectArticleStorageDeleteTargets(tx *gorm.DB, article *models.Article, commentIds []int64) ([]storageDeleteTarget, error) {
	if tx == nil || article == nil || article.Id <= 0 {
		return nil, nil
	}
	collector := newStorageTargetCollector()
	if cover := req.ParseImageDTO(article.Cover); cover != nil {
		collector.addForumObjectURL(cover.Url)
	}
	collector.addInlineContent(article.Content)

	for start := 0; start < len(commentIds); start += 500 {
		end := start + 500
		if end > len(commentIds) {
			end = len(commentIds)
		}
		var comments []models.Comment
		if err := tx.Select("id, content, image_list").Where("id IN ?", commentIds[start:end]).Find(&comments).Error; err != nil {
			return nil, err
		}
		for _, comment := range comments {
			collector.addImageList(comment.ImageList)
			collector.addVoiceContent(comment.Content)
		}
	}
	return collector.targets(), nil
}

func collectCommentStorageDeleteTargets(tx *gorm.DB, comment *models.Comment, includeReplies bool) ([]storageDeleteTarget, error) {
	if tx == nil || comment == nil || comment.Id <= 0 {
		return nil, nil
	}
	collector := newStorageTargetCollector()
	collector.addImageList(comment.ImageList)
	collector.addVoiceContent(comment.Content)
	if includeReplies {
		var cursor int64
		for {
			var replies []models.Comment
			if err := tx.Select("id, content, image_list").Where(
				"entity_type = ? AND entity_id = ? AND id > ?", constants.EntityComment, comment.Id, cursor,
			).Order("id ASC").Limit(500).Find(&replies).Error; err != nil {
				return nil, err
			}
			if len(replies) == 0 {
				break
			}
			for _, reply := range replies {
				collector.addImageList(reply.ImageList)
				collector.addVoiceContent(reply.Content)
			}
			cursor = replies[len(replies)-1].Id
			if len(replies) < 500 {
				break
			}
		}
	}
	return collector.targets(), nil
}

type storageTargetCollector struct {
	items map[string]storageDeleteTarget
}

func newStorageTargetCollector() *storageTargetCollector {
	return &storageTargetCollector{items: make(map[string]storageDeleteTarget)}
}

func (c *storageTargetCollector) addForumObjectURL(raw string) {
	method, key, ok := UploadService.ResolveOwnedObject(raw)
	if !ok {
		return
	}
	target := storageDeleteTarget{Backend: string(method), ObjectKey: key}
	c.items[target.Backend+"\x00"+target.ObjectKey] = target
}

func (c *storageTargetCollector) addImageList(raw string) {
	for _, image := range req.ParseImageList(raw) {
		c.addForumObjectURL(image.Url)
	}
}

func (c *storageTargetCollector) addInlineContent(content string) {
	for _, match := range inlineMediaURLPattern.FindAllString(content, -1) {
		c.addForumObjectURL(strings.TrimRight(match, ".,;:!?"))
	}
}

func (c *storageTargetCollector) addVoiceContent(content string) {
	path, ok := normalizeTangSengForumVoicePath(content)
	if !ok {
		return
	}
	target := storageDeleteTarget{Backend: storageDeleteBackendTangSeng, ObjectKey: path}
	c.items[target.Backend+"\x00"+target.ObjectKey] = target
}

func (c *storageTargetCollector) targets() []storageDeleteTarget {
	result := make([]storageDeleteTarget, 0, len(c.items))
	for _, target := range c.items {
		result = append(result, target)
	}
	return result
}

func normalizeTangSengForumVoicePath(content string) (string, bool) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "voice:") {
		return "", false
	}
	value := strings.TrimSpace(strings.SplitN(strings.TrimPrefix(content, "voice:"), "|", 2)[0])
	if value == "" {
		return "", false
	}
	if u, err := url.Parse(value); err == nil && u.Path != "" {
		// Use only the URL path even for relative preview URLs so query strings
		// such as ?filename=... can never become part of the storage object key.
		value = u.Path
	}
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "/")
	value = strings.TrimPrefix(value, "v1/")
	value = strings.TrimPrefix(value, "file/preview/")
	value = strings.TrimPrefix(value, "file/")
	value = strings.TrimPrefix(value, "/")
	value = strings.ReplaceAll(value, "\\", "/")
	for strings.Contains(value, "//") {
		value = strings.ReplaceAll(value, "//", "/")
	}
	if !strings.HasPrefix(value, "common/forum/") || strings.Contains(value, "../") || strings.HasSuffix(value, "/") {
		return "", false
	}
	return value, true
}
