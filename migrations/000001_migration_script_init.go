package migrations

import (
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/dto"
	"bbs-go/internal/pkg/config"
	"bbs-go/internal/repositories"

	"encoding/json"
	"strconv"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
)

type roleSeed struct {
	ID     int64
	Type   int
	Name   string
	Code   string
	SortNo int
	Remark string
	Status int
}

type categorySeed struct {
	ID            int64
	Name          string
	Description   string
	Logo          string
	SortNo        int
	Status        int
	AdminOnlyPost bool
}

type sysConfigSeed struct {
	Key         string
	Value       interface{}
	Name        string
	Description string
}

type seedData struct {
	Roles      []roleSeed
	Categories []categorySeed
	SysConfigs []sysConfigSeed
}

func migrate_init() error {
	return sqls.DB().Transaction(func(tx *gorm.DB) error {
		now := dates.NowTimestamp()
		seed := seedForLanguage()

		for _, r := range seed.Roles {
			existing := repositories.RoleRepository.Take(tx, "code = ?", r.Code)
			if existing != nil {
				existing.Type = r.Type
				existing.Name = r.Name
				existing.SortNo = r.SortNo
				existing.Remark = r.Remark
				existing.Status = r.Status
				existing.UpdateTime = now
				if err := repositories.RoleRepository.Update(tx, existing); err != nil {
					return err
				}
				continue
			}

			role := &models.Role{
				Model:      models.Model{Id: r.ID},
				Type:       r.Type,
				Name:       r.Name,
				Code:       r.Code,
				SortNo:     r.SortNo,
				Remark:     r.Remark,
				Status:     r.Status,
				CreateTime: now,
				UpdateTime: now,
			}
			if err := repositories.RoleRepository.Create(tx, role); err != nil {
				return err
			}
		}

		categoryIDMap := make(map[int64]int64)
		for _, n := range seed.Categories {
			existing := repositories.CategoryRepository.Take(tx, "name = ?", n.Name)
			if existing != nil {
				existing.Description = n.Description
				existing.Logo = n.Logo
				existing.SortNo = n.SortNo
				existing.Status = n.Status
				existing.AdminOnlyPost = n.AdminOnlyPost
				if err := repositories.CategoryRepository.Update(tx, existing); err != nil {
					return err
				}
				categoryIDMap[n.ID] = existing.Id
				continue
			}

			category := &models.Category{
				Model:         models.Model{Id: n.ID},
				Name:          n.Name,
				Description:   n.Description,
				Logo:          n.Logo,
				SortNo:        n.SortNo,
				Status:        n.Status,
				AdminOnlyPost: n.AdminOnlyPost,
				CreateTime:    now,
			}
			if err := repositories.CategoryRepository.Create(tx, category); err != nil {
				return err
			}
			categoryIDMap[n.ID] = category.Id
		}

		for _, c := range seed.SysConfigs {
			existing := repositories.SysConfigRepository.GetByKey(tx, c.Key)
			if existing != nil {
				existing.Value = toConfigValue(c.Value)
				existing.Name = c.Name
				existing.Description = c.Description
				existing.UpdateTime = now
				if err := repositories.SysConfigRepository.Update(tx, existing); err != nil {
					return err
				}
				continue
			}

			cfg := &models.SysConfig{
				Key:         c.Key,
				Value:       toConfigValue(c.Value),
				Name:        c.Name,
				Description: c.Description,
				CreateTime:  now,
				UpdateTime:  now,
			}
			if err := repositories.SysConfigRepository.Create(tx, cfg); err != nil {
				return err
			}
		}

		// ensure defaultCategoryId sys config points to created category id
		if categoryID := categoryIDMap[2]; categoryID > 0 {
			if cfg := repositories.SysConfigRepository.GetByKey(tx, constants.SysConfigDefaultCategoryId); cfg != nil {
				cfg.Value = strconv.FormatInt(categoryID, 10)
				cfg.UpdateTime = now
				if err := repositories.SysConfigRepository.Update(tx, cfg); err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func toConfigValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		if b, err := json.Marshal(val); err == nil {
			return string(b)
		}
	}
	return ""
}

func seedForLanguage() seedData {
	lang := config.Instance.Language
	if !lang.IsValid() {
		lang = config.DefaultLanguage
	}
	if lang == config.LanguageEnUS {
		return seedData{
			Roles: []roleSeed{
				{ID: 1, Type: constants.RoleTypeSystem, Name: "Owner", Code: constants.RoleOwner, SortNo: 0, Remark: "Owner with highest privileges", Status: constants.StatusOk},
			},
			Categories: []categorySeed{
				{ID: 1, Name: "Announcements", Description: "Product news, community rules and safety notices", Logo: "", SortNo: 10, Status: constants.StatusOk, AdminOnlyPost: true},
				{ID: 2, Name: "Learning", Description: "Pronunciation, grammar, speaking and learning methods", Logo: "", SortNo: 20, Status: constants.StatusOk},
				{ID: 3, Name: "Resources", Description: "Courses, dictionaries, websites and learning tools", Logo: "", SortNo: 30, Status: constants.StatusOk},
				{ID: 4, Name: "Culture & Life", Description: "Culture, travel, study abroad and daily life", Logo: "", SortNo: 40, Status: constants.StatusOk},
				{ID: 5, Name: "Language Partners", Description: "Language exchange experiences, topics and study check-ins", Logo: "", SortNo: 50, Status: constants.StatusOk},
			},
			SysConfigs: []sysConfigSeed{
				{Key: constants.SysConfigSiteTitle, Value: "Talkami Community", Name: "Site Title", Description: "Site Title"},
				{Key: constants.SysConfigSiteDescription, Value: "A language learning and cultural exchange community", Name: "Site Description", Description: "Site Description"},
				{Key: constants.SysConfigBaseURL, Value: "/", Name: "Site URL", Description: "Site URL"},
				{Key: constants.SysConfigSiteKeywords, Value: []string{"Talkami", "language learning", "language exchange"}, Name: "Site Keywords", Description: "Site Keywords"},
				{Key: constants.SysConfigSiteNavs, Value: []map[string]string{
					{"title": "Community", "url": "/topics"},
				}, Name: "Site Navigation", Description: "Site Navigation"},
				{Key: constants.SysConfigDefaultCategoryId, Value: "2", Name: "Default Category", Description: "Default Category"},
				{Key: constants.SysConfigTokenExpireDays, Value: "365", Name: "User Login Validity Period (Days)", Description: "User Login Validity Period (Days)"},
				{Key: constants.SysConfigUrlRedirect, Value: "false"},
				{Key: constants.SysConfigEnableHideContent, Value: "false"},
				{Key: constants.SysConfigSiteLogo, Value: ""},
				{Key: constants.SysConfigSiteNotification, Value: ""},
				{Key: constants.SysConfigRecommendTags, Value: ""},
				{Key: constants.SysConfigModules, Value: map[string]bool{"tweet": false, "topic": true, "qa": false, "article": false}},
				{Key: constants.SysConfigSmtpConfig, Value: dto.SmtpConfig{}},
				{Key: constants.SysConfigUploadConfig, Value: dto.UploadConfig{
					EnableUploadMethod: dto.Local,
					AliyunOss:          dto.AliyunOssUploadConfig{},
					TencentCos:         dto.TencentCosUploadConfig{},
					AwsS3:              dto.AwsS3UploadConfig{},
				}},
			},
		}
	}

	return seedData{
		Roles: []roleSeed{
			{ID: 1, Type: constants.RoleTypeSystem, Name: "超级管理员", Code: constants.RoleOwner, SortNo: 0, Remark: "超级管理员拥有最高权限", Status: constants.StatusOk},
		},
		Categories: []categorySeed{
			{ID: 1, Name: "官方公告", Description: "产品公告、功能更新、社区规则与安全提醒", Logo: "", SortNo: 10, Status: constants.StatusOk, AdminOnlyPost: true},
			{ID: 2, Name: "学习交流", Description: "发音、语法、口语、翻译与学习方法", Logo: "", SortNo: 20, Status: constants.StatusOk},
			{ID: 3, Name: "学习资源", Description: "课程、词典、网站、工具与学习资料整理", Logo: "", SortNo: 30, Status: constants.StatusOk},
			{ID: 4, Name: "文化与生活", Description: "文化、旅行、留学、签证与日常生活", Logo: "", SortNo: 40, Status: constants.StatusOk},
			{ID: 5, Name: "语伴交流", Description: "语言交换经验、聊天话题与学习打卡", Logo: "", SortNo: 50, Status: constants.StatusOk},
		},
		SysConfigs: []sysConfigSeed{
			{Key: constants.SysConfigSiteTitle, Value: "Talkami 社区", Name: "站点标题", Description: "站点标题"},
			{Key: constants.SysConfigSiteDescription, Value: "语言学习、文化交流与语伴经验社区", Name: "站点描述", Description: "站点描述"},
			{Key: constants.SysConfigBaseURL, Value: "/", Name: "网站URL", Description: "网站URL"},
			{Key: constants.SysConfigSiteKeywords, Value: []string{"Talkami", "语言学习", "语伴", "文化交流"}, Name: "站点关键字", Description: "站点关键字"},
			{Key: constants.SysConfigSiteNavs, Value: []map[string]string{
				{"title": "社区", "url": "/topics"},
			}, Name: "站点导航", Description: "站点导航"},
			{Key: constants.SysConfigDefaultCategoryId, Value: "2", Name: "默认节点", Description: "默认节点"},
			{Key: constants.SysConfigTokenExpireDays, Value: "365", Name: "用户登录有效期(天)", Description: "用户登录有效期(天)"},
			{Key: constants.SysConfigUrlRedirect, Value: "false"},
			{Key: constants.SysConfigEnableHideContent, Value: "false"},
			{Key: constants.SysConfigSiteLogo, Value: ""},
			{Key: constants.SysConfigSiteNotification, Value: ""},
			{Key: constants.SysConfigRecommendTags, Value: ""},
			{Key: constants.SysConfigModules, Value: map[string]bool{"tweet": false, "topic": true, "qa": false, "article": false}},
			{Key: constants.SysConfigSmtpConfig, Value: dto.SmtpConfig{}},
			{Key: constants.SysConfigUploadConfig, Value: dto.UploadConfig{
				EnableUploadMethod: dto.Local,
				AliyunOss: dto.AliyunOssUploadConfig{
					StyleSplitter: "@",
				},
				TencentCos: dto.TencentCosUploadConfig{},
				AwsS3:      dto.AwsS3UploadConfig{},
			}},
		},
	}
}
