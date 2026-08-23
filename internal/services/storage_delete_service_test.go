package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/dto"

	"github.com/mlogclub/simple/sqls"
)

func TestResolveOwnedObjectKeyAcrossBackends(t *testing.T) {
	cfg := dto.UploadConfig{
		AliyunOss: dto.AliyunOssUploadConfig{
			Host: "https://cdn.example.com/forum", Bucket: "bucket", Endpoint: "oss-cn.example.com",
		},
		TencentCos: dto.TencentCosUploadConfig{Bucket: "bucket-123", Region: "ap-shanghai"},
		AwsS3:      dto.AwsS3UploadConfig{Bucket: "bucket", Region: "ap-northeast-1"},
	}

	tests := []struct {
		name   string
		method dto.UploadMethod
		raw    string
		want   string
		ok     bool
	}{
		{"local relative", dto.Local, "/res/uploads/images/2026/a.webp", "images/2026/a.webp", true},
		{"aliyun custom host", dto.AliyunOss, "https://cdn.example.com/forum/images/2026/a.webp?x=1", "images/2026/a.webp", true},
		{"tencent canonical", dto.TencentCos, "https://bucket-123.cos.ap-shanghai.myqcloud.com/images/2026/a.webp", "images/2026/a.webp", true},
		{"s3 canonical", dto.AwsS3, "https://bucket.s3.ap-northeast-1.amazonaws.com/images/2026/a.webp", "images/2026/a.webp", true},
		{"wrong s3 host", dto.AwsS3, "https://other.s3.ap-northeast-1.amazonaws.com/images/2026/a.webp", "", false},
		{"same bucket outside forum namespace", dto.AwsS3, "https://bucket.s3.ap-northeast-1.amazonaws.com/backups/db.sql", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveOwnedObjectKey(cfg, tc.method, tc.raw)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("got key=%q ok=%v, want key=%q ok=%v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestNormalizeTangSengForumVoicePath(t *testing.T) {
	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"voice:file/preview/common/forum/u/voice.m4a|8|1,2", "common/forum/u/voice.m4a", true},
		{"voice:/v1/file/preview/common/forum/u/voice.m4a?filename=x|8|1,2", "common/forum/u/voice.m4a", true},
		{"voice:https://api.example.com/v1/file/preview/common/forum/u/voice.m4a?filename=x|8|1,2", "common/forum/u/voice.m4a", true},
		{"voice:file/preview/common/avatar/u.png|1|", "", false},
		{"voice:file/preview/common/forum/../avatar/u.png|1|", "", false},
		{"plain text", "", false},
	}
	for _, tc := range tests {
		got, ok := normalizeTangSengForumVoicePath(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("raw=%q got=%q ok=%v want=%q ok=%v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDeletedCommentDoesNotProtectStorageObject(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Comment{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	needle := "common/forum/u/voice.m4a"
	deleted := &models.Comment{
		UserId: 1, EntityType: constants.EntityTopic, EntityId: 1,
		Content: "voice:file/preview/" + needle + "|8|", ContentType: constants.ContentTypeText,
		Status: constants.StatusDeleted,
	}
	if err := sqls.DB().Create(deleted).Error; err != nil {
		t.Fatalf("create deleted comment: %v", err)
	}
	referenced, err := countReferenceSubstring(needle, []referenceField{{&models.Comment{}, "content", true}})
	if err != nil {
		t.Fatalf("reference check: %v", err)
	}
	if referenced {
		t.Fatalf("deleted comment must not protect object")
	}

	active := &models.Comment{
		UserId: 2, EntityType: constants.EntityTopic, EntityId: 1,
		Content: "voice:file/preview/" + needle + "|8|", ContentType: constants.ContentTypeText,
		Status: constants.StatusOk,
	}
	if err := sqls.DB().Create(active).Error; err != nil {
		t.Fatalf("create active comment: %v", err)
	}
	referenced, err = countReferenceSubstring(needle, []referenceField{{&models.Comment{}, "content", true}})
	if err != nil {
		t.Fatalf("reference check active: %v", err)
	}
	if !referenced {
		t.Fatalf("active shared reference must protect object")
	}
}

func TestTangSengMediaDeleteClientRequiresConfirmedDeletion(t *testing.T) {
	secret := strings.Repeat("s", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(r.Header.Get("X-Forum-Timestamp")))
		_, _ = mac.Write([]byte("\n"))
		_, _ = mac.Write(body)
		if !hmac.Equal(mac.Sum(nil), mustDecodeHexForTest(t, r.Header.Get("X-Forum-Signature"))) {
			t.Fatalf("invalid signature")
		}
		if r.URL.Path != "/v1/forum/media/delete" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deleted":1}`))
	}))
	defer server.Close()

	t.Setenv("BBSGO_TANGSENG_API_URL", server.URL)
	t.Setenv("BBSGO_TALKAMI_HMAC_SECRET", secret)
	client := newStorageDeleteService()
	if err := client.deleteTangSengFile("common/forum/u/voice.m4a"); err != nil {
		t.Fatalf("delete client failed: %v", err)
	}
}

func mustDecodeHexForTest(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	return decoded
}

func TestFindReferenceSubstringsBatchesMultipleKeys(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Comment{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	active := &models.Comment{
		UserId: 1, EntityType: constants.EntityTopic, EntityId: 1,
		Content: "voice:file/preview/common/forum/u/keep.m4a|8|", ContentType: constants.ContentTypeText,
		Status: constants.StatusOk,
	}
	if err := sqls.DB().Create(active).Error; err != nil {
		t.Fatalf("create active comment: %v", err)
	}
	deleted := &models.Comment{
		UserId: 2, EntityType: constants.EntityTopic, EntityId: 1,
		Content: "voice:file/preview/common/forum/u/deleted.m4a|8|", ContentType: constants.ContentTypeText,
		Status: constants.StatusDeleted,
	}
	if err := sqls.DB().Create(deleted).Error; err != nil {
		t.Fatalf("create deleted comment: %v", err)
	}

	refs, err := findReferenceSubstrings(
		[]string{"common/forum/u/keep.m4a", "common/forum/u/deleted.m4a", "common/forum/u/missing.m4a"},
		[]referenceField{{&models.Comment{}, "content", true}},
	)
	if err != nil {
		t.Fatalf("batch reference lookup: %v", err)
	}
	if _, ok := refs["common/forum/u/keep.m4a"]; !ok {
		t.Fatalf("active object was not protected")
	}
	if _, ok := refs["common/forum/u/deleted.m4a"]; ok {
		t.Fatalf("deleted object must not be protected")
	}
	if _, ok := refs["common/forum/u/missing.m4a"]; ok {
		t.Fatalf("missing object unexpectedly marked referenced")
	}
}

func TestResolveOwnedObjectRejectsParentTraversal(t *testing.T) {
	cfg := dto.UploadConfig{}
	if key, ok := resolveOwnedObjectKey(cfg, dto.Local, "/res/uploads/../private/secret.dat"); ok || key != "" {
		t.Fatalf("parent traversal must be rejected, key=%q ok=%v", key, ok)
	}
	if key, ok := resolveOwnedObjectKey(cfg, dto.Local, `/res/uploads/..\\private\\secret.dat`); ok || key != "" {
		t.Fatalf("backslash parent traversal must be rejected, key=%q ok=%v", key, ok)
	}
}

func TestForumStorageReferenceFieldsProtectCrossFeatureImages(t *testing.T) {
	protected := map[string]bool{}
	for _, field := range forumStorageReferenceFields() {
		switch field.model.(type) {
		case *models.User:
			if field.field == "avatar" || field.field == "background_image" {
				protected["user:"+field.field] = true
			}
		case *models.ThirdUser:
			if field.field == "avatar" {
				protected["third_user:avatar"] = true
			}
		case *models.Category:
			if field.field == "logo" {
				protected["category:logo"] = true
			}
		case *models.Badge:
			if field.field == "icon" {
				protected["badge:icon"] = true
			}
		}
	}
	for _, key := range []string{
		"user:avatar", "user:background_image", "third_user:avatar", "category:logo", "badge:icon",
	} {
		if !protected[key] {
			t.Fatalf("missing cross-feature storage reference protection for %s", key)
		}
	}
}
