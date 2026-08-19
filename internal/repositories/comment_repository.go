package repositories

import (
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/params"

	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"

	"bbs-go/internal/models"
)

var CommentRepository = newCommentRepository()

func newCommentRepository() *commentRepository {
	return &commentRepository{}
}

type commentRepository struct {
}

func (r *commentRepository) Get(db *gorm.DB, id int64) *models.Comment {
	ret := &models.Comment{}
	if err := db.First(ret, "id = ?", id).Error; err != nil {
		return nil
	}
	return ret
}

func (r *commentRepository) Take(db *gorm.DB, where ...interface{}) *models.Comment {
	ret := &models.Comment{}
	if err := db.Take(ret, where...).Error; err != nil {
		return nil
	}
	return ret
}

func (r *commentRepository) Find(db *gorm.DB, cnd *sqls.Cnd) (list []models.Comment) {
	cnd.Find(db, &list)
	return
}

func (r *commentRepository) FindOne(db *gorm.DB, cnd *sqls.Cnd) *models.Comment {
	ret := &models.Comment{}
	if err := cnd.FindOne(db, &ret); err != nil {
		return nil
	}
	return ret
}

func (r *commentRepository) FindPageByParams(db *gorm.DB, params *params.QueryParams) (list []models.Comment, paging *sqls.Paging) {
	return r.FindPageByCnd(db, &params.Cnd)
}

func (r *commentRepository) FindPageByCnd(db *gorm.DB, cnd *sqls.Cnd) (list []models.Comment, paging *sqls.Paging) {
	cnd.Find(db, &list)
	count := cnd.Count(db, &models.Comment{})

	paging = &sqls.Paging{
		Page:  cnd.Paging.Page,
		Limit: cnd.Paging.Limit,
		Total: count,
	}
	return
}

func (r *commentRepository) Count(db *gorm.DB, cnd *sqls.Cnd) int64 {
	return cnd.Count(db, &models.Comment{})
}

func (r *commentRepository) FindByIds(db *gorm.DB, ids []int64) (list []models.Comment) {
	if len(ids) == 0 {
		return nil
	}
	db.Where("id IN ?", ids).Find(&list)
	return
}

// FindTopRepliesByCommentIds 批量获取每个一级评论最早的 limit 条回复。
// 使用窗口函数把原来的逐评论查询合并成一次数据库访问；MySQL 8 和项目使用的 SQLite 均支持该语法。
func (r *commentRepository) FindTopRepliesByCommentIds(db *gorm.DB, commentIds []int64, limit int) (list []models.Comment, err error) {
	if len(commentIds) == 0 || limit <= 0 {
		return nil, nil
	}
	err = db.Raw(`
SELECT ranked.*
FROM (
    SELECT c.*, ROW_NUMBER() OVER (PARTITION BY c.entity_id ORDER BY c.id ASC) AS reply_rank
    FROM t_comment c
    WHERE c.entity_type = ? AND c.status = ? AND c.entity_id IN ?
) ranked
WHERE ranked.reply_rank <= ?
ORDER BY ranked.entity_id ASC, ranked.id ASC`, constants.EntityComment, constants.StatusOk, commentIds, limit).Scan(&list).Error
	return
}

func (r *commentRepository) Create(db *gorm.DB, t *models.Comment) (err error) {
	err = db.Create(t).Error
	return
}

func (r *commentRepository) Update(db *gorm.DB, t *models.Comment) (err error) {
	err = db.Save(t).Error
	return
}

func (r *commentRepository) Updates(db *gorm.DB, id int64, columns map[string]interface{}) (err error) {
	err = db.Model(&models.Comment{}).Where("id = ?", id).Updates(columns).Error
	return
}

func (r *commentRepository) UpdateColumn(db *gorm.DB, id int64, name string, value interface{}) (err error) {
	err = db.Model(&models.Comment{}).Where("id = ?", id).UpdateColumn(name, value).Error
	return
}

func (r *commentRepository) Delete(db *gorm.DB, id int64) {
	db.Delete(&models.Comment{}, "id = ?", id)
}
