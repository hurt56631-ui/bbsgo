package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"bbs-go/internal/cache"
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/search"

	"github.com/golang-jwt/jwt/v4"
	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/common/strs"
	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TalkamiAuthSource = "talkami"

	defaultTalkamiIssuer        = "tangsengdaodao"
	defaultTalkamiAudience      = "bbs-go-forum"
	defaultTalkamiMaxTokenTTL   = 15 * time.Minute
	defaultTalkamiSessionTTL    = time.Hour
	minTalkamiSessionTTL        = 5 * time.Minute
	maxTalkamiSessionTTL        = 7 * 24 * time.Hour
	maxTalkamiIdentityTokenSize = 16 * 1024
	talkamiClockSkew            = 30 * time.Second
)

var (
	TalkamiAuthService = newTalkamiAuthService()

	ErrTalkamiAuthDisabled = errors.New("唐僧统一登录尚未启用")
	ErrTalkamiTokenInvalid = errors.New("论坛登录凭证无效或已过期")
	ErrTalkamiTokenUsed    = errors.New("论坛登录凭证已使用，请重新获取")
	ErrTalkamiUserDisabled = errors.New("当前账号不可使用论坛")
)

type talkamiAuthService struct{}

type talkamiConfig struct {
	secret      string
	issuer      string
	audience    string
	maxTokenTTL time.Duration
	sessionTTL  time.Duration
}

type TalkamiClaims struct {
	jwt.RegisteredClaims

	UID               string   `json:"uid"`
	Nickname          string   `json:"nickname"`
	Avatar            string   `json:"avatar"`
	Sex               int      `json:"sex"`
	Description       string   `json:"description"`
	CountryCode       string   `json:"country_code"`
	Country           string   `json:"country"`
	NativeLanguages   []string `json:"native_languages"`
	LearningLanguages []string `json:"learning_languages"`
	ProfileVersion    int64    `json:"profile_version"`
	UserStatus        int      `json:"user_status"`
	IsDestroy         int      `json:"is_destroy"`
}

type TalkamiExchangeResult struct {
	User      *models.User
	Token     string
	ExpiresIn int64
	ExpiresAt int64
}

func newTalkamiAuthService() *talkamiAuthService {
	return &talkamiAuthService{}
}

func (s *talkamiAuthService) Enabled() bool {
	return envBoolDefault("BBSGO_TALKAMI_ENABLED", false)
}

func (s *talkamiAuthService) Exclusive() bool {
	return s.Enabled() && envBoolDefault("BBSGO_TALKAMI_EXCLUSIVE_AUTH", true)
}

func (s *talkamiAuthService) IsManagedUser(user *models.User) bool {
	return user != nil && user.AuthSource == TalkamiAuthSource && user.ExternalUID.Valid && strings.TrimSpace(user.ExternalUID.String) != ""
}

func (s *talkamiAuthService) Exchange(identityToken string) (*TalkamiExchangeResult, error) {
	if !s.Enabled() {
		return nil, ErrTalkamiAuthDisabled
	}
	cfg, err := loadTalkamiConfig()
	if err != nil {
		return nil, err
	}

	claims, err := parseAndValidateTalkamiToken(identityToken, cfg)
	if err != nil {
		return nil, err
	}

	profile := normalizeTalkamiProfile(claims)
	now := time.Now()
	nowTimestamp := dates.Timestamp(now)
	var (
		user        *models.User
		forumToken  string
		expiresAt   int64
		userChanged bool
	)

	err = sqls.DB().Transaction(func(tx *gorm.DB) error {
		nonce := &models.TalkamiTokenNonce{
			JTI:         claims.ID,
			ExternalUID: profile.uid,
			ExpiresAt:   claims.ExpiresAt.Time.Unix(),
			CreateTime:  now.Unix(),
		}
		if createErr := tx.Create(nonce).Error; createErr != nil {
			if isDuplicateDBError(createErr) {
				return ErrTalkamiTokenUsed
			}
			return fmt.Errorf("保存论坛登录凭证状态失败: %w", createErr)
		}

		candidate := &models.User{
			AuthSource:           TalkamiAuthSource,
			ExternalUID:          sql.NullString{String: profile.uid, Valid: true},
			Nickname:             profile.nickname,
			Avatar:               profile.avatar,
			Gender:               profile.gender,
			Description:          profile.description,
			CountryCode:          profile.countryCode,
			Country:              profile.country,
			NativeLanguages:      profile.nativeLanguages,
			LearningLanguages:    profile.learningLanguages,
			SourceProfileVersion: claims.ProfileVersion,
			Status:               constants.StatusOk,
			CreateTime:           nowTimestamp,
			UpdateTime:           nowTimestamp,
		}

		createResult := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "external_uid"}},
			DoNothing: true,
		}).Create(candidate)
		if createResult.Error != nil {
			return fmt.Errorf("创建论坛用户失败: %w", createResult.Error)
		}

		current := &models.User{}
		if queryErr := tx.Where("external_uid = ?", profile.uid).First(current).Error; queryErr != nil {
			return fmt.Errorf("读取论坛用户映射失败: %w", queryErr)
		}
		if current.AuthSource != TalkamiAuthSource {
			return errors.New("论坛用户映射冲突")
		}
		if current.Status != constants.StatusOk {
			return ErrTalkamiUserDisabled
		}

		if current.CreateTime == nowTimestamp {
			// candidate uses this exact timestamp. A second concurrent first login may
			// also mark the user changed, which only causes a harmless index refresh.
			userChanged = true
		}
		if claims.ProfileVersion > current.SourceProfileVersion {
			// The version must advance even when the visible profile is unchanged.
			// Otherwise an older token carrying stale profile data could be replayed
			// later and still pass the source_profile_version condition.
			updates := map[string]any{
				"source_profile_version": claims.ProfileVersion,
			}
			profileChanged := !talkamiProfileMatchesUser(current, profile)
			if profileChanged {
				updates["nickname"] = profile.nickname
				updates["avatar"] = profile.avatar
				updates["gender"] = profile.gender
				updates["description"] = profile.description
				updates["country_code"] = profile.countryCode
				updates["country"] = profile.country
				updates["native_languages"] = profile.nativeLanguages
				updates["learning_languages"] = profile.learningLanguages
				updates["update_time"] = nowTimestamp
			}
			updateResult := tx.Model(&models.User{}).
				Where("id = ? AND source_profile_version < ?", current.Id, claims.ProfileVersion).
				Updates(updates)
			if updateResult.Error != nil {
				return fmt.Errorf("同步论坛用户资料失败: %w", updateResult.Error)
			}
			if updateResult.RowsAffected == 1 {
				if queryErr := tx.First(current, "id = ?", current.Id).Error; queryErr != nil {
					return fmt.Errorf("重新读取论坛用户失败: %w", queryErr)
				}
				userChanged = userChanged || profileChanged
			}
		}

		forumToken = strs.UUID()
		sessionExpiresAt := now.Add(cfg.sessionTTL)
		expiresAt = sessionExpiresAt.Unix() // API contract keeps Unix seconds.
		if tokenErr := tx.Create(&models.UserToken{
			Token:      forumToken,
			UserId:     current.Id,
			ExpiredAt:  dates.Timestamp(sessionExpiresAt),
			Status:     constants.StatusOk,
			CreateTime: nowTimestamp,
		}).Error; tokenErr != nil {
			return fmt.Errorf("创建论坛登录状态失败: %w", tokenErr)
		}

		user = current
		return nil
	})
	if err != nil {
		return nil, err
	}

	cache.UserCache.Invalidate(user.Id)
	if latest := UserService.Get(user.Id); latest != nil {
		user = latest
	}
	if userChanged {
		search.UpdateUserIndex(user)
	}

	return &TalkamiExchangeResult{
		User:      user,
		Token:     forumToken,
		ExpiresIn: int64(cfg.sessionTTL / time.Second),
		ExpiresAt: expiresAt,
	}, nil
}

// CleanupExpiredNonces deletes already-expired one-time SSO credentials after
// a retention window. Replay protection remains intact until the credential
// has been expired for the requested period.
func (s *talkamiAuthService) CleanupExpiredNonces(retainFor time.Duration) (int64, error) {
	if retainFor < 0 {
		retainFor = 0
	}
	cutoff := time.Now().Add(-retainFor).Unix()
	var total int64
	for {
		var ids []int64
		if err := sqls.DB().Model(&models.TalkamiTokenNonce{}).
			Where("expires_at < ?", cutoff).
			Order("id ASC").Limit(1000).Pluck("id", &ids).Error; err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		result := sqls.DB().Where("id IN ?", ids).Delete(&models.TalkamiTokenNonce{})
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
		if len(ids) < 1000 {
			return total, nil
		}
	}
}

func loadTalkamiConfig() (talkamiConfig, error) {
	secret := strings.TrimSpace(os.Getenv("BBSGO_TALKAMI_HMAC_SECRET"))
	if len(secret) < 32 {
		return talkamiConfig{}, errors.New("BBSGO_TALKAMI_HMAC_SECRET 至少需要 32 个字符")
	}
	issuer := strings.TrimSpace(os.Getenv("BBSGO_TALKAMI_ISSUER"))
	if issuer == "" {
		issuer = defaultTalkamiIssuer
	}
	audience := strings.TrimSpace(os.Getenv("BBSGO_TALKAMI_AUDIENCE"))
	if audience == "" {
		audience = defaultTalkamiAudience
	}

	maxTokenTTL, err := envDurationSeconds("BBSGO_TALKAMI_MAX_TOKEN_TTL_SECONDS", defaultTalkamiMaxTokenTTL)
	if err != nil || maxTokenTTL < time.Minute || maxTokenTTL > time.Hour {
		return talkamiConfig{}, errors.New("BBSGO_TALKAMI_MAX_TOKEN_TTL_SECONDS 必须在 60 到 3600 秒之间")
	}
	sessionTTL, err := envDurationSeconds("BBSGO_TALKAMI_SESSION_TTL_SECONDS", defaultTalkamiSessionTTL)
	if err != nil || sessionTTL < minTalkamiSessionTTL || sessionTTL > maxTalkamiSessionTTL {
		return talkamiConfig{}, fmt.Errorf("BBSGO_TALKAMI_SESSION_TTL_SECONDS 必须在 %d 到 %d 秒之间", int(minTalkamiSessionTTL/time.Second), int(maxTalkamiSessionTTL/time.Second))
	}

	return talkamiConfig{
		secret:      secret,
		issuer:      issuer,
		audience:    audience,
		maxTokenTTL: maxTokenTTL,
		sessionTTL:  sessionTTL,
	}, nil
}

func parseAndValidateTalkamiToken(raw string, cfg talkamiConfig) (*TalkamiClaims, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxTalkamiIdentityTokenSize {
		return nil, ErrTalkamiTokenInvalid
	}

	claims := &TalkamiClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		// RegisteredClaims.Valid() has no clock-skew support and would reject
		// a fresh token before the explicit checks below can apply the allowed skew.
		jwt.WithoutClaimsValidation(),
	)
	token, err := parser.ParseWithClaims(raw, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrTalkamiTokenInvalid
		}
		return []byte(cfg.secret), nil
	})
	if err != nil || token == nil || !token.Valid {
		return nil, ErrTalkamiTokenInvalid
	}

	now := time.Now()
	if claims.ExpiresAt == nil || claims.NotBefore == nil || claims.IssuedAt == nil || strings.TrimSpace(claims.ID) == "" || len(claims.ID) > 64 {
		return nil, ErrTalkamiTokenInvalid
	}
	if !claims.VerifyIssuer(cfg.issuer, true) || !claims.VerifyAudience(cfg.audience, true) {
		return nil, ErrTalkamiTokenInvalid
	}
	if claims.ExpiresAt.Time.Before(now) ||
		claims.NotBefore.Time.After(now.Add(talkamiClockSkew)) ||
		claims.IssuedAt.Time.After(now.Add(talkamiClockSkew)) ||
		claims.NotBefore.Time.After(claims.ExpiresAt.Time) ||
		claims.IssuedAt.Time.After(claims.ExpiresAt.Time) {
		return nil, ErrTalkamiTokenInvalid
	}
	if duration := claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time); duration <= 0 || duration > cfg.maxTokenTTL+talkamiClockSkew {
		return nil, ErrTalkamiTokenInvalid
	}

	claims.UID = strings.TrimSpace(claims.UID)
	if claims.UID == "" || len(claims.UID) > 128 || claims.Subject != claims.UID {
		return nil, ErrTalkamiTokenInvalid
	}
	if claims.UserStatus != 1 || claims.IsDestroy != 0 || claims.ProfileVersion <= 0 {
		return nil, ErrTalkamiUserDisabled
	}
	return claims, nil
}

type normalizedTalkamiProfile struct {
	uid               string
	nickname          string
	avatar            string
	gender            constants.Gender
	description       string
	countryCode       string
	country           string
	nativeLanguages   string
	learningLanguages string
}

func normalizeTalkamiProfile(claims *TalkamiClaims) normalizedTalkamiProfile {
	nickname := cleanText(claims.Nickname, 16)
	if nickname == "" {
		nickname = "Talkami用户"
	}

	return normalizedTalkamiProfile{
		uid:               claims.UID,
		nickname:          nickname,
		avatar:            normalizeAvatarURL(claims.Avatar),
		gender:            mapTalkamiGender(claims.Sex),
		description:       cleanText(claims.Description, 2000),
		countryCode:       cleanText(claims.CountryCode, 16),
		country:           cleanText(claims.Country, 80),
		nativeLanguages:   encodeLanguageList(claims.NativeLanguages),
		learningLanguages: encodeLanguageList(claims.LearningLanguages),
	}
}

func talkamiProfileMatchesUser(user *models.User, profile normalizedTalkamiProfile) bool {
	if user == nil {
		return false
	}
	return user.Nickname == profile.nickname &&
		user.Avatar == profile.avatar &&
		user.Gender == profile.gender &&
		user.Description == profile.description &&
		user.CountryCode == profile.countryCode &&
		user.Country == profile.country &&
		user.NativeLanguages == profile.nativeLanguages &&
		user.LearningLanguages == profile.learningLanguages
}

func mapTalkamiGender(sex int) constants.Gender {
	if sex == 1 {
		return constants.GenderMale
	}
	if sex == 0 {
		return constants.GenderFemale
	}
	return ""
}

func normalizeAvatarURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 2048 {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) {
		return value
	}
	return ""
}

func encodeLanguageList(values []string) string {
	cleaned := make([]string, 0, 5)
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = cleanText(value, 32)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, value)
		if len(cleaned) == 5 {
			break
		}
	}
	encoded, _ := json.Marshal(cleaned)
	return string(encoded)
}

func cleanText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

func envDurationSeconds(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(seconds) * time.Second, nil
}

func envBoolDefault(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func isDuplicateDBError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return errors.Is(err, gorm.ErrDuplicatedKey) ||
		strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "is not unique")
}
