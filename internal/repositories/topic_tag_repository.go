package repositories

import (
	"bbs-go/internal/pkg/params"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"

	"bbs-go/internal/models"
)

var TopicTagRepository = newTopicTagRepository()

func newTopicTagRepository() *topicTagRepository {
	return &topicTagRepository{}
}

type topicTagRepository struct {
}

func (r *topicTagRepository) Get(db *gorm.DB, id int64) *models.TopicTag {
	ret := &models.TopicTag{}
	if err := db.First(ret, "id = ?", id).Error; err != nil {
		return nil
	}
	return ret
}

func (r *topicTagRepository) Take(db *gorm.DB, where ...interface{}) *models.TopicTag {
	ret := &models.TopicTag{}
	if err := db.Take(ret, where...).Error; err != nil {
		return nil
	}
	return ret
}

func (r *topicTagRepository) Find(db *gorm.DB, cnd *sqls.Cnd) (list []models.TopicTag) {
	cnd.Find(db, &list)
	return
}

func (r *topicTagRepository) FindOne(db *gorm.DB, cnd *sqls.Cnd) *models.TopicTag {
	ret := &models.TopicTag{}
	if err := cnd.FindOne(db, &ret); err != nil {
		return nil
	}
	return ret
}

func (r *topicTagRepository) FindPageByParams(db *gorm.DB, params *params.QueryParams) (list []models.TopicTag, paging *sqls.Paging) {
	return r.FindPageByCnd(db, &params.Cnd)
}

func (r *topicTagRepository) FindPageByCnd(db *gorm.DB, cnd *sqls.Cnd) (list []models.TopicTag, paging *sqls.Paging) {
	cnd.Find(db, &list)
	count := cnd.Count(db, &models.TopicTag{})

	paging = &sqls.Paging{
		Page:  cnd.Paging.Page,
		Limit: cnd.Paging.Limit,
		Total: count,
	}
	return
}

func (r *topicTagRepository) FindByTopicIds(db *gorm.DB, topicIds []int64, status int) (list []models.TopicTag) {
	if len(topicIds) == 0 {
		return nil
	}
	db.Where("topic_id IN ? AND status = ?", topicIds, status).
		Order("topic_id ASC").Order("id ASC").Find(&list)
	return
}

func (r *topicTagRepository) Create(db *gorm.DB, t *models.TopicTag) (err error) {
	err = db.Create(t).Error
	return
}

func (r *topicTagRepository) Update(db *gorm.DB, t *models.TopicTag) (err error) {
	err = db.Save(t).Error
	return
}

func (r *topicTagRepository) Updates(db *gorm.DB, id int64, columns map[string]interface{}) (err error) {
	err = db.Model(&models.TopicTag{}).Where("id = ?", id).Updates(columns).Error
	return
}

func (r *topicTagRepository) UpdateColumn(db *gorm.DB, id int64, name string, value interface{}) (err error) {
	err = db.Model(&models.TopicTag{}).Where("id = ?", id).UpdateColumn(name, value).Error
	return
}

func (r *topicTagRepository) Delete(db *gorm.DB, id int64) {
	db.Delete(&models.TopicTag{}, "id = ?", id)
}

func (r *topicTagRepository) AddTopicTags(db *gorm.DB, topicId int64, tagIds []int64) error {
	if topicId <= 0 || len(tagIds) == 0 {
		return nil
	}
	now := dates.NowTimestamp()
	seen := make(map[int64]struct{}, len(tagIds))
	rows := make([]models.TopicTag, 0, len(tagIds))
	for _, tagId := range tagIds {
		if tagId <= 0 {
			continue
		}
		if _, exists := seen[tagId]; exists {
			continue
		}
		seen[tagId] = struct{}{}
		rows = append(rows, models.TopicTag{TopicId: topicId, TagId: tagId, CreateTime: now})
	}
	if len(rows) == 0 {
		return nil
	}
	return db.CreateInBatches(&rows, 100).Error
}
