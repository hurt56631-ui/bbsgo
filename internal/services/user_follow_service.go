package services

import (
	"bbs-go/internal/cache"
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/event"
	"bbs-go/internal/pkg/locales"
	"bbs-go/internal/repositories"
	"errors"

	"bbs-go/internal/pkg/params"

	"github.com/emirpasic/gods/sets/hashset"
	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var UserFollowService = newUserFollowService()

func newUserFollowService() *userFollowService {
	return &userFollowService{}
}

type userFollowService struct {
}

func (s *userFollowService) Get(id int64) *models.UserFollow {
	return repositories.UserFollowRepository.Get(sqls.DB(), id)
}

func (s *userFollowService) Take(where ...interface{}) *models.UserFollow {
	return repositories.UserFollowRepository.Take(sqls.DB(), where...)
}

func (s *userFollowService) Find(cnd *sqls.Cnd) []models.UserFollow {
	return repositories.UserFollowRepository.Find(sqls.DB(), cnd)
}

func (s *userFollowService) FindOne(cnd *sqls.Cnd) *models.UserFollow {
	return repositories.UserFollowRepository.FindOne(sqls.DB(), cnd)
}

func (s *userFollowService) FindPageByParams(params *params.QueryParams) (list []models.UserFollow, paging *sqls.Paging) {
	return repositories.UserFollowRepository.FindPageByParams(sqls.DB(), params)
}

func (s *userFollowService) FindPageByCnd(cnd *sqls.Cnd) (list []models.UserFollow, paging *sqls.Paging) {
	return repositories.UserFollowRepository.FindPageByCnd(sqls.DB(), cnd)
}

func (s *userFollowService) Count(cnd *sqls.Cnd) int64 {
	return repositories.UserFollowRepository.Count(sqls.DB(), cnd)
}

func (s *userFollowService) Create(t *models.UserFollow) error {
	return repositories.UserFollowRepository.Create(sqls.DB(), t)
}

func (s *userFollowService) Update(t *models.UserFollow) error {
	return repositories.UserFollowRepository.Update(sqls.DB(), t)
}

func (s *userFollowService) Updates(id int64, columns map[string]interface{}) error {
	return repositories.UserFollowRepository.Updates(sqls.DB(), id, columns)
}

func (s *userFollowService) UpdateColumn(id int64, name string, value interface{}) error {
	return repositories.UserFollowRepository.UpdateColumn(sqls.DB(), id, name, value)
}

func (s *userFollowService) Delete(id int64) {
	repositories.UserFollowRepository.Delete(sqls.DB(), id)
}

func lockFollowUsers(tx *gorm.DB, userId, otherId int64) error {
	ids := []int64{userId, otherId}
	if ids[0] > ids[1] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	for _, id := range ids {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").Take(&user, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New(locales.Get("user.not_found"))
			}
			return err
		}
	}
	return nil
}

// reconcileFollowStatusesTx derives the redundant status column from the two
// actual relationship rows. Callers hold both user rows in deterministic order,
// so follow/unfollow requests for the same pair cannot leave asymmetric status.
func reconcileFollowStatusesTx(tx *gorm.DB, userId, otherId int64) error {
	var rows []models.UserFollow
	if err := tx.Where(
		"(user_id = ? AND other_id = ?) OR (user_id = ? AND other_id = ?)",
		userId, otherId, otherId, userId,
	).Find(&rows).Error; err != nil {
		return err
	}
	mutual := len(rows) == 2
	for _, row := range rows {
		status := constants.FollowStatusFollow
		if mutual {
			status = constants.FollowStatusBoth
		}
		if row.Status == status {
			continue
		}
		if err := tx.Model(&models.UserFollow{}).Where("id = ?", row.Id).
			UpdateColumn("status", status).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *userFollowService) Follow(userId, otherId int64) error {
	if userId == otherId {
		return nil
	}

	created := false
	err := sqls.DB().Transaction(func(tx *gorm.DB) error {
		// Serialize all mutations for one user pair before touching the unique
		// relationship rows. This removes the A->B/B->A deadlock and the later
		// follow-vs-unfollow status race in one place.
		if err := lockFollowUsers(tx, userId, otherId); err != nil {
			return err
		}

		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.UserFollow{
			UserId:     userId,
			OtherId:    otherId,
			Status:     constants.FollowStatusFollow,
			CreateTime: dates.NowTimestamp(),
		})
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected > 0
		if created {
			if err := tx.Model(&models.User{}).Where("id = ?", userId).
				UpdateColumn("follow_count", gorm.Expr("follow_count + 1")).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.User{}).Where("id = ?", otherId).
				UpdateColumn("fans_count", gorm.Expr("fans_count + 1")).Error; err != nil {
				return err
			}
		}
		return reconcileFollowStatusesTx(tx, userId, otherId)
	})
	if err != nil {
		return err
	}
	if !created {
		return nil
	}

	cache.UserCache.Invalidate(userId)
	cache.UserCache.Invalidate(otherId)
	UserService.invalidateInfoCache(userId)
	UserService.invalidateInfoCache(otherId)

	event.Send(event.FollowEvent{UserId: userId, OtherId: otherId})
	return nil
}

func (s *userFollowService) UnFollow(userId, otherId int64) error {
	if userId == otherId {
		return nil
	}

	deleted := false
	err := sqls.DB().Transaction(func(tx *gorm.DB) error {
		if err := lockFollowUsers(tx, userId, otherId); err != nil {
			return err
		}
		result := tx.Where("user_id = ? AND other_id = ?", userId, otherId).
			Delete(&models.UserFollow{})
		if result.Error != nil {
			return result.Error
		}
		deleted = result.RowsAffected > 0
		if deleted {
			if err := tx.Model(&models.User{}).Where("id = ?", userId).UpdateColumn(
				"follow_count", gorm.Expr("CASE WHEN follow_count > 0 THEN follow_count - 1 ELSE 0 END"),
			).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.User{}).Where("id = ?", otherId).UpdateColumn(
				"fans_count", gorm.Expr("CASE WHEN fans_count > 0 THEN fans_count - 1 ELSE 0 END"),
			).Error; err != nil {
				return err
			}
		}
		return reconcileFollowStatusesTx(tx, userId, otherId)
	})
	if err != nil {
		return err
	}
	if !deleted {
		return nil
	}

	cache.UserCache.Invalidate(userId)
	cache.UserCache.Invalidate(otherId)
	UserService.invalidateInfoCache(userId)
	UserService.invalidateInfoCache(otherId)

	event.Send(event.UnFollowEvent{UserId: userId, OtherId: otherId})
	return nil
}

// GetFans 粉丝列表
func (s *userFollowService) GetFans(userId int64, cursor int64, limit int) (itemList []int64, nextCursor int64, hasMore bool) {
	cnd := sqls.NewCnd().Eq("other_id", userId)
	if cursor > 0 {
		cnd.Lt("id", cursor)
	}
	cnd.Desc("id").Limit(limit)
	list := repositories.UserFollowRepository.Find(sqls.DB(), cnd)

	if len(list) > 0 {
		nextCursor = list[len(list)-1].Id
		hasMore = len(list) >= limit
		for _, e := range list {
			itemList = append(itemList, e.UserId)
		}
	} else {
		nextCursor = cursor
	}
	return
}

// GetFollows 关注列表
func (s *userFollowService) GetFollows(userId int64, cursor int64, limit int) (itemList []int64, nextCursor int64, hasMore bool) {
	cnd := sqls.NewCnd().Eq("user_id", userId)
	if cursor > 0 {
		cnd.Lt("id", cursor)
	}
	cnd.Desc("id").Limit(limit)
	list := repositories.UserFollowRepository.Find(sqls.DB(), cnd)

	if len(list) > 0 {
		nextCursor = list[len(list)-1].Id
		hasMore = len(list) >= limit
		for _, e := range list {
			itemList = append(itemList, e.OtherId)
		}
	} else {
		nextCursor = cursor
	}
	return
}

// ScanFans 扫描粉丝
func (s *userFollowService) ScanFans(userId int64, handle func(fansId int64)) {
	var cursor int64 = 0
	for {
		list := s.Find(sqls.NewCnd().Eq("other_id", userId).Gt("id", cursor).Asc("id").Limit(100))
		if len(list) == 0 {
			break
		}
		cursor = list[len(list)-1].Id
		for _, item := range list {
			handle(item.UserId)
		}
	}
}

// ScanFansBatch streams fan IDs in bounded batches so callers can use one
// database insert per batch instead of one insert per follower.
func (s *userFollowService) ScanFansBatch(userId int64, batchSize int, handle func(fansIds []int64) error) error {
	if batchSize <= 0 {
		batchSize = 500
	}
	var cursor int64
	for {
		var list []models.UserFollow
		if err := sqls.DB().Model(&models.UserFollow{}).
			Select("id, user_id").
			Where("other_id = ? AND id > ?", userId, cursor).
			Order("id ASC").Limit(batchSize).Find(&list).Error; err != nil {
			return err
		}
		if len(list) == 0 {
			return nil
		}
		cursor = list[len(list)-1].Id
		fansIds := make([]int64, 0, len(list))
		for _, item := range list {
			fansIds = append(fansIds, item.UserId)
		}
		if err := handle(fansIds); err != nil {
			return err
		}
	}
}

// ScanFollowed 扫描关注的用户
func (s *userFollowService) ScanFollowed(userId int64, handle func(followUserId int64)) {
	var cursor int64 = 0
	for {
		list := s.Find(sqls.NewCnd().Eq("user_id", userId).Gt("id", cursor).Asc("id").Limit(100))
		if len(list) == 0 {
			break
		}
		cursor = list[len(list)-1].Id
		for _, item := range list {
			handle(item.OtherId)
		}
	}
}

func (s *userFollowService) IsFollowed(userId, otherId int64) bool {
	if userId == otherId {
		return false
	}
	set := s.IsFollowedUsers(userId, otherId)
	return set.Contains(otherId)
}

func (s *userFollowService) IsFollowedUsers(userId int64, otherIds ...int64) hashset.Set {
	set := hashset.New()
	list := s.Find(sqls.NewCnd().Eq("user_id", userId).In("other_id", otherIds))
	for _, follow := range list {
		set.Add(follow.OtherId)
	}
	return *set
}
