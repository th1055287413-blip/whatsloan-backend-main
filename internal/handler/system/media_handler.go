package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"
)

// MediaHandler 媒體文件處理器
type MediaHandler struct {
	mediaDir string
}

// NewMediaHandler 創建媒體處理器
func NewMediaHandler(mediaDir string) *MediaHandler {
	return &MediaHandler{
		mediaDir: mediaDir,
	}
}

// UploadFile 通用檔案上傳（供內部微服務使用，依 source 分資料夾）
func (h *MediaHandler) UploadFile(c *gin.Context) {
	source := c.PostForm("source")
	if source == "" {
		common.Error(c, common.CodeInvalidParams, "source 為必填欄位")
		return
	}
	if !isValidSource(source) {
		common.Error(c, common.CodeInvalidParams, "source 只能包含英文字母、數字、底線和連字號")
		return
	}

	h.uploadMedia(c, "file", map[string]bool{
		// image
		"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true, "image/jpg": true,
		// audio
		"audio/ogg": true, "audio/mpeg": true, "audio/mp3": true, "audio/wav": true,
		"audio/aac": true, "audio/mp4": true, "audio/x-m4a": true, "audio/webm": true,
		// video
		"video/mp4": true, "video/webm": true, "video/ogg": true, "video/mov": true, "video/avi": true, "video/mkv": true,
		// document
		"application/pdf": true, "application/msword": true,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true,
		"application/vnd.ms-excel":                                                  true,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
		"application/vnd.ms-powerpoint":                                             true,
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
		"application/zip": true, "application/x-rar-compressed": true, "application/x-7z-compressed": true,
		"text/plain": true, "text/csv": true,
	}, 20*1024*1024, "platform", source) // 20MB
}

// UploadImage 上傳圖片
func (h *MediaHandler) UploadImage(c *gin.Context) {
	h.uploadMedia(c, "image", map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
		"image/jpg":  true,
	}, 16*1024*1024) // 16MB
}

// UploadAudio 上傳語音/音頻文件
func (h *MediaHandler) UploadAudio(c *gin.Context) {
	h.uploadMedia(c, "audio", map[string]bool{
		"audio/ogg":   true,
		"audio/mpeg":  true,
		"audio/mp3":   true,
		"audio/wav":   true,
		"audio/aac":   true,
		"audio/mp4":   true,
		"audio/x-m4a": true,
		"audio/webm":  true,
	}, 16*1024*1024) // 16MB
}

// UploadVideo 上傳視頻文件
func (h *MediaHandler) UploadVideo(c *gin.Context) {
	h.uploadMedia(c, "video", map[string]bool{
		"video/mp4":  true,
		"video/webm": true,
		"video/ogg":  true,
		"video/mov":  true,
		"video/avi":  true,
		"video/mkv":  true,
	}, 64*1024*1024) // 64MB
}

// UploadDocument 上傳文檔/文件
func (h *MediaHandler) UploadDocument(c *gin.Context) {
	h.uploadMedia(c, "document", map[string]bool{
		"application/pdf":    true,
		"application/msword": true,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
		"application/vnd.ms-excel": true,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       true,
		"application/vnd.ms-powerpoint":                                           true,
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
		"application/zip":              true,
		"application/x-rar-compressed": true,
		"application/x-7z-compressed":  true,
		"text/plain":                   true,
		"text/csv":                     true,
	}, 100*1024*1024) // 100MB
}

// uploadMedia 通用媒體上傳處理函數
func (h *MediaHandler) uploadMedia(c *gin.Context, mediaType string, allowedTypes map[string]bool, maxSize int64, pathPrefix ...string) {
	// 1. 獲取上傳的文件
	file, err := c.FormFile("file")
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("取得上傳檔案失敗", "error", err)
		common.Error(c, common.CodeInvalidParams, "未找到上傳文件")
		return
	}

	// 2. 驗證文件類型
	contentType := file.Header.Get("Content-Type")
	if !allowedTypes[contentType] {
		logger.Ctx(c.Request.Context()).Warnw("不支援的檔案類型", "content_type", contentType, "media_type", mediaType)
		common.Error(c, common.CodeInvalidParams, fmt.Sprintf("不支持的%s文件類型", mediaType))
		return
	}

	// 3. 驗證文件大小
	if file.Size > maxSize {
		logger.Ctx(c.Request.Context()).Warnw("檔案過大", "file_size", file.Size, "max_size", maxSize)
		common.Error(c, common.CodeInvalidParams, fmt.Sprintf("文件大小不能超過%dMB", maxSize/(1024*1024)))
		return
	}

	// 4. 生成唯一文件名
	ext := filepath.Ext(file.Filename)
	if ext == "" {
		ext = getExtensionByMimeType(contentType)
	}
	newFilename := uuid.New().String() + ext

	// 5. 創建媒體類型目錄並保存文件
	parts := []string{h.mediaDir}
	parts = append(parts, pathPrefix...)
	parts = append(parts, mediaType)
	mediaDir := filepath.Join(parts...)
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("創建媒體目錄失敗", "media_type", mediaType, "error", err)
		common.Error(c, common.CodeInternalError, "創建目錄失敗")
		return
	}

	savePath := filepath.Join(mediaDir, newFilename)
	if err := c.SaveUploadedFile(file, savePath); err != nil {
		logger.Ctx(c.Request.Context()).Errorw("儲存檔案失敗", "error", err)
		common.Error(c, common.CodeInternalError, "文件保存失敗")
		return
	}

	logger.Ctx(c.Request.Context()).Debugw("檔案上傳成功", "media_type", mediaType, "file_name", newFilename, "original_name", file.Filename, "file_size", file.Size)

	// 6. 返回文件資訊
	urlPrefix := mediaType
	if len(pathPrefix) > 0 {
		urlPrefix = strings.Join(pathPrefix, "/") + "/" + mediaType
	}
	common.Success(c, map[string]interface{}{
		"file_path":  fmt.Sprintf("/media/%s/%s", urlPrefix, newFilename),
		"file_name":  file.Filename,
		"file_size":  file.Size,
		"mime_type":  contentType,
		"media_type": mediaType,
	})
}

// getExtensionByMimeType 根據 MIME 類型獲取文件擴展名
func getExtensionByMimeType(mimeType string) string {
	mimeMap := map[string]string{
		// Images
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/gif":  ".gif",
		"image/webp": ".webp",
		// Audio
		"audio/ogg":   ".ogg",
		"audio/mpeg":  ".mp3",
		"audio/mp3":   ".mp3",
		"audio/wav":   ".wav",
		"audio/aac":   ".aac",
		"audio/mp4":   ".m4a",
		"audio/x-m4a": ".m4a",
		"audio/webm":  ".webm",
		// Video
		"video/mp4":  ".mp4",
		"video/webm": ".webm",
		"video/ogg":  ".ogv",
		"video/mov":  ".mov",
		"video/avi":  ".avi",
		"video/mkv":  ".mkv",
		// Documents
		"application/pdf":    ".pdf",
		"application/msword": ".doc",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
		"application/vnd.ms-excel": ".xls",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       ".xlsx",
		"application/vnd.ms-powerpoint":                                           ".ppt",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
		"application/zip":              ".zip",
		"application/x-rar-compressed": ".rar",
		"application/x-7z-compressed":  ".7z",
		"text/plain":                   ".txt",
		"text/csv":                     ".csv",
	}
	if ext, ok := mimeMap[mimeType]; ok {
		return ext
	}
	return ""
}

// isValidSource 驗證 source 名稱（防止路徑穿越）
func isValidSource(s string) bool {
	if len(s) == 0 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
