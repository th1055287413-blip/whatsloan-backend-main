package whatsmeow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

// downloadAndUploadMedia downloads media from WhatsApp and saves it locally.
// Returns the media URL path (e.g. "/media/image/{uuid}.jpg") or empty string on failure.
func (m *Manager) downloadAndUploadMedia(parentCtx context.Context, client *whatsmeow.Client, msg *events.Message, contentType string) string {
	if m.mediaDir == "" {
		m.log.Warnw("mediaDir 未設定，跳過媒體下載")
		return ""
	}

	// Determine downloadable message and extension
	var downloadable whatsmeow.DownloadableMessage
	var ext string
	var mediaType string

	switch contentType {
	case "image":
		img := msg.Message.GetImageMessage()
		if img == nil {
			return ""
		}
		downloadable = img
		ext = ".jpg"
		if img.GetMimetype() != "" && strings.Contains(img.GetMimetype(), "png") {
			ext = ".png"
		}
		mediaType = "image"
	case "video":
		vid := msg.Message.GetVideoMessage()
		if vid == nil {
			return ""
		}
		downloadable = vid
		ext = ".mp4"
		mediaType = "video"
	case "audio":
		aud := msg.Message.GetAudioMessage()
		if aud == nil {
			return ""
		}
		downloadable = aud
		ext = ".ogg"
		if aud.GetMimetype() != "" && strings.Contains(aud.GetMimetype(), "mp3") {
			ext = ".mp3"
		}
		mediaType = "audio"
	case "document":
		doc := msg.Message.GetDocumentMessage()
		if doc == nil {
			return ""
		}
		downloadable = doc
		ext = ".bin"
		if doc.GetFileName() != "" {
			if e := filepath.Ext(doc.GetFileName()); e != "" {
				ext = e
			}
		}
		mediaType = "document"
	case "sticker":
		stk := msg.Message.GetStickerMessage()
		if stk == nil {
			return ""
		}
		downloadable = stk
		ext = ".webp"
		mediaType = "image"
	default:
		return ""
	}

	// Download from WhatsApp（從 worker ctx 派生，帳號移除時自動中斷）
	ctx, cancel := context.WithTimeout(parentCtx, 60*time.Second)
	defer cancel()

	data, err := client.Download(ctx, downloadable)
	if err != nil {
		m.log.Warnw("下載媒體失敗", "msg_id", msg.Info.ID, "content_type", contentType, "error", err)
		return ""
	}

	// Save to local filesystem
	mediaURL, err := m.saveMediaLocal(data, mediaType, ext)
	if err != nil {
		m.log.Warnw("儲存媒體到本地失敗", "msg_id", msg.Info.ID, "error", err)
		return ""
	}

	m.log.Debugw("媒體下載+儲存成功", "msg_id", msg.Info.ID, "content_type", contentType, "media_url", mediaURL)
	return mediaURL
}

// saveMediaLocal saves media data to the local filesystem.
// Returns the URL path (e.g. "/media/image/{uuid}.jpg").
func (m *Manager) saveMediaLocal(data []byte, mediaType, ext string) (string, error) {
	dir := filepath.Join(m.mediaDir, mediaType)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create dir %s: %w", dir, err)
	}

	filename := uuid.New().String() + ext
	savePath := filepath.Join(dir, filename)
	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return "", fmt.Errorf("write file %s: %w", savePath, err)
	}

	return fmt.Sprintf("/media/%s/%s", mediaType, filename), nil
}
