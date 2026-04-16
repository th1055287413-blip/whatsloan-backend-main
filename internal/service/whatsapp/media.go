package whatsapp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"whatsapp_golang/internal/logger"
)

// SendImageMessage 发送图片消息
// 注意：此方法使用舊的 whatsmeow 直連方式，adminID 參數暫不使用
func (s *whatsappService) SendImageMessage(accountID uint, contactPhone, imagePath, caption string, adminID *uint) error {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsLoggedIn() {
		return fmt.Errorf("账号未连接")
	}

	// 格式化为完整的 JID (如果contactPhone不包含@,则添加@s.whatsapp.net)
	toJID := contactPhone
	if !strings.Contains(contactPhone, "@") {
		toJID = contactPhone + "@s.whatsapp.net"
	}

	jid, err := types.ParseJID(toJID)
	if err != nil {
		return fmt.Errorf("无效的JID: %v", err)
	}

	// 读取图片文件
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return fmt.Errorf("读取图片失败: %v", err)
	}

	// 上传图片到 WhatsApp 服务器
	uploaded, err := client.Upload(context.Background(), imageData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("上传图片到WhatsApp失败: %v", err)
	}

	// 构造图片消息
	imageMsg := &waE2E.ImageMessage{
		Caption:       proto.String(caption),
		Mimetype:      proto.String(http.DetectContentType(imageData)),
		URL:           &uploaded.URL,
		DirectPath:    &uploaded.DirectPath,
		MediaKey:      uploaded.MediaKey,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(imageData))),
	}

	// 发送消息
	resp, err := client.SendMessage(context.Background(), jid, &waE2E.Message{
		ImageMessage: imageMsg,
	})
	if err != nil {
		return fmt.Errorf("发送图片消息失败: %v", err)
	}

	// 移动文件从 temp/ 到永久目录
	permanentPath := strings.Replace(imagePath, "/temp/", "/", 1)
	if err := os.Rename(imagePath, permanentPath); err != nil {
		logger.Warnw("移動檔案失敗，保留在臨時目錄", "error", err)
		permanentPath = imagePath // 失败则使用原路径
	}

	// 转换为前端可访问的URL路径格式 (与接收消息保持一致)
	// permanentPath 格式: uploads/media/xxx.png
	// mediaURL 格式: /media/xxx.png
	mediaURL := strings.Replace(permanentPath, "uploads/media/", "/media/", 1)

	// 保存消息到数据库
	go s.saveMessage(accountID, resp.ID, client.Store.ID.String(), toJID, caption, "image", time.Now(), true, "", mediaURL)

	logger.WithAccount(accountID).Debugw("圖片訊息發送成功",
		"to", toJID, "media", permanentPath)

	return nil
}

// SendVideoMessage 发送视频消息
// 注意：此方法使用舊的 whatsmeow 直連方式，adminID 參數暫不使用
func (s *whatsappService) SendVideoMessage(accountID uint, contactPhone, videoPath, caption string, adminID *uint) error {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsLoggedIn() {
		return fmt.Errorf("账号未连接")
	}

	// 格式化为完整的 JID (如果contactPhone不包含@,则添加@s.whatsapp.net)
	toJID := contactPhone
	if !strings.Contains(contactPhone, "@") {
		toJID = contactPhone + "@s.whatsapp.net"
	}

	jid, err := types.ParseJID(toJID)
	if err != nil {
		return fmt.Errorf("无效的JID: %v", err)
	}

	// 读取视频文件
	videoData, err := os.ReadFile(videoPath)
	if err != nil {
		return fmt.Errorf("读取视频失败: %v", err)
	}

	// 上传视频到 WhatsApp 服务器
	uploaded, err := client.Upload(context.Background(), videoData, whatsmeow.MediaVideo)
	if err != nil {
		return fmt.Errorf("上传视频到WhatsApp失败: %v", err)
	}

	// 构造视频消息
	videoMsg := &waE2E.VideoMessage{
		Caption:       proto.String(caption),
		Mimetype:      proto.String(http.DetectContentType(videoData)),
		URL:           &uploaded.URL,
		DirectPath:    &uploaded.DirectPath,
		MediaKey:      uploaded.MediaKey,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(videoData))),
	}

	// 发送消息
	resp, err := client.SendMessage(context.Background(), jid, &waE2E.Message{
		VideoMessage: videoMsg,
	})
	if err != nil {
		return fmt.Errorf("发送视频消息失败: %v", err)
	}

	// 移动文件从 temp/ 到永久目录
	permanentPath := strings.Replace(videoPath, "/temp/", "/", 1)
	if err := os.Rename(videoPath, permanentPath); err != nil {
		logger.Warnw("移動檔案失敗，保留在臨時目錄", "error", err)
		permanentPath = videoPath // 失败则使用原路径
	}

	// 转换为前端可访问的URL路径格式
	mediaURL := strings.Replace(permanentPath, "uploads/media/", "/media/", 1)

	// 保存消息到数据库
	go s.saveMessage(accountID, resp.ID, client.Store.ID.String(), toJID, caption, "video", time.Now(), true, "", mediaURL)

	logger.WithAccount(accountID).Debugw("影片訊息發送成功",
		"to", toJID, "media", permanentPath)

	return nil
}

// SendAudioMessage 发送音频消息
// 注意：此方法使用舊的 whatsmeow 直連方式，adminID 參數暫不使用
func (s *whatsappService) SendAudioMessage(accountID uint, contactPhone, audioPath string, adminID *uint) error {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsLoggedIn() {
		return fmt.Errorf("账号未连接")
	}

	// 格式化为完整的 JID (如果contactPhone不包含@,则添加@s.whatsapp.net)
	toJID := contactPhone
	if !strings.Contains(contactPhone, "@") {
		toJID = contactPhone + "@s.whatsapp.net"
	}

	jid, err := types.ParseJID(toJID)
	if err != nil {
		return fmt.Errorf("无效的JID: %v", err)
	}

	// 读取音频文件
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return fmt.Errorf("读取音频失败: %v", err)
	}

	// 上传音频到 WhatsApp 服务器
	uploaded, err := client.Upload(context.Background(), audioData, whatsmeow.MediaAudio)
	if err != nil {
		return fmt.Errorf("上传音频到WhatsApp失败: %v", err)
	}

	// 构造音频消息
	audioMsg := &waE2E.AudioMessage{
		Mimetype:      proto.String(http.DetectContentType(audioData)),
		URL:           &uploaded.URL,
		DirectPath:    &uploaded.DirectPath,
		MediaKey:      uploaded.MediaKey,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(audioData))),
	}

	// 发送消息
	resp, err := client.SendMessage(context.Background(), jid, &waE2E.Message{
		AudioMessage: audioMsg,
	})
	if err != nil {
		return fmt.Errorf("发送音频消息失败: %v", err)
	}

	// 移动文件从 temp/ 到永久目录
	permanentPath := strings.Replace(audioPath, "/temp/", "/", 1)
	if err := os.Rename(audioPath, permanentPath); err != nil {
		logger.Warnw("移動檔案失敗，保留在臨時目錄", "error", err)
		permanentPath = audioPath // 失败则使用原路径
	}

	// 转换为前端可访问的URL路径格式
	mediaURL := strings.Replace(permanentPath, "uploads/media/", "/media/", 1)

	// 保存消息到数据库
	go s.saveMessage(accountID, resp.ID, client.Store.ID.String(), toJID, "", "audio", time.Now(), true, "", mediaURL)

	logger.WithAccount(accountID).Debugw("音訊訊息發送成功",
		"to", toJID, "media", permanentPath)

	return nil
}

// SendDocumentMessage 发送文档消息
// 注意：此方法使用舊的 whatsmeow 直連方式，adminID 參數暫不使用
func (s *whatsappService) SendDocumentMessage(accountID uint, contactPhone, documentPath, fileName string, adminID *uint) error {
	s.mu.RLock()
	client, exists := s.clients[accountID]
	s.mu.RUnlock()

	if !exists || !client.IsLoggedIn() {
		return fmt.Errorf("账号未连接")
	}

	// 格式化为完整的 JID (如果contactPhone不包含@,则添加@s.whatsapp.net)
	toJID := contactPhone
	if !strings.Contains(contactPhone, "@") {
		toJID = contactPhone + "@s.whatsapp.net"
	}

	jid, err := types.ParseJID(toJID)
	if err != nil {
		return fmt.Errorf("无效的JID: %v", err)
	}

	// 读取文档文件
	documentData, err := os.ReadFile(documentPath)
	if err != nil {
		return fmt.Errorf("读取文档失败: %v", err)
	}

	// 上传文档到 WhatsApp 服务器
	uploaded, err := client.Upload(context.Background(), documentData, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("上传文档到WhatsApp失败: %v", err)
	}

	// 构造文档消息
	documentMsg := &waE2E.DocumentMessage{
		FileName:      proto.String(fileName),
		Mimetype:      proto.String(http.DetectContentType(documentData)),
		URL:           &uploaded.URL,
		DirectPath:    &uploaded.DirectPath,
		MediaKey:      uploaded.MediaKey,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(documentData))),
	}

	// 发送消息
	resp, err := client.SendMessage(context.Background(), jid, &waE2E.Message{
		DocumentMessage: documentMsg,
	})
	if err != nil {
		return fmt.Errorf("发送文档消息失败: %v", err)
	}

	// 移动文件从 temp/ 到永久目录
	permanentPath := strings.Replace(documentPath, "/temp/", "/", 1)
	if err := os.Rename(documentPath, permanentPath); err != nil {
		logger.Warnw("移動檔案失敗，保留在臨時目錄", "error", err)
		permanentPath = documentPath // 失败则使用原路径
	}

	// 转换为前端可访问的URL路径格式
	mediaURL := strings.Replace(permanentPath, "uploads/media/", "/media/", 1)

	// 保存消息到数据库
	go s.saveMessage(accountID, resp.ID, client.Store.ID.String(), toJID, fileName, "document", time.Now(), true, "", mediaURL)

	logger.WithAccount(accountID).Debugw("文件訊息發送成功",
		"to", toJID, "media", permanentPath)

	return nil
}

// downloadMediaMessage 下载媒体消息
func (s *whatsappService) downloadMediaMessage(client *whatsmeow.Client, msg *events.Message) (string, error) {
	// 确定可下载的消息类型
	var downloadable whatsmeow.DownloadableMessage
	if img := msg.Message.GetImageMessage(); img != nil {
		downloadable = img
	} else if video := msg.Message.GetVideoMessage(); video != nil {
		downloadable = video
	} else if audio := msg.Message.GetAudioMessage(); audio != nil {
		downloadable = audio
	} else if doc := msg.Message.GetDocumentMessage(); doc != nil {
		downloadable = doc
	} else if sticker := msg.Message.GetStickerMessage(); sticker != nil {
		downloadable = sticker
	} else {
		return "", fmt.Errorf("不支持的媒体类型")
	}

	// 下载媒体内容
	ctx := context.Background()
	data, err := client.Download(ctx, downloadable)
	if err != nil {
		return "", fmt.Errorf("下载媒体失败: %v", err)
	}

	// 确定文件扩展名
	var ext string
	var msgType string
	if img := msg.Message.GetImageMessage(); img != nil {
		ext = ".jpg"
		msgType = "image"
		if img.Mimetype != nil && strings.Contains(*img.Mimetype, "png") {
			ext = ".png"
		}
	} else if video := msg.Message.GetVideoMessage(); video != nil {
		ext = ".mp4"
		msgType = "video"
	} else if audio := msg.Message.GetAudioMessage(); audio != nil {
		ext = ".ogg"
		msgType = "audio"
		if audio.Mimetype != nil && strings.Contains(*audio.Mimetype, "mp3") {
			ext = ".mp3"
		}
		// 区分语音笔记
		if audio.GetPTT() {
			msgType = "voice_note"
		}
	} else if doc := msg.Message.GetDocumentMessage(); doc != nil {
		ext = ".bin"
		msgType = "document"
		if doc.FileName != nil && *doc.FileName != "" {
			// 从文件名中提取扩展名
			if fileExt := filepath.Ext(*doc.FileName); fileExt != "" {
				ext = fileExt
			}
		}
	} else if sticker := msg.Message.GetStickerMessage(); sticker != nil {
		ext = ".webp" // WhatsApp 贴纸使用 WebP 格式
		msgType = "sticker"
	} else {
		return "", fmt.Errorf("不支持的媒体类型")
	}

	// 生成唯一文件名
	filename := fmt.Sprintf("%s_%s%s", msgType, msg.Info.ID, ext)

	// 构建完整文件路径
	filePath := filepath.Join(s.config.WhatsApp.MediaDir, filename)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return "", fmt.Errorf("创建媒体目录失败: %v", err)
	}

	// 保存文件
	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("创建媒体文件失败: %v", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, strings.NewReader(string(data))); err != nil {
		return "", fmt.Errorf("保存媒体文件失败: %v", err)
	}

	// 返回媒体URL (相对路径,前端会拼接服务器地址)
	mediaURL := "/media/" + filename
	logger.Debugw("媒體檔案已儲存", "filename", filename, "media_url", mediaURL)

	return mediaURL, nil
}

// parseMessageContent 统一解析消息内容
func (s *whatsappService) parseMessageContent(msg *events.Message, accountID uint) *ParsedMessage {
	result := &ParsedMessage{}

	// 检查消息是否为空
	if msg == nil || msg.Message == nil {
		logger.WithAccount(accountID).Debugw("跳過空訊息")
		result.Skip = true
		return result
	}

	// 跳过内部协议消息
	if msg.Message.GetProtocolMessage() != nil {
		logger.WithAccount(accountID).Debugw("跳過協議訊息",
			"type", msg.Message.GetProtocolMessage().GetType())
		result.Skip = true
		return result
	}
	if msg.Message.GetSenderKeyDistributionMessage() != nil {
		logger.WithAccount(accountID).Debugw("跳過金鑰分發訊息")
		result.Skip = true
		return result
	}

	// 解析消息内容
	if msg.Message.GetConversation() != "" {
		result.Content = msg.Message.GetConversation()
		result.Type = "text"
	} else if extMsg := msg.Message.GetExtendedTextMessage(); extMsg != nil {
		result.Content = extMsg.GetText()
		result.Type = "text"
	} else if msg.Message.GetImageMessage() != nil {
		result.Content = "[图片消息]"
		result.Type = "image"
		result.NeedsMedia = true
	} else if msg.Message.GetVideoMessage() != nil {
		result.Content = "[视频消息]"
		result.Type = "video"
		result.NeedsMedia = true
	} else if audioMsg := msg.Message.GetAudioMessage(); audioMsg != nil {
		if audioMsg.GetPTT() {
			result.Content = "[语音消息]"
			result.Type = "voice_note"
		} else {
			result.Content = "[音频文件]"
			result.Type = "audio"
		}
		result.NeedsMedia = true
		result.Metadata = map[string]interface{}{
			"duration": audioMsg.GetSeconds(),
			"ptt":      audioMsg.GetPTT(),
		}
	} else if msg.Message.GetDocumentMessage() != nil {
		result.Content = "[文档消息]"
		result.Type = "document"
		result.NeedsMedia = true
	} else if stickerMsg := msg.Message.GetStickerMessage(); stickerMsg != nil {
		result.Content = "[贴纸]"
		result.Type = "sticker"
		result.NeedsMedia = true
		result.Metadata = map[string]interface{}{
			"is_animated": stickerMsg.GetIsAnimated(),
		}
	} else if locMsg := msg.Message.GetLocationMessage(); locMsg != nil {
		locName := locMsg.GetName()
		if locName == "" {
			locName = "位置"
		}
		result.Content = fmt.Sprintf("[位置] %s", locName)
		result.Type = "location"
		result.Metadata = map[string]interface{}{
			"latitude":  locMsg.GetDegreesLatitude(),
			"longitude": locMsg.GetDegreesLongitude(),
			"name":      locMsg.GetName(),
			"address":   locMsg.GetAddress(),
		}
	} else if contactMsg := msg.Message.GetContactMessage(); contactMsg != nil {
		displayName := contactMsg.GetDisplayName()
		if displayName == "" {
			displayName = "联系人"
		}
		result.Content = fmt.Sprintf("[联系人] %s", displayName)
		result.Type = "contact"
		result.Metadata = map[string]interface{}{
			"display_name": contactMsg.GetDisplayName(),
			"vcard":        contactMsg.GetVcard(),
		}
	} else if reactionMsg := msg.Message.GetReactionMessage(); reactionMsg != nil {
		emoji := reactionMsg.GetText()
		if emoji == "" {
			emoji = "👍"
		}
		result.Content = fmt.Sprintf("[回应] %s", emoji)
		result.Type = "reaction"
		result.Metadata = map[string]interface{}{
			"emoji":             reactionMsg.GetText(),
			"target_message_id": reactionMsg.GetKey().GetID(),
		}
	} else if liveLocMsg := msg.Message.GetLiveLocationMessage(); liveLocMsg != nil {
		result.Content = "[实时位置分享]"
		result.Type = "live_location"
		result.Metadata = map[string]interface{}{
			"latitude":  liveLocMsg.GetDegreesLatitude(),
			"longitude": liveLocMsg.GetDegreesLongitude(),
			"accuracy":  liveLocMsg.GetAccuracyInMeters(),
			"speed":     liveLocMsg.GetSpeedInMps(),
		}
	} else if templateMsg := msg.Message.GetTemplateMessage(); templateMsg != nil {
		result.Content = "[模板消息]"
		result.Type = "template"
		if hydratedTemplate := templateMsg.GetHydratedTemplate(); hydratedTemplate != nil {
			if hydratedTemplate.GetHydratedContentText() != "" {
				result.Content = hydratedTemplate.GetHydratedContentText()
			} else if hydratedTemplate.GetHydratedTitleText() != "" {
				result.Content = hydratedTemplate.GetHydratedTitleText()
			}
		}
	} else if pollMsg := msg.Message.GetPollCreationMessage(); pollMsg != nil {
		result.Content = fmt.Sprintf("[投票] %s", pollMsg.GetName())
		result.Type = "poll"
	} else {
		result.Content = "[其他类型消息]"
		result.Type = "other"
		logger.WithAccount(accountID).Warnw("未知的訊息類型",
			"message_id", msg.Info.ID, "message", msg.Message.String())
	}

	return result
}
