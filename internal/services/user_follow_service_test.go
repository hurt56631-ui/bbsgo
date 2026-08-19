package services

import (
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"testing"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
)

func setupUserFollowServiceTestDB(t *testing.T) {
	t.Helper()
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.UserFollow{}); err != nil {
		t.Fatalf("auto migrate follow: %v", err)
	}
}

func TestUserFollowService_IdempotentCounters(t *testing.T) {
	setupUserFollowServiceTestDB(t)
	now := dates.NowTimestamp()
	from := mustCreateUser(t, now)
	to := mustCreateUser(t, now+1)

	if err := UserFollowService.Follow(from.Id, to.Id); err != nil {
		t.Fatalf("first follow: %v", err)
	}
	if err := UserFollowService.Follow(from.Id, to.Id); err != nil {
		t.Fatalf("duplicate follow: %v", err)
	}

	var rows int64
	if err := sqls.DB().Model(&models.UserFollow{}).Count(&rows).Error; err != nil {
		t.Fatalf("count follows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected one follow row, got %d", rows)
	}
	var gotFrom, gotTo models.User
	_ = sqls.DB().First(&gotFrom, from.Id).Error
	_ = sqls.DB().First(&gotTo, to.Id).Error
	if gotFrom.FollowCount != 1 || gotTo.FansCount != 1 {
		t.Fatalf("unexpected counts follow=%d fans=%d", gotFrom.FollowCount, gotTo.FansCount)
	}

	if err := UserFollowService.UnFollow(from.Id, to.Id); err != nil {
		t.Fatalf("first unfollow: %v", err)
	}
	if err := UserFollowService.UnFollow(from.Id, to.Id); err != nil {
		t.Fatalf("duplicate unfollow: %v", err)
	}
	_ = sqls.DB().First(&gotFrom, from.Id).Error
	_ = sqls.DB().First(&gotTo, to.Id).Error
	if gotFrom.FollowCount != 0 || gotTo.FansCount != 0 {
		t.Fatalf("unexpected counts after unfollow follow=%d fans=%d", gotFrom.FollowCount, gotTo.FansCount)
	}
}

func TestUserFollowService_ReciprocalStatus(t *testing.T) {
	setupUserFollowServiceTestDB(t)
	now := dates.NowTimestamp()
	a := mustCreateUser(t, now)
	b := mustCreateUser(t, now+1)
	if err := UserFollowService.Follow(a.Id, b.Id); err != nil {
		t.Fatalf("a follows b: %v", err)
	}
	if err := UserFollowService.Follow(b.Id, a.Id); err != nil {
		t.Fatalf("b follows a: %v", err)
	}
	var follows []models.UserFollow
	if err := sqls.DB().Order("id ASC").Find(&follows).Error; err != nil {
		t.Fatalf("load follows: %v", err)
	}
	if len(follows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(follows))
	}
	for _, follow := range follows {
		if follow.Status != constants.FollowStatusBoth {
			t.Fatalf("expected mutual status, got %d", follow.Status)
		}
	}
}

func TestUserFollowService_MissingTargetRollsBack(t *testing.T) {
	setupUserFollowServiceTestDB(t)
	from := mustCreateUser(t, dates.NowTimestamp())
	if err := UserFollowService.Follow(from.Id, 999999); err == nil {
		t.Fatalf("expected missing target error")
	}
	var rows int64
	_ = sqls.DB().Model(&models.UserFollow{}).Count(&rows).Error
	if rows != 0 {
		t.Fatalf("expected transaction rollback, rows=%d", rows)
	}
	var got models.User
	_ = sqls.DB().First(&got, from.Id).Error
	if got.FollowCount != 0 {
		t.Fatalf("expected rolled back counter, got %d", got.FollowCount)
	}
}

func TestUserFollowService_RepairsReciprocalStatusInsideTransaction(t *testing.T) {
	setupUserFollowServiceTestDB(t)
	now := dates.NowTimestamp()
	a := mustCreateUser(t, now)
	b := mustCreateUser(t, now+1)
	if err := UserFollowService.Follow(a.Id, b.Id); err != nil {
		t.Fatalf("a follows b: %v", err)
	}
	if err := UserFollowService.Follow(b.Id, a.Id); err != nil {
		t.Fatalf("b follows a: %v", err)
	}

	// Simulate an old asymmetric row left by the pre-fix implementation. An
	// idempotent follow must reconcile it without changing counters.
	if err := sqls.DB().Model(&models.UserFollow{}).
		Where("user_id = ? AND other_id = ?", b.Id, a.Id).
		UpdateColumn("status", constants.FollowStatusFollow).Error; err != nil {
		t.Fatalf("corrupt reciprocal status: %v", err)
	}
	if err := UserFollowService.Follow(a.Id, b.Id); err != nil {
		t.Fatalf("repair duplicate follow: %v", err)
	}
	var rows []models.UserFollow
	if err := sqls.DB().Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load follows: %v", err)
	}
	for _, row := range rows {
		if row.Status != constants.FollowStatusBoth {
			t.Fatalf("expected repaired mutual status, got %d", row.Status)
		}
	}

	if err := UserFollowService.UnFollow(a.Id, b.Id); err != nil {
		t.Fatalf("a unfollows b: %v", err)
	}
	var remaining models.UserFollow
	if err := sqls.DB().Where("user_id = ? AND other_id = ?", b.Id, a.Id).Take(&remaining).Error; err != nil {
		t.Fatalf("load remaining follow: %v", err)
	}
	if remaining.Status != constants.FollowStatusFollow {
		t.Fatalf("expected one-way status after unfollow, got %d", remaining.Status)
	}
}
