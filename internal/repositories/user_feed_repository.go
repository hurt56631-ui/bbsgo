package repositories

import (
	"bbs-go/internal/models"

	"bbs-go/internal/pkg/params"

	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var UserFeedRepository = newUserFeedRepository()

func newUserFeedRepository() *userFeedRepository {
	return &userFeedRepository{}
}

type userFeedRepository struct {
}

func (r *userFeedRepository) Get(db *gorm.DB, id int64) *models.UserFeed {
	ret := &models.UserFeed{}
	if err := db.First(ret, "id = ?", id).Error; err != nil {
		return nil
	}
	return ret
}

func (r *userFeedRepository) Take(db *gorm.DB, where ...interface{}) *models.UserFeed {
	ret := &models.UserFeed{}
	if err := db.Take(ret, where...).Error; err != nil {
		return nil
	}
	return ret
}

func (r *userFeedRepository) Find(db *gorm.DB, cnd *sqls.Cnd) (list []models.UserFeed) {
	cnd.Find(db, &list)
	return
}

func (r *userFeedRepository) FindOne(db *gorm.DB, cnd *sqls.Cnd) *models.UserFeed {
	ret := &models.UserFeed{}
	if err := cnd.FindOne(db, &ret); err != nil {
		return nil
	}
	return ret
}

func (r *userFeedRepository) FindPageByParams(db *gorm.DB, params *params.QueryParams) (list []models.UserFeed, paging *sqls.Paging) {
	return r.FindPageByCnd(db, &params.Cnd)
}

func (r *userFeedRepository) FindPageByCnd(db *gorm.DB, cnd *sqls.Cnd) (list []models.UserFeed, paging *sqls.Paging) {
	cnd.Find(db, &list)
	count := cnd.Count(db, &models.UserFeed{})

	paging = &sqls.Paging{
		Page:  cnd.Paging.Page,
		Limit: cnd.Paging.Limit,
		Total: count,
	}
	return
}

func (r *userFeedRepository) Count(db *gorm.DB, cnd *sqls.Cnd) int64 {
	return cnd.Count(db, &models.UserFeed{})
}

func (r *userFeedRepository) Create(db *gorm.DB, t *models.UserFeed) (err error) {
	err = db.Create(t).Error
	return
}

// CreateInBatches inserts feed fan-out rows with one statement per batch. The
// unique constraint makes retries idempotent, so duplicate rows are ignored.
func (r *userFeedRepository) CreateInBatches(db *gorm.DB, feeds []models.UserFeed, batchSize int) error {
	if len(feeds) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(feeds, batchSize).Error
}

// FanOutFromFollowers creates one feed row for every current follower with a
// single set-based INSERT ... SELECT statement. This avoids loading follower
// IDs into Go and turns hundreds of batched round trips for a high-fan account
// into one database operation. The unique key keeps retries idempotent.
func (r *userFeedRepository) FanOutFromFollowers(db *gorm.DB, authorId, dataId int64, dataType string, createTime int64) error {
	if authorId <= 0 || dataId <= 0 {
		return nil
	}
	query := `INSERT IGNORE INTO t_user_feed (user_id, data_id, data_type, author_id, create_time)
		SELECT user_id, ?, ?, ?, ? FROM t_user_follow WHERE other_id = ?`
	if db.Dialector.Name() == "sqlite" {
		query = `INSERT OR IGNORE INTO t_user_feed (user_id, data_id, data_type, author_id, create_time)
			SELECT user_id, ?, ?, ?, ? FROM t_user_follow WHERE other_id = ?`
	}
	return db.Exec(query, dataId, dataType, authorId, createTime, authorId).Error
}

func (r *userFeedRepository) Update(db *gorm.DB, t *models.UserFeed) (err error) {
	err = db.Save(t).Error
	return
}

func (r *userFeedRepository) Updates(db *gorm.DB, id int64, columns map[string]interface{}) (err error) {
	err = db.Model(&models.UserFeed{}).Where("id = ?", id).Updates(columns).Error
	return
}

func (r *userFeedRepository) UpdateColumn(db *gorm.DB, id int64, name string, value interface{}) (err error) {
	err = db.Model(&models.UserFeed{}).Where("id = ?", id).UpdateColumn(name, value).Error
	return
}

func (r *userFeedRepository) Delete(db *gorm.DB, id int64) {
	db.Delete(&models.UserFeed{}, "id = ?", id)
}
