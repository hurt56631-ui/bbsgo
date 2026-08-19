package services

import (
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/event"
	"bbs-go/internal/pkg/locales"
	"errors"

	"bbs-go/internal/pkg/params"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"

	"bbs-go/internal/models"
	"bbs-go/internal/repositories"
)

var UserLikeService = newUserLikeService()

func newUserLikeService() *userLikeService {
	return &userLikeService{}
}

type userLikeService struct {
}

func (s *userLikeService) Get(id int64) *models.UserLike {
	return repositories.UserLikeRepository.Get(sqls.DB(), id)
}

func (s *userLikeService) Take(where ...interface{}) *models.UserLike {
	return repositories.UserLikeRepository.Take(sqls.DB(), where...)
}

func (s *userLikeService) Find(cnd *sqls.Cnd) []models.UserLike {
	return repositories.UserLikeRepository.Find(sqls.DB(), cnd)
}

func (s *userLikeService) FindOne(cnd *sqls.Cnd) *models.UserLike {
	return repositories.UserLikeRepository.FindOne(sqls.DB(), cnd)
}

func (s *userLikeService) FindPageByParams(params *params.QueryParams) (list []models.UserLike, paging *sqls.Paging) {
	return repositories.UserLikeRepository.FindPageByParams(sqls.DB(), params)
}

func (s *userLikeService) FindPageByCnd(cnd *sqls.Cnd) (list []models.UserLike, paging *sqls.Paging) {
	return repositories.UserLikeRepository.FindPageByCnd(sqls.DB(), cnd)
}

func (s *userLikeService) Create(t *models.UserLike) error {
	return repositories.UserLikeRepository.Create(sqls.DB(), t)
}

func (s *userLikeService) Update(t *models.UserLike) error {
	return repositories.UserLikeRepository.Update(sqls.DB(), t)
}

func (s *userLikeService) Updates(id int64, columns map[string]interface{}) error {
	return repositories.UserLikeRepository.Updates(sqls.DB(), id, columns)
}

func (s *userLikeService) UpdateColumn(id int64, name string, value interface{}) error {
	return repositories.UserLikeRepository.UpdateColumn(sqls.DB(), id, name, value)
}

func (s *userLikeService) Delete(id int64) {
	repositories.UserLikeRepository.Delete(sqls.DB(), id)
}

// 统计数量
func (s *userLikeService) Count(entityType string, entityId int64) int64 {
	var count int64 = 0
	sqls.DB().Model(&models.UserLike{}).Where("entity_id = ?", entityId).Where("entity_type = ?", entityType).Count(&count)
	return count
}

// 最近点赞
func (s *userLikeService) Recent(entityType string, entityId int64, count int) []models.UserLike {
	return s.Find(sqls.NewCnd().Eq("entity_id", entityId).Eq("entity_type", entityType).Desc("id").Limit(count))
}

// Exists 是否点赞
func (s *userLikeService) Exists(userId int64, entityType string, entityId int64) bool {
	return repositories.UserLikeRepository.Exists(sqls.DB(), userId, entityType, entityId)
}

// 是否点赞，返回已点赞实体编号
func (s *userLikeService) IsLiked(userId int64, entityType string, entityIds []int64) (likedEntityIds []int64) {
	list := repositories.UserLikeRepository.Find(sqls.DB(), sqls.NewCnd().Eq("user_id", userId).
		In("entity_id", entityIds).Eq("entity_type", entityType))
	for _, like := range list {
		likedEntityIds = append(likedEntityIds, like.EntityId)
	}
	return
}

// TopicLike 话题点赞
func (s *userLikeService) TopicLike(userId int64, topicId int64) error {
	if err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		if err := s.like(ctx, userId, constants.EntityTopic, topicId); err != nil {
			return err
		}
		result := ctx.Tx.Model(&models.Topic{}).Where("id = ? AND status = ?", topicId, constants.StatusOk).
			UpdateColumn("like_count", gorm.Expr("like_count + 1"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New(locales.Get("topic.topic_not_found"))
		}
		return nil
	}); err != nil {
		return err
	}

	// 发送事件
	event.Send(event.UserLikeEvent{
		UserId:     userId,
		EntityId:   topicId,
		EntityType: constants.EntityTopic,
	})

	return nil
}

func (s *userLikeService) TopicUnLike(userId int64, topicId int64) error {
	deleted := false
	if err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		var err error
		deleted, err = s.unlike(ctx.Tx, userId, constants.EntityTopic, topicId)
		if err != nil || !deleted {
			return err
		}
		result := ctx.Tx.Model(&models.Topic{}).Where("id = ? AND status = ?", topicId, constants.StatusOk).
			UpdateColumn("like_count", gorm.Expr("CASE WHEN like_count > 0 THEN like_count - 1 ELSE 0 END"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New(locales.Get("topic.topic_not_found"))
		}
		return nil
	}); err != nil {
		return err
	}
	if !deleted {
		return nil
	}

	// 发送事件
	event.Send(event.UserUnLikeEvent{
		UserId:     userId,
		EntityId:   topicId,
		EntityType: constants.EntityTopic,
	})

	return nil
}

func (s *userLikeService) ArticleLike(userId int64, articleId int64) error {
	if err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		if err := s.like(ctx, userId, constants.EntityArticle, articleId); err != nil {
			return err
		}
		result := ctx.Tx.Model(&models.Article{}).Where("id = ? AND status = ?", articleId, constants.StatusOk).
			UpdateColumn("like_count", gorm.Expr("like_count + 1"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New(locales.Get("article.not_found"))
		}
		return nil
	}); err != nil {
		return err
	}

	// 发送事件
	event.Send(event.UserLikeEvent{
		UserId:     userId,
		EntityId:   articleId,
		EntityType: constants.EntityArticle,
	})
	return nil
}

func (s *userLikeService) ArticleUnLike(userId int64, articleId int64) error {
	deleted := false
	if err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		var err error
		deleted, err = s.unlike(ctx.Tx, userId, constants.EntityArticle, articleId)
		if err != nil || !deleted {
			return err
		}
		result := ctx.Tx.Model(&models.Article{}).Where("id = ? AND status = ?", articleId, constants.StatusOk).
			UpdateColumn("like_count", gorm.Expr("CASE WHEN like_count > 0 THEN like_count - 1 ELSE 0 END"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New(locales.Get("article.not_found"))
		}
		return nil
	}); err != nil {
		return err
	}
	if !deleted {
		return nil
	}

	// 发送事件
	event.Send(event.UserUnLikeEvent{
		UserId:     userId,
		EntityId:   articleId,
		EntityType: constants.EntityArticle,
	})

	return nil
}

// CommentLike comment like
func (s *userLikeService) CommentLike(userId int64, commentId int64) error {
	if err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		if err := s.like(ctx, userId, constants.EntityComment, commentId); err != nil {
			return err
		}
		result := ctx.Tx.Model(&models.Comment{}).Where("id = ? AND status = ?", commentId, constants.StatusOk).
			Updates(map[string]interface{}{
				"like_count": gorm.Expr("like_count + 1"),
				"hot_score":  gorm.Expr("hot_score + 3"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New(locales.Get("comment.not_found"))
		}
		return nil
	}); err != nil {
		return err
	}

	// 发送事件
	event.Send(event.UserLikeEvent{
		UserId:     userId,
		EntityId:   commentId,
		EntityType: constants.EntityComment,
	})

	return nil
}

// CommentUnLike comment unlike
func (s *userLikeService) CommentUnLike(userId int64, commentId int64) error {
	deleted := false
	if err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		var err error
		deleted, err = s.unlike(ctx.Tx, userId, constants.EntityComment, commentId)
		if err != nil || !deleted {
			return err
		}
		result := ctx.Tx.Model(&models.Comment{}).Where("id = ? AND status = ?", commentId, constants.StatusOk).
			Updates(map[string]interface{}{
				"like_count": gorm.Expr("CASE WHEN like_count > 0 THEN like_count - 1 ELSE 0 END"),
				"hot_score":  gorm.Expr("CASE WHEN hot_score >= 3 THEN hot_score - 3 ELSE 0 END"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New(locales.Get("comment.not_found"))
		}
		return nil
	}); err != nil {
		return err
	}
	if !deleted {
		return nil
	}

	// 发送事件
	event.Send(event.UserUnLikeEvent{
		UserId:     userId,
		EntityId:   commentId,
		EntityType: constants.EntityComment,
	})

	return nil
}

func (s *userLikeService) like(ctx *sqls.TxContext, userId int64, entityType string, entityId int64) error {
	err := repositories.UserLikeRepository.Create(ctx.Tx, &models.UserLike{
		UserId:     userId,
		EntityType: entityType,
		EntityId:   entityId,
		CreateTime: dates.NowTimestamp(),
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return errors.New(locales.Get("like.already_liked"))
	}
	return err
}

func (s userLikeService) unlike(tx *gorm.DB, userId int64, entityType string, entityId int64) (bool, error) {
	result := tx.Delete(&models.UserLike{}, "user_id = ? and entity_id = ? and entity_type = ?", userId, entityId, entityType)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
