package migrations

import (
	"errors"

	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
)

func init() {
	register(1002, "add Talkami forum categories", migrateTalkamiForumCategories)
}

type talkamiCategorySeed struct {
	name        string
	description string
	sortNo      int
}

// migrateTalkamiForumCategories adds the product-facing Talkami sections to
// existing installations without deleting or renaming administrator-created
// categories. Fresh databases receive the same categories after migration 1.
func migrateTalkamiForumCategories() error {
	seeds := []talkamiCategorySeed{
		{name: "学习交流", description: "语言学习方法、语法、翻译、作业答疑与经验分享", sortNo: 20},
		{name: "口语练习", description: "中文、缅语和其他语言的发音练习、口语打卡与纠音", sortNo: 30},
		{name: "学习资源", description: "课程、词典、网站、工具与学习资料整理", sortNo: 40},
		{name: "中缅贸易", description: "货源、采购、物流、清关、商务合作与贸易避坑", sortNo: 50},
		{name: "找工作", description: "招聘、求职、兼职、远程工作、翻译岗位与经验交流", sortNo: 60},
		{name: "影视交流", description: "电影、电视剧、短剧、音乐、字幕与文化讨论", sortNo: 70},
		{name: "游戏交流", description: "手游、端游、组队、攻略、赛事与玩家交流", sortNo: 80},
		{name: "文化与生活", description: "文化、旅行、留学、签证与日常生活", sortNo: 90},
		{name: "闲聊", description: "轻松聊天、日常分享、心情记录与社区话题", sortNo: 100},
		{name: "语伴交流", description: "语言交换经验、聊天话题与学习打卡", sortNo: 110},
	}

	db := sqls.DB()
	now := dates.NowTimestamp()
	for _, seed := range seeds {
		category := &models.Category{}
		err := db.Where("name = ?", seed.name).First(category).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			category = &models.Category{
				ParentId:    0,
				Name:        seed.name,
				Type:        constants.CategoryTypeNormal,
				Description: seed.description,
				Logo:        "",
				SortNo:      seed.sortNo,
				Status:      constants.StatusOk,
				CreateTime:  now,
			}
			if createErr := db.Create(category).Error; createErr != nil {
				return createErr
			}
			continue
		}
		if err != nil {
			return err
		}

		// Keep administrator choices such as status and hierarchy, while making
		// sure the standard description and ordering are available.
		if updateErr := db.Model(category).Updates(map[string]any{
			"description": seed.description,
			"sort_no":     seed.sortNo,
		}).Error; updateErr != nil {
			return updateErr
		}
	}
	return nil
}
