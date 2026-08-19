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

var FavoriteService = newFavoriteService()

func newFavoriteService() *favoriteService {
	return &favoriteService{}
}

type favoriteService struct {
}

func (s *favoriteService) Get(id int64) *models.Favorite {
	return repositories.FavoriteRepository.Get(sqls.DB(), id)
}

func (s *favoriteService) Take(where ...interface{}) *models.Favorite {
	return repositories.FavoriteRepository.Take(sqls.DB(), where...)
}

func (s *favoriteService) Find(cnd *sqls.Cnd) []models.Favorite {
	return repositories.FavoriteRepository.Find(sqls.DB(), cnd)
}

func (s *favoriteService) FindOne(cnd *sqls.Cnd) *models.Favorite {
	return repositories.FavoriteRepository.FindOne(sqls.DB(), cnd)
}

func (s *favoriteService) FindPageByParams(params *params.QueryParams) (list []models.Favorite, paging *sqls.Paging) {
	return repositories.FavoriteRepository.FindPageByParams(sqls.DB(), params)
}

func (s *favoriteService) FindPageByCnd(cnd *sqls.Cnd) (list []models.Favorite, paging *sqls.Paging) {
	return repositories.FavoriteRepository.FindPageByCnd(sqls.DB(), cnd)
}

func (s *favoriteService) Create(t *models.Favorite) error {
	return repositories.FavoriteRepository.Create(sqls.DB(), t)
}

func (s *favoriteService) Update(t *models.Favorite) error {
	return repositories.FavoriteRepository.Update(sqls.DB(), t)
}

func (s *favoriteService) Updates(id int64, columns map[string]interface{}) error {
	return repositories.FavoriteRepository.Updates(sqls.DB(), id, columns)
}

func (s *favoriteService) UpdateColumn(id int64, name string, value interface{}) error {
	return repositories.FavoriteRepository.UpdateColumn(sqls.DB(), id, name, value)
}

func (s *favoriteService) Delete(id int64) {
	repositories.FavoriteRepository.Delete(sqls.DB(), id)
}

func (s *favoriteService) IsFavorited(userId int64, entityType string, entityId int64) bool {
	return repositories.FavoriteRepository.Take(sqls.DB(), "user_id = ? and entity_type = ? and entity_id = ?",
		userId, entityType, entityId) != nil
}

func (s *favoriteService) GetBy(userId int64, entityType string, entityId int64) *models.Favorite {
	return repositories.FavoriteRepository.Take(sqls.DB(), "user_id = ? and entity_type = ? and entity_id = ?",
		userId, entityType, entityId)
}

// AddArticleFavorite 收藏文章
func (s *favoriteService) AddArticleFavorite(userId, articleId int64) error {
	if !favoriteEntityExists(&models.Article{}, articleId) {
		return errors.New(locales.Get("favorite.article_not_found"))
	}
	return s.addFavorite(userId, constants.EntityArticle, articleId)
}

// AddTopicFavorite 收藏主题
func (s *favoriteService) AddTopicFavorite(userId, topicId int64) error {
	if !favoriteEntityExists(&models.Topic{}, topicId) {
		return errors.New(locales.Get("favorite.topic_not_found"))
	}
	return s.addFavorite(userId, constants.EntityTopic, topicId)
}

func favoriteEntityExists(model interface{}, id int64) bool {
	if id <= 0 {
		return false
	}
	var foundId int64
	err := sqls.DB().Model(model).Select("id").Where("id = ? AND status = ?", id, constants.StatusOk).
		Limit(1).Scan(&foundId).Error
	return err == nil && foundId > 0
}

func (s *favoriteService) addFavorite(userId int64, entityType string, entityId int64) error {
	if err := repositories.FavoriteRepository.Create(sqls.DB(), &models.Favorite{
		UserId:     userId,
		EntityType: entityType,
		EntityId:   entityId,
		CreateTime: dates.NowTimestamp(),
	}); err != nil {
		// The unique index is the concurrency guard. Avoiding a preflight SELECT
		// halves the normal favorite path and treats duplicate retries as success.
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil
		}
		return err
	}

	// 只有真正创建收藏记录时才发送事件，避免并发重试重复计数。
	event.Send(event.UserFavoriteEvent{
		UserId:     userId,
		EntityId:   entityId,
		EntityType: entityType,
	})
	return nil
}
