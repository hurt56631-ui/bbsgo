package render

import (
	"bbs-go/internal/models/req"
	"bbs-go/internal/models/resp"
	"log/slog"
	"net/url"
	"path"
	"strings"

	"github.com/mlogclub/simple/common/jsons"
	"github.com/mlogclub/simple/common/strs"
)

func BuildImageList(imageListStr string) (imageList []resp.ImageInfo) {
	if strs.IsNotBlank(imageListStr) {
		var images []req.ImageDTO
		if err := jsons.Parse(imageListStr, &images); err == nil {
			if len(images) > 0 {
				for _, image := range images {
					if isVideoMediaURL(image.Url) {
						imageList = append(imageList, resp.ImageInfo{Url: image.Url, Preview: image.Url})
						continue
					}
					imageList = append(imageList, resp.ImageInfo{
						Url:     HandleOssImageStyleDetail(image.Url),
						Preview: HandleOssImageStylePreview(image.Url),
					})
				}
			}
		} else {
			slog.Error(err.Error(), slog.Any("err", err))
		}
	}
	return
}

func BuildImage(imageStr string) *resp.ImageInfo {
	if strs.IsBlank(imageStr) {
		return nil
	}
	var img *req.ImageDTO
	if err := jsons.Parse(imageStr, &img); err != nil {
		slog.Error(err.Error(), slog.Any("err", err))
		return nil
	} else {
		if isVideoMediaURL(img.Url) {
			return &resp.ImageInfo{Url: img.Url, Preview: img.Url}
		}
		return &resp.ImageInfo{
			Url:     HandleOssImageStyleDetail(img.Url),
			Preview: HandleOssImageStylePreview(img.Url),
		}
	}
}

func isVideoMediaURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(parsed.Path))
	switch ext {
	case ".mp4", ".m4v", ".webm", ".mov", ".3gp", ".mkv":
		return true
	default:
		return false
	}
}
