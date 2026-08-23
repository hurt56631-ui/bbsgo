package scheduler

import (
	"log/slog"
	"time"

	"bbs-go/internal/services"

	"github.com/robfig/cron/v3"
)

func Start() {
	c := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cron.DefaultLogger),
		cron.Recover(cron.DefaultLogger),
	))

	// The default robfig parser uses standard five-field cron expressions.
	// The previous Quartz-style "?" expression was invalid and silently left
	// the sitemap task unscheduled.
	addCronFunc(c, "0 4 * * *", func() {
		if err := services.SeoSitemapService.GenerateAndUpload(); err != nil {
			slog.Error("generate sitemap error", slog.Any("err", err))
		}
	})

	if addCronFunc(c, "* * * * *", func() {
		if err := services.ViewCountService.Flush(); err != nil {
			slog.Error("flush view counts error", slog.Any("err", err))
		}
	}) {
		services.ViewCountService.EnableBuffering()
	}

	addCronFunc(c, "* * * * *", func() {
		if err := services.StorageDeleteService.ProcessPending(100); err != nil {
			slog.Warn("retry forum storage deletes incomplete", slog.Any("err", err))
		}
	})

	addCronFunc(c, "* * * * *", func() {
		if err := services.SearchDeleteService.ProcessPending(100); err != nil {
			slog.Warn("retry forum search deletes incomplete", slog.Any("err", err))
		}
	})

	addCronFunc(c, "15 4 * * *", func() {
		deletedTokens, err := services.UserTokenService.CleanupExpired(7 * 24 * time.Hour)
		if err != nil {
			slog.Error("cleanup expired user tokens error", slog.Any("err", err))
		} else if deletedTokens > 0 {
			slog.Info("expired user tokens cleaned", slog.Int64("count", deletedTokens))
		}

		deletedNonces, err := services.TalkamiAuthService.CleanupExpiredNonces(24 * time.Hour)
		if err != nil {
			slog.Error("cleanup expired talkami nonces error", slog.Any("err", err))
		} else if deletedNonces > 0 {
			slog.Info("expired talkami nonces cleaned", slog.Int64("count", deletedNonces))
		}
	})

	c.Start()
}

func addCronFunc(c *cron.Cron, spec string, cmd func()) bool {
	if _, err := c.AddFunc(spec, cmd); err != nil {
		slog.Error("add cron func error", slog.String("spec", spec), slog.Any("err", err))
		return false
	}
	return true
}
