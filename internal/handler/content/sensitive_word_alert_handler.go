package content

import (
	"time"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SensitiveWordAlertHandler 告警處理器
type SensitiveWordAlertHandler struct {
	db              *gorm.DB
	telegramService contentSvc.TelegramService
}

// NewSensitiveWordAlertHandler 創建處理器
func NewSensitiveWordAlertHandler(
	db *gorm.DB,
	telegramService contentSvc.TelegramService,
) *SensitiveWordAlertHandler {
	return &SensitiveWordAlertHandler{
		db:              db,
		telegramService: telegramService,
	}
}

// ListAlerts 獲取告警列表
func (h *SensitiveWordAlertHandler) ListAlerts(c *gin.Context) {
	params := common.ParsePaginationParams(c)

	var alerts []model.SensitiveWordAlert
	var total int64

	query := h.db.Model(&model.SensitiveWordAlert{})

	// 篩選
	if matchedWord := c.Query("matchedWord"); matchedWord != "" {
		query = query.Where("matched_word = ?", matchedWord)
	}
	if telegramSent := c.Query("telegramSent"); telegramSent != "" {
		query = query.Where("telegram_sent = ?", telegramSent == "true")
	}

	query.Count(&total)

	if err := query.Offset(params.Offset()).Limit(params.Limit()).
		Order("created_at DESC").Find(&alerts).Error; err != nil {
		common.HandleDatabaseError(c, err, "查詢告警")
		return
	}

	common.PaginatedList(c, alerts, total, params.Page, params.PageSize)
}

// GetAlert 獲取告警詳情
func (h *SensitiveWordAlertHandler) GetAlert(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var alert model.SensitiveWordAlert
	if err := h.db.First(&alert, id).Error; err != nil {
		common.HandleNotFoundError(c, "告警")
		return
	}

	common.Success(c, alert)
}

// GetStats 獲取統計資訊
func (h *SensitiveWordAlertHandler) GetStats(c *gin.Context) {
	now := time.Now()
	today := now.Format("2006-01-02")
	weekAgo := now.AddDate(0, 0, -7).Format("2006-01-02")

	var todayCount, weekCount, sentCount, failedCount int64

	h.db.Model(&model.SensitiveWordAlert{}).
		Where("DATE(created_at) = ?", today).Count(&todayCount)

	h.db.Model(&model.SensitiveWordAlert{}).
		Where("DATE(created_at) >= ?", weekAgo).Count(&weekCount)

	h.db.Model(&model.SensitiveWordAlert{}).
		Where("telegram_sent = ?", true).Count(&sentCount)

	h.db.Model(&model.SensitiveWordAlert{}).
		Where("telegram_sent = ?", false).Count(&failedCount)

	common.Success(c, gin.H{
		"today":  todayCount,
		"week":   weekCount,
		"sent":   sentCount,
		"failed": failedCount,
	})
}

// ResendTelegram 重新發送 Telegram 通知
func (h *SensitiveWordAlertHandler) ResendTelegram(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var alert model.SensitiveWordAlert
	if err := h.db.First(&alert, id).Error; err != nil {
		common.HandleNotFoundError(c, "告警")
		return
	}

	if err := h.telegramService.SendAlert(&alert); err != nil {
		common.Error(c, common.CodeInternalError, "發送失敗: "+err.Error())
		return
	}

	now := time.Now()
	h.db.Model(&alert).Updates(map[string]interface{}{
		"telegram_sent":    true,
		"telegram_sent_at": &now,
	})

	common.Success(c, nil)
}
