package repositories

import (
	"bbs-go/internal/pkg/params"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"

	"bbs-go/internal/models"
)

var ArticleTagRepository = newArticleTagRepository()

func newArticleTagRepository() *articleTagRepository {
	return &articleTagRepository{}
}

type articleTagRepository struct {
}

func (r *articleTagRepository) Get(db *gorm.DB, id int64) *models.ArticleTag {
	ret := &models.ArticleTag{}
	if err := db.First(ret, "id = ?", id).Error; err != nil {
		return nil
	}
	return ret
}

func (r *articleTagRepository) Take(db *gorm.DB, where ...interface{}) *models.ArticleTag {
	ret := &models.ArticleTag{}
	if err := db.Take(ret, where...).Error; err != nil {
		return nil
	}
	return ret
}

func (r *articleTagRepository) Find(db *gorm.DB, cnd *sqls.Cnd) (list []models.ArticleTag) {
	cnd.Find(db, &list)
	return
}

func (r *articleTagRepository) FindOne(db *gorm.DB, cnd *sqls.Cnd) *models.ArticleTag {
	ret := &models.ArticleTag{}
	if err := cnd.FindOne(db, &ret); err != nil {
		return nil
	}
	return ret
}

func (r *articleTagRepository) FindPageByParams(db *gorm.DB, params *params.QueryParams) (list []models.ArticleTag, paging *sqls.Paging) {
	return r.FindPageByCnd(db, &params.Cnd)
}

func (r *articleTagRepository) FindPageByCnd(db *gorm.DB, cnd *sqls.Cnd) (list []models.ArticleTag, paging *sqls.Paging) {
	cnd.Find(db, &list)
	count := cnd.Count(db, &models.ArticleTag{})

	paging = &sqls.Paging{
		Page:  cnd.Paging.Page,
		Limit: cnd.Paging.Limit,
		Total: count,
	}
	return
}

func (r *articleTagRepository) Create(db *gorm.DB, t *models.ArticleTag) (err error) {
	err = db.Create(t).Error
	return
}

func (r *articleTagRepository) Update(db *gorm.DB, t *models.ArticleTag) (err error) {
	err = db.Save(t).Error
	return
}

func (r *articleTagRepository) Updates(db *gorm.DB, id int64, columns map[string]interface{}) (err error) {
	err = db.Model(&models.ArticleTag{}).Where("id = ?", id).Updates(columns).Error
	return
}

func (r *articleTagRepository) UpdateColumn(db *gorm.DB, id int64, name string, value interface{}) (err error) {
	err = db.Model(&models.ArticleTag{}).Where("id = ?", id).UpdateColumn(name, value).Error
	return
}

func (r *articleTagRepository) Delete(db *gorm.DB, id int64) {
	db.Delete(&models.ArticleTag{}, "id = ?", id)
}

func (r *articleTagRepository) AddArticleTags(db *gorm.DB, articleId int64, tagIds []int64) error {
	if articleId <= 0 || len(tagIds) == 0 {
		return nil
	}

	now := dates.NowTimestamp()
	seen := make(map[int64]struct{}, len(tagIds))
	rows := make([]models.ArticleTag, 0, len(tagIds))
	for _, tagId := range tagIds {
		if tagId <= 0 {
			continue
		}
		if _, exists := seen[tagId]; exists {
			continue
		}
		seen[tagId] = struct{}{}
		rows = append(rows, models.ArticleTag{
			ArticleId:  articleId,
			TagId:      tagId,
			CreateTime: now,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return db.CreateInBatches(&rows, 100).Error
}

func (r *articleTagRepository) DeleteArticleTags(db *gorm.DB, articleId int64) error {
	if articleId <= 0 {
		return nil
	}
	return db.Where("article_id = ?", articleId).Delete(&models.ArticleTag{}).Error
}

func (r *articleTagRepository) DeleteArticleTag(db *gorm.DB, articleId, tagId int64) {
	if articleId <= 0 {
		return
	}
	db.Where("article_id = ? and tag_id = ?", articleId, tagId).Delete(models.ArticleTag{})
}

func (r *articleTagRepository) FindByArticleId(db *gorm.DB, articleId int64) []models.ArticleTag {
	return r.Find(db, sqls.NewCnd().Where("article_id = ?", articleId))
}

func (r *articleTagRepository) FindByArticleIds(db *gorm.DB, articleIds []int64, status int) []models.ArticleTag {
	if len(articleIds) == 0 {
		return nil
	}
	var list []models.ArticleTag
	db.Where("article_id IN ? AND status = ?", articleIds, status).
		Order("article_id ASC").Order("id ASC").Find(&list)
	return list
}
