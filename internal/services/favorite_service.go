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
	"gorm.io/gorm/clause"

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
	return s.addFavoriteForEntity(userId, constants.EntityArticle, articleId, &models.Article{}, locales.Get("favorite.article_not_found"))
}

// AddTopicFavorite 收藏主题
func (s *favoriteService) AddTopicFavorite(userId, topicId int64) error {
	return s.addFavoriteForEntity(userId, constants.EntityTopic, topicId, &models.Topic{}, locales.Get("favorite.topic_not_found"))
}

// addFavoriteForEntity serializes favorite creation with physical root deletion.
// A plain preflight SELECT can race with hard delete and leave a favorite row
// after the root disappears; a root row lock makes the two operations ordered.
func (s *favoriteService) addFavoriteForEntity(userId int64, entityType string, entityId int64, model interface{}, notFoundMessage string) error {
	if entityId <= 0 {
		return errors.New(notFoundMessage)
	}
	created := false
	err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		var found struct{ Id int64 }
		query := ctx.Tx.Model(model).Select("id").Where("id = ? AND status = ?", entityId, constants.StatusOk)
		if ctx.Tx.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Take(&found).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(notFoundMessage)
			}
			return err
		}
		if err := repositories.FavoriteRepository.Create(ctx.Tx, &models.Favorite{
			UserId:     userId,
			EntityType: entityType,
			EntityId:   entityId,
			CreateTime: dates.NowTimestamp(),
		}); err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return nil
			}
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return err
	}
	if created {
		event.Send(event.UserFavoriteEvent{
			UserId:     userId,
			EntityId:   entityId,
			EntityType: entityType,
		})
	}
	return nil
}
