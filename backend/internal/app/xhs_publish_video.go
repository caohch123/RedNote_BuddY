package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"os"
	"path/filepath"
	"time"

	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/store"
	"xhs-workbench/internal/tooling"
)

func (a *App) prepareXHSVideoCover(ctx context.Context, publication domain.Publication, video xhsPublishAsset) (xhsPublishAsset, error) {
	var result xhsPublishAsset
	settings, err := a.settings()
	if err != nil {
		return result, errors.New("无法读取视频封面配置")
	}
	path, err := a.videoRewriteTemp(".publish-cover.jpg")
	if err != nil {
		return result, errors.New("无法准备视频封面")
	}
	keep := false
	defer func() {
		if !keep {
			a.removeVideoRewriteTemp(path)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err = tooling.Command(ctx, a.Root, settings.FFmpegPath, "ffmpeg", "-nostdin", "-y", "-i", video.Path, "-frames:v", "1", "-vf", "scale=1280:1280:force_original_aspect_ratio=decrease", "-q:v", "3", path).Run(); err != nil {
		return result, errors.New("视频封面提取失败，请检查 FFmpeg，或选择一张图片作封面")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, errors.New("无法读取视频封面")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "jpeg" || config.Width < 1 || config.Height < 1 || len(data) > 20<<20 {
		return result, errors.New("提取的视频封面无效，请选择一张 JPEG 或 PNG 图片")
	}
	relative, err := filepath.Rel(a.Data, path)
	if err != nil {
		return result, errors.New("视频封面路径无效")
	}
	hash := sha256.Sum256(data)
	asset := domain.Asset{ID: ID(), OwnerID: publication.ID, Kind: "cover", MIME: "image/jpeg", Status: "ready", LocalPath: relative,
		Width: config.Width, Height: config.Height, Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:]), Position: 1}
	publication.AssetIDs = append(append([]string{}, publication.AssetIDs...), asset.ID)
	err = a.Store.Atomic(func(tx *store.Transaction) error {
		if err := tx.Put("assets", asset.ID, asset); err != nil {
			return err
		}
		return tx.Put("publications", publication.ID, publication)
	})
	if err != nil {
		return result, errors.New("保存视频封面失败，请重试检查发布内容")
	}
	keep = true
	return xhsPublishAsset{ID: asset.ID, Kind: "cover", MIME: asset.MIME, Status: "ready", Position: 1, Width: asset.Width, Height: asset.Height, Size: asset.Size, SHA256: asset.SHA256, Path: path}, nil
}
