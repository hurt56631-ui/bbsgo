package middleware

import (
	"net/http"
	"strings"

	"bbs-go/internal/pkg/common"
	"bbs-go/internal/pkg/config"
	"bbs-go/internal/pkg/ginx"
	"bbs-go/internal/pkg/locales"
	"bbs-go/internal/services"

	"github.com/gin-gonic/gin"
)

func AuthMiddleware(ctx *gin.Context) {
	if !config.Instance.Installed {
		ctx.Next()
		return
	}

	if services.TalkamiAuthService.Exclusive() && blocksLocalLoginRoute(ctx.Request.Method, ctx.Request.URL.Path) {
		ginx.WriteJSON(ctx, ginx.ErrorMessage(locales.Get("auth.talkami_login_required")))
		ctx.Abort()
		return
	}

	user := services.UserTokenService.GetCurrent(ctx)
	if user == nil {
		ctx.Next()
		return
	}

	// 独占模式下，历史本地普通账号的旧 Token 不再获得登录身份。
	// 站长账号用于后台恢复，唐僧托管账号用于正常业务。
	if services.TalkamiAuthService.Exclusive() && !user.IsOwner() && !services.TalkamiAuthService.IsManagedUser(user) {
		ctx.Next()
		return
	}

	common.SetCurrentUser(ctx, user)
	if services.TalkamiAuthService.IsManagedUser(user) && blocksManagedIdentityMutation(ctx.Request.Method, ctx.Request.URL.Path) {
		ginx.WriteJSON(ctx, ginx.ErrorMessage(locales.Get("auth.managed_profile_edit_in_app")))
		ctx.Abort()
		return
	}

	ctx.Next()
}

func blocksLocalLoginRoute(method, path string) bool {
	if !strings.HasPrefix(path, "/api/login/") {
		return false
	}
	// 站长需要 signin 进入后台；任何用户都应能 signout。
	if path == "/api/login/signin" && method == http.MethodPost {
		return false
	}
	if path == "/api/login/signout" && (method == http.MethodGet || method == http.MethodPost) {
		return false
	}
	return true
}

func blocksManagedIdentityMutation(method, path string) bool {
	if method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch {
		return false
	}
	if strings.HasPrefix(path, "/api/user/update/") {
		return true
	}
	switch path {
	case "/api/user/update_avatar",
		"/api/user/set_username",
		"/api/user/set_email",
		"/api/user/set_password",
		"/api/user/update_password",
		"/api/user/send_verify_email",
		"/api/user/verify_email",
		"/api/login/wx_bind",
		"/api/login/wx_unbind",
		"/api/login/google_bind",
		"/api/login/google_unbind",
		"/api/login/github_bind",
		"/api/login/github_unbind":
		return true
	default:
		return false
	}
}
