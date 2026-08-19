package services

import (
	"errors"
	"sort"
	"sync"

	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/locales"

	"bbs-go/internal/pkg/params"

	"github.com/mlogclub/simple/sqls"

	"bbs-go/internal/models"
	"bbs-go/internal/repositories"

	"gorm.io/gorm"
)

var CategoryService = newCategoryService()

func newCategoryService() *categoryService {
	return &categoryService{}
}

type categorySnapshot struct {
	byID map[int64]models.Category
	all  []models.Category
}

type categoryService struct {
	snapshotMu sync.RWMutex
	snapshot   *categorySnapshot
}

func (s *categoryService) getSnapshot() *categorySnapshot {
	s.snapshotMu.RLock()
	snapshot := s.snapshot
	s.snapshotMu.RUnlock()
	if snapshot != nil {
		return snapshot
	}

	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	if s.snapshot != nil {
		return s.snapshot
	}

	list := repositories.CategoryRepository.Find(sqls.DB(), sqls.NewCnd().Asc("sort_no").Desc("id"))
	byID := make(map[int64]models.Category, len(list))
	for _, category := range list {
		byID[category.Id] = category
	}
	s.snapshot = &categorySnapshot{byID: byID, all: list}
	return s.snapshot
}

func (s *categoryService) invalidateSnapshot() {
	s.snapshotMu.Lock()
	s.snapshot = nil
	s.snapshotMu.Unlock()
}

func (s *categoryService) Get(id int64) *models.Category {
	if id <= 0 {
		return nil
	}
	category, ok := s.getSnapshot().byID[id]
	if !ok {
		return nil
	}
	copy := category
	return &copy
}

// GetMapWithParents serves requested categories and their parents from the
// in-process immutable snapshot, eliminating category SQL from every topic page.
func (s *categoryService) GetMapWithParents(ids []int64) map[int64]models.Category {
	result := make(map[int64]models.Category)
	snapshot := s.getSnapshot()
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		category, ok := snapshot.byID[id]
		if !ok {
			continue
		}
		result[id] = category
		if category.ParentId > 0 {
			if parent, exists := snapshot.byID[category.ParentId]; exists {
				result[parent.Id] = parent
			}
		}
	}
	return result
}

// IsAdminOnlyPostFromMap resolves inherited posting policy without additional
// database calls.
func (s *categoryService) IsAdminOnlyPostFromMap(category *models.Category, categories map[int64]models.Category) bool {
	if category == nil {
		return false
	}
	if category.AdminOnlyPost {
		return true
	}
	if category.ParentId <= 0 {
		return false
	}
	parent, ok := categories[category.ParentId]
	return ok && parent.AdminOnlyPost
}

// IsAdminOnlyPost returns the effective posting policy. Child categories inherit
// the restriction from their parent so an announcement sub-board cannot bypass it.
func (s *categoryService) IsAdminOnlyPost(category *models.Category) bool {
	if category == nil {
		return false
	}
	if category.AdminOnlyPost {
		return true
	}
	if category.ParentId <= 0 {
		return false
	}
	parent := s.Get(category.ParentId)
	return parent != nil && parent.AdminOnlyPost
}

func (s *categoryService) Take(where ...interface{}) *models.Category {
	return repositories.CategoryRepository.Take(sqls.DB(), where...)
}

func (s *categoryService) Find(cnd *sqls.Cnd) []models.Category {
	return repositories.CategoryRepository.Find(sqls.DB(), cnd)
}

func (s *categoryService) FindOne(cnd *sqls.Cnd) *models.Category {
	return repositories.CategoryRepository.FindOne(sqls.DB(), cnd)
}

func (s *categoryService) FindPageByParams(params *params.QueryParams) (list []models.Category, paging *sqls.Paging) {
	return repositories.CategoryRepository.FindPageByParams(sqls.DB(), params)
}

func (s *categoryService) FindPageByCnd(cnd *sqls.Cnd) (list []models.Category, paging *sqls.Paging) {
	return repositories.CategoryRepository.FindPageByCnd(sqls.DB(), cnd)
}

func (s *categoryService) Create(t *models.Category) error {
	err := repositories.CategoryRepository.Create(sqls.DB(), t)
	if err == nil {
		s.invalidateSnapshot()
	}
	return err
}

func (s *categoryService) Update(t *models.Category) error {
	err := repositories.CategoryRepository.Update(sqls.DB(), t)
	if err == nil {
		s.invalidateSnapshot()
	}
	return err
}

func (s *categoryService) Updates(id int64, columns map[string]interface{}) error {
	err := repositories.CategoryRepository.Updates(sqls.DB(), id, columns)
	if err == nil {
		s.invalidateSnapshot()
	}
	return err
}

func (s *categoryService) UpdateColumn(id int64, name string, value interface{}) error {
	err := repositories.CategoryRepository.UpdateColumn(sqls.DB(), id, name, value)
	if err == nil {
		s.invalidateSnapshot()
	}
	return err
}

// DeleteWithCheck 删除节点，若为一级且有子节点则返回错误
func (s *categoryService) DeleteWithCheck(id int64) error {
	category := s.Get(id)
	if category == nil {
		return nil
	}
	if category.ParentId == 0 {
		children := s.GetChildren(id)
		if len(children) > 0 {
			return errors.New(locales.Get("topic.category.has_children"))
		}
	}
	err := repositories.CategoryRepository.Updates(sqls.DB(), id, map[string]interface{}{
		"status": constants.StatusDeleted,
	})
	if err == nil {
		s.invalidateSnapshot()
	}
	return err
}

// GetTopLevelCategories 仅一级节点（parent_id=0），用于导航
func (s *categoryService) GetTopLevelCategories() []models.Category {
	return s.filterSnapshot(func(category models.Category) bool {
		return category.Status == constants.StatusOk && category.ParentId == 0
	})
}

// GetChildren 获取某一级下的二级节点
func (s *categoryService) GetChildren(parentId int64) []models.Category {
	return s.filterSnapshot(func(category models.Category) bool {
		return category.Status == constants.StatusOk && category.ParentId == parentId
	})
}

func (s *categoryService) filterSnapshot(accept func(models.Category) bool) []models.Category {
	all := s.getSnapshot().all
	result := make([]models.Category, 0, len(all))
	for _, category := range all {
		if accept(category) {
			result = append(result, category)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SortNo == result[j].SortNo {
			return result[i].Id > result[j].Id
		}
		return result[i].SortNo < result[j].SortNo
	})
	return result
}

// GetCategoryIdsForList 用于帖子列表筛选：一级返回 [自身+子节点id]，二级返回 [自身]
func (s *categoryService) GetCategoryIdsForList(categoryId int64) []int64 {
	category := s.Get(categoryId)
	if category == nil {
		return nil
	}
	if category.ParentId == 0 {
		children := s.GetChildren(categoryId)
		ids := make([]int64, 0, len(children)+1)
		ids = append(ids, categoryId)
		for _, child := range children {
			ids = append(ids, child.Id)
		}
		return ids
	}
	return []int64{categoryId}
}

func (s *categoryService) GetCategories() []models.Category {
	return s.filterSnapshot(func(category models.Category) bool {
		return category.Status == constants.StatusOk
	})
}

func (s *categoryService) GetCategoriesByType(categoryType constants.CategoryType) []models.Category {
	return s.filterSnapshot(func(category models.Category) bool {
		return category.Status == constants.StatusOk && category.Type == categoryType
	})
}

func (s *categoryService) GetCategoriesByTopicType(topicType constants.TopicType) []models.Category {
	return s.filterSnapshot(func(category models.Category) bool {
		if category.Status != constants.StatusOk {
			return false
		}
		// Questions may be published in both normal boards and dedicated QA
		// boards. Other topic types continue to use normal boards only.
		isNormal := category.Type == "" || category.Type == constants.CategoryTypeNormal
		if topicType == constants.TopicTypeQA {
			return isNormal || category.Type == constants.CategoryTypeQA
		}
		return isNormal
	})
}

func (s *categoryService) GetNextSortNo() int {
	if max := s.FindOne(sqls.NewCnd().Eq("status", constants.StatusOk).Desc("sort_no")); max != nil {
		return max.SortNo + 1
	}
	return 0
}

func (s *categoryService) UpdateSort(ids []int64) error {
	err := sqls.DB().Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			if err := repositories.CategoryRepository.UpdateColumn(tx, id, "sort_no", i); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.invalidateSnapshot()
	}
	return err
}

// UpdateChildrenType 将父节点下所有子节点的 type 更新为指定值（父节点编辑类型时联动）
func (s *categoryService) UpdateChildrenType(parentId int64, categoryType constants.CategoryType) error {
	children := s.GetChildren(parentId)
	if len(children) == 0 {
		return nil
	}
	err := sqls.DB().Transaction(func(tx *gorm.DB) error {
		for _, c := range children {
			if err := repositories.CategoryRepository.UpdateColumn(tx, c.Id, "type", categoryType); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.invalidateSnapshot()
	}
	return err
}
