package api

import (
	"bbs-go/internal/pkg/locales"
	"net/http"
	"strings"
	"time"

	"bbs-go/internal/handlers/render"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/ginx"
	"bbs-go/internal/services"

	"github.com/gin-gonic/gin"
)

type talkamiExchangeRequest struct {
	Token string `json:"token" form:"token"`
}

func TalkamiExchange(ctx *gin.Context) {
	ctx.Header("Cache-Control", "no-store")
	ctx.Header("Pragma", "no-cache")
	identityToken := strings.TrimSpace(ctx.GetHeader("X-Talkami-Token"))
	if identityToken == "" {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 20*1024)
		var req talkamiExchangeRequest
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ginx.WriteJSON(ctx, ginx.ErrorMessage(locales.Get("talkami.token_missing")))
			return
		}
		identityToken = strings.TrimSpace(req.Token)
	}
	if identityToken == "" {
		ginx.WriteJSON(ctx, ginx.ErrorMessage(locales.Get("talkami.token_missing")))
		return
	}

	result, err := services.TalkamiAuthService.Exchange(identityToken)
	if err != nil {
		ginx.WriteJSON(ctx, err)
		return
	}

	ginx.SetCookieKV(
		ctx,
		constants.CookieTokenKey,
		result.Token,
		ginx.CookieHTTPOnly(true),
		ginx.CookieExpires(time.Duration(result.ExpiresIn)*time.Second),
	)
	ginx.WriteJSON(ctx, map[string]any{
		"token":     result.Token,
		"tokenType": "Bearer",
		"expiresIn": result.ExpiresIn,
		"expiresAt": result.ExpiresAt,
		"user":      render.BuildUserProfile(result.User),
	})
}
