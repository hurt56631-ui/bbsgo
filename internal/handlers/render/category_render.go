package render

import (
	"bbs-go/internal/models"
	"bbs-go/internal/models/resp"
	"bbs-go/internal/services"

	"github.com/mlogclub/simple/common/strs"
)

func BuildCategory(category *models.Category) *resp.CategoryResponse {
	if category == nil {
		return nil
	}
	adminOnly := services.CategoryService.IsAdminOnlyPost(category)
	return buildCategoryResolved(category, adminOnly)
}

// BuildCategoryFromMap renders inherited permissions using already-loaded
// categories, avoiding parent lookups while assembling topic lists.
func BuildCategoryFromMap(category *models.Category, categories map[int64]models.Category) *resp.CategoryResponse {
	if category == nil {
		return nil
	}
	adminOnly := services.CategoryService.IsAdminOnlyPostFromMap(category, categories)
	return buildCategoryResolved(category, adminOnly)
}

func buildCategoryResolved(category *models.Category, adminOnly bool) *resp.CategoryResponse {
	logo := category.Logo
	if strs.IsBlank(logo) {
		logo = "/res/images/category_default.svg"
	}
	return &resp.CategoryResponse{
		Id:            category.Id,
		ParentId:      category.ParentId,
		Name:          category.Name,
		Type:          category.Type,
		Logo:          logo,
		Description:   category.Description,
		AdminOnlyPost: adminOnly,
		CanPost:       !adminOnly,
	}
}

func BuildCategoryWithChildren(category *models.Category) *resp.CategoryResponse {
	if category == nil {
		return nil
	}
	categories := map[int64]models.Category{category.Id: *category}
	children := services.CategoryService.GetChildren(category.Id)
	for _, child := range children {
		categories[child.Id] = child
	}
	r := BuildCategoryFromMap(category, categories)
	if category.ParentId == 0 && len(children) > 0 {
		r.Children = make([]resp.CategoryResponse, 0, len(children))
		for i := range children {
			r.Children = append(r.Children, *BuildCategoryFromMap(&children[i], categories))
		}
	}
	return r
}

func BuildCategoryResponses(categories []models.Category) []resp.CategoryResponse {
	if len(categories) == 0 {
		return nil
	}
	loaded := make(map[int64]models.Category, len(categories))
	missingParents := make([]int64, 0)
	for _, category := range categories {
		loaded[category.Id] = category
	}
	seenParents := make(map[int64]struct{})
	for _, category := range categories {
		if category.ParentId <= 0 {
			continue
		}
		if _, ok := loaded[category.ParentId]; ok {
			continue
		}
		if _, ok := seenParents[category.ParentId]; ok {
			continue
		}
		seenParents[category.ParentId] = struct{}{}
		missingParents = append(missingParents, category.ParentId)
	}
	for id, category := range services.CategoryService.GetMapWithParents(missingParents) {
		loaded[id] = category
	}

	ret := make([]resp.CategoryResponse, 0, len(categories))
	for i := range categories {
		ret = append(ret, *BuildCategoryFromMap(&categories[i], loaded))
	}
	return ret
}

func BuildCategoryResponseTree(parentId int64, list []models.Category) []resp.CategoryResponse {
	categories := make(map[int64]models.Category, len(list))
	for _, category := range list {
		categories[category.Id] = category
	}
	return buildCategoryResponseTree(parentId, list, categories)
}

func buildCategoryResponseTree(parentId int64, list []models.Category, categories map[int64]models.Category) []resp.CategoryResponse {
	var ret []resp.CategoryResponse
	for i := range list {
		category := &list[i]
		if category.ParentId != parentId {
			continue
		}
		item := BuildCategoryFromMap(category, categories)
		if item == nil {
			continue
		}
		children := buildCategoryResponseTree(category.Id, list, categories)
		if len(children) > 0 {
			item.Children = children
		}
		ret = append(ret, *item)
	}
	return ret
}

func BuildCategoryTree(parentId int64, list []models.Category) []resp.CategoryTreeItem {
	var ret []resp.CategoryTreeItem
	for _, category := range list {
		if category.ParentId == parentId {
			children := BuildCategoryTree(category.Id, list)
			logo := category.Logo
			if strs.IsBlank(logo) {
				logo = "/res/images/category_default.svg"
			}
			ret = append(ret, resp.CategoryTreeItem{
				Id:            category.Id,
				ParentId:      category.ParentId,
				Name:          category.Name,
				Type:          category.Type,
				Logo:          logo,
				Description:   category.Description,
				AdminOnlyPost: category.AdminOnlyPost,
				SortNo:        category.SortNo,
				Status:        category.Status,
				CreateTime:    category.CreateTime,
				Children:      children,
			})
		}
	}
	return ret
}
