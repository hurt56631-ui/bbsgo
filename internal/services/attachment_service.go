package services

import (
	"errors"
	"io"
	"log/slog"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/common/strs"
	"github.com/mlogclub/simple/sqls"

	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/locales"
	"bbs-go/internal/pkg/uploader"
	"bbs-go/internal/repositories"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var AttachmentService = new(attachmentService)

type attachmentService struct{}

func (s *attachmentService) extAllowed(ext string, allowedTypes []string) bool {
	if len(allowedTypes) == 0 {
		return false
	}
	ext = strings.ToLower(ext)
	for _, a := range allowedTypes {
		if strings.ToLower(strings.TrimSpace(a)) == ext {
			return true
		}
	}
	return false
}

// Upload 流式上传附件；content 为数据流，contentLength 为文件大小（用于存储 FileSize 与上传 ContentLength）。
func (s *attachmentService) Upload(userId int64, filename string, content io.Reader, contentLength int64, contentType string, downloadScore int) (*models.Attachment, error) {
	if downloadScore < 0 {
		downloadScore = 0
	}
	cfg := SysConfigService.GetAttachmentConfig()
	if !cfg.Enabled {
		return nil, errors.New(locales.Get("attachment.disabled"))
	}
	if contentLength < 0 {
		return nil, errors.New("attachment size is required")
	}
	if cfg.MaxSizeMB > 0 && contentLength > int64(cfg.MaxSizeMB)*1024*1024 {
		return nil, errors.New("attachment exceeds configured size limit")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if !s.extAllowed(ext, cfg.AllowedTypes) {
		return nil, errors.New(locales.Get("attachment.ext_not_allowed"))
	}
	var (
		attId       = strs.UUID()
		key         = uploader.GenerateAttachmentKey(attId, ext)
		disposition = "attachment; filename=\"" + url.QueryEscape(filepath.Base(filename)) + "\""
	)
	fileUrl, err := UploadService.PutObject(key, content, &uploader.PutOptions{ContentType: contentType, ContentDisposition: disposition, ContentLength: contentLength})
	if err != nil {
		return nil, err
	}
	att := &models.Attachment{
		Id:            attId,
		TopicId:       0,
		UserId:        userId,
		FileName:      filename,
		FileUrl:       fileUrl,
		FileSize:      contentLength,
		FileType:      contentType,
		DownloadScore: downloadScore,
		Status:        constants.StatusOk,
		CreateTime:    dates.NowTimestamp(),
		UpdateTime:    dates.NowTimestamp(),
	}
	if err := repositories.AttachmentRepository.Create(sqls.DB(), att); err != nil {
		// The object was already uploaded. Roll it back immediately. If the
		// provider is temporarily unavailable, persist the same cleanup target in
		// the durable outbox instead of silently leaking the orphan forever.
		if method, objectKey, ok := UploadService.ResolveOwnedObject(fileUrl); ok {
			if deleteErr := UploadService.DeleteObject(method, objectKey); deleteErr != nil {
				target := storageDeleteTarget{Backend: string(method), ObjectKey: objectKey}
				if queueErr := StorageDeleteService.EnqueueTargets(sqls.DB(), []storageDeleteTarget{target}); queueErr != nil {
					slog.Error("failed attachment upload left an object that could not be queued for cleanup",
						slog.String("backend", string(method)), slog.String("objectKey", objectKey),
						slog.Any("deleteErr", deleteErr), slog.Any("queueErr", queueErr))
				} else {
					slog.Warn("failed attachment upload cleanup queued for retry",
						slog.String("backend", string(method)), slog.String("objectKey", objectKey), slog.Any("err", deleteErr))
				}
			}
		}
		return nil, err
	}
	return att, nil
}

// UpdateDownloadScore 更新附件的下载积分（仅附件所属用户可更新）
func (s *attachmentService) UpdateDownloadScore(attachmentId string, userId int64, downloadScore int) (*models.Attachment, error) {
	if strs.IsBlank(attachmentId) {
		return nil, errors.New(locales.Get("attachment.not_found"))
	}
	att := repositories.AttachmentRepository.Get(sqls.DB(), attachmentId)
	if att == nil || att.Status != constants.StatusOk {
		return nil, errors.New(locales.Get("attachment.not_found"))
	}
	if att.UserId != userId {
		return nil, errors.New(locales.Get("attachment.no_permission"))
	}
	if downloadScore < 0 {
		downloadScore = 0
	}
	att.DownloadScore = downloadScore
	att.UpdateTime = dates.NowTimestamp()
	return att, repositories.AttachmentRepository.Updates(sqls.DB(), attachmentId, map[string]any{
		"download_score": downloadScore,
		"update_time":    dates.NowTimestamp(),
	})
}

// Get 根据 ID 获取附件（仅返回存在且正常的）
func (s *attachmentService) Get(id string) *models.Attachment {
	att := repositories.AttachmentRepository.Get(sqls.DB(), id)
	if att == nil || att.Status != constants.StatusOk {
		return nil
	}
	return att
}

// ListByTopicId 按帖子查询正常状态的附件
func (s *attachmentService) ListByTopicId(topicId int64) []models.Attachment {
	return repositories.AttachmentRepository.ListByTopicId(sqls.DB(), topicId)
}

// HasDownloaded 当前用户是否已购买该附件
func (s *attachmentService) HasDownloaded(userId int64, attachmentId string) bool {
	if userId <= 0 || strs.IsBlank(attachmentId) {
		return false
	}
	return repositories.AttachmentDownloadLogRepository.Exists(sqls.DB(), userId, attachmentId)
}

func (s *attachmentService) FindDownloadedAttachmentIds(userId int64, attachmentIds []string) []string {
	if userId <= 0 || len(attachmentIds) == 0 {
		return nil
	}

	filteredIds := make([]string, 0, len(attachmentIds))
	seen := make(map[string]bool, len(attachmentIds))
	for _, attachmentId := range attachmentIds {
		if strs.IsBlank(attachmentId) || seen[attachmentId] {
			continue
		}
		seen[attachmentId] = true
		filteredIds = append(filteredIds, attachmentId)
	}
	if len(filteredIds) == 0 {
		return nil
	}
	return repositories.AttachmentDownloadLogRepository.FindDownloadedAttachmentIds(sqls.DB(), userId, filteredIds)
}

// GetDownloadRedirectUrl 根据附件访问地址生成 302 目标 URL（Local 需拼 baseURL）
func (s *attachmentService) GetDownloadRedirectUrl(att *models.Attachment) string {
	return att.FileUrl
}

// Download 鉴权并返回下载重定向 URL；如需扣积分则在事务内扣费并写入 download_log
func (s *attachmentService) Download(attachmentId string, userId int64) (redirectURL string, err error) {
	if strs.IsBlank(attachmentId) {
		return "", errors.New(locales.Get("attachment.not_found"))
	}
	att := repositories.AttachmentRepository.Get(sqls.DB(), attachmentId)
	if att == nil || att.Status != constants.StatusOk {
		return "", errors.New(locales.Get("attachment.not_found"))
	}
	if att.TopicId <= 0 {
		return "", errors.New(locales.Get("attachment.not_found"))
	}

	topic := repositories.TopicRepository.Get(sqls.DB(), att.TopicId)
	if topic == nil || topic.Status == constants.StatusDeleted {
		return "", errors.New(locales.Get("attachment.not_found"))
	}

	// 已购买：直接放行
	if repositories.AttachmentDownloadLogRepository.Exists(sqls.DB(), userId, attachmentId) {
		redirectURL = s.GetDownloadRedirectUrl(att)
		if strs.IsNotBlank(redirectURL) {
			repositories.AttachmentRepository.IncrDownloadCount(sqls.DB(), att.Id)
		}
		return redirectURL, nil
	}

	// 帖主本人或 0 积分：免费，写入 download_log 便于统计
	if att.UserId == userId || att.DownloadScore <= 0 {
		_ = repositories.AttachmentDownloadLogRepository.Create(sqls.DB(), &models.AttachmentDownloadLog{
			UserId:       userId,
			AttachmentId: attachmentId,
			CreateTime:   dates.NowTimestamp(),
		})
		redirectURL = s.GetDownloadRedirectUrl(att)
		if strs.IsNotBlank(redirectURL) {
			repositories.AttachmentRepository.IncrDownloadCount(sqls.DB(), att.Id)
		}
		return redirectURL, nil
	}

	// 需扣积分：按“帖子根记录 -> 附件 -> 用户/购买记录”的固定顺序加锁。
	// 这样管理员物理删帖不能夹在校验与扣积分之间，避免帖子已经删除后
	// 晚到的下载事务重新插入购买记录并扣掉用户积分。
	err = sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		var lockedTopic models.Topic
		topicQuery := ctx.Tx.Select("id, status").Where("id = ?", att.TopicId)
		if ctx.Tx.Dialector.Name() != "sqlite" {
			topicQuery = topicQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if e := topicQuery.Take(&lockedTopic).Error; e != nil || lockedTopic.Status == constants.StatusDeleted {
			return errors.New(locales.Get("attachment.not_found"))
		}

		var lockedAtt models.Attachment
		attQuery := ctx.Tx.Where("id = ?", attachmentId)
		if ctx.Tx.Dialector.Name() != "sqlite" {
			attQuery = attQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if e := attQuery.Take(&lockedAtt).Error; e != nil || lockedAtt.Status != constants.StatusOk || lockedAtt.TopicId != lockedTopic.Id {
			return errors.New(locales.Get("attachment.not_found"))
		}
		// Another concurrent download may have completed while this request was
		// waiting for the locks. Re-check inside the transaction before charging.
		if repositories.AttachmentDownloadLogRepository.Exists(ctx.Tx, userId, attachmentId) {
			return nil
		}

		user := repositories.UserRepository.Get(ctx.Tx, userId)
		if user == nil {
			return errors.New(locales.Get("common.not_found"))
		}
		if user.Score < lockedAtt.DownloadScore {
			return errors.New(locales.Get("attachment.insufficient_score"))
		}
		if err := UserService.DecrScoreTx(ctx, userId, lockedAtt.DownloadScore, constants.SourceTypeAttachmentDownload, attachmentId, locales.Get("attachment.download_deduct")); err != nil {
			return err
		}
		if err := repositories.AttachmentDownloadLogRepository.Create(ctx.Tx, &models.AttachmentDownloadLog{
			UserId:       userId,
			AttachmentId: attachmentId,
			CreateTime:   dates.NowTimestamp(),
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	redirectURL = s.GetDownloadRedirectUrl(att)
	if strs.IsNotBlank(redirectURL) {
		repositories.AttachmentRepository.IncrDownloadCount(sqls.DB(), att.Id)
	}
	return redirectURL, nil
}

// SoftDeleteByTopicId 帖子删除时软删除其下所有附件
func (s *attachmentService) SoftDeleteByTopicId(ctx *sqls.TxContext, topicId int64) error {
	return repositories.AttachmentRepository.UpdateColumns(ctx.Tx, topicId, map[string]interface{}{
		"status":      constants.StatusDeleted,
		"update_time": dates.NowTimestamp(),
	})
}

// RestoreByTopicId restores attachments that are still bound to an undeleted
// topic. Attachments removed during an edit have topic_id=0 and are unaffected.
func (s *attachmentService) RestoreByTopicId(ctx *sqls.TxContext, topicId int64) error {
	return repositories.AttachmentRepository.UpdateColumns(ctx.Tx, topicId, map[string]interface{}{
		"status":      constants.StatusOk,
		"update_time": dates.NowTimestamp(),
	})
}

// ReplaceTopicAttachments 编辑帖时全量替换附件
func (s *attachmentService) ReplaceTopicAttachments(ctx *sqls.TxContext, topicId, userId int64, attachmentIds []string) error {
	newSet := make(map[string]bool)
	for _, id := range attachmentIds {
		if strs.IsNotBlank(id) {
			newSet[id] = true
		}
	}

	// The topic root row is already locked by TopicService.Edit. Build one
	// deterministic lock set containing both currently-bound attachments and
	// requested attachments, then acquire every attachment row in lexical ID
	// order. This prevents A->B / B->A attachment swaps from deadlocking when
	// two topic edits run concurrently.
	current := repositories.AttachmentRepository.ListByTopicId(ctx.Tx, topicId)
	lockSet := make(map[string]struct{}, len(current)+len(attachmentIds))
	for _, att := range current {
		if strs.IsNotBlank(att.Id) {
			lockSet[att.Id] = struct{}{}
		}
	}
	for _, aid := range attachmentIds {
		if strs.IsNotBlank(aid) {
			lockSet[aid] = struct{}{}
		}
	}
	lockIDs := make([]string, 0, len(lockSet))
	for id := range lockSet {
		lockIDs = append(lockIDs, id)
	}
	sort.Strings(lockIDs)
	locked := make(map[string]models.Attachment, len(lockIDs))
	for _, aid := range lockIDs {
		var att models.Attachment
		query := ctx.Tx.Where("id = ?", aid)
		if ctx.Tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Take(&att).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(locales.Get("attachment.no_permission"))
			}
			return err
		}
		locked[aid] = att
	}

	// 从当前中移除的：解绑 + 软删除
	for _, att := range current {
		if !newSet[att.Id] {
			if err := repositories.AttachmentRepository.Updates(ctx.Tx, att.Id, map[string]interface{}{
				"topic_id": 0, "status": constants.StatusDeleted, "update_time": dates.NowTimestamp(),
			}); err != nil {
				return err
			}
		}
	}

	// 新列表中的：校验归属且未绑其他帖，再绑定
	for _, aid := range attachmentIds {
		if strs.IsBlank(aid) {
			continue
		}
		att, ok := locked[aid]
		if !ok || att.UserId != userId || att.Status != constants.StatusOk {
			return errors.New(locales.Get("attachment.no_permission"))
		}
		if att.TopicId != 0 && att.TopicId != topicId {
			return errors.New(locales.Get("attachment.already_bound"))
		}
		if err := repositories.AttachmentRepository.Updates(ctx.Tx, aid, map[string]interface{}{
			"topic_id": topicId, "status": constants.StatusOk, "update_time": dates.NowTimestamp(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// CheckAttachmentsExistAndOwned 检查 attachmentIds 是否存在且均属于 userId，且未绑定其他帖子（或仅绑定 topicId）
func (s *attachmentService) CheckAttachmentsExistAndOwned(ctx *sqls.TxContext, userId int64, attachmentIds []string, topicId int64) error {
	for _, aid := range attachmentIds {
		if strs.IsBlank(aid) {
			continue
		}
		att := repositories.AttachmentRepository.Get(ctx.Tx, aid)
		if att == nil || att.UserId != userId || att.Status != constants.StatusOk {
			return errors.New(locales.Get("attachment.no_permission"))
		}
		if att.TopicId != 0 && att.TopicId != topicId {
			return errors.New(locales.Get("attachment.already_bound"))
		}
	}
	return nil
}
