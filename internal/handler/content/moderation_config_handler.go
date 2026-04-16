package content

import (
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"
	systemSvc "whatsapp_golang/internal/service/system"

	"github.com/gin-gonic/gin"
)

var moderationConfigKeys = map[string]bool{
	"sensitive_word.enabled": true,
	"telegram.enabled":      true,
	"telegram.bot_token":    true,
	"telegram.chat_id":      true,
}

// ModerationConfigHandler 敏感詞設定處理器
type ModerationConfigHandler struct {
	configService   systemSvc.ConfigService
	telegramService contentSvc.TelegramService
	opLogService    systemSvc.OperationLogService
}

// NewModerationConfigHandler 創建處理器
func NewModerationConfigHandler(
	configService systemSvc.ConfigService,
	telegramService contentSvc.TelegramService,
	opLogService systemSvc.OperationLogService,
) *ModerationConfigHandler {
	return &ModerationConfigHandler{
		configService:   configService,
		telegramService: telegramService,
		opLogService:    opLogService,
	}
}

// GetModerationConfigs 獲取敏感詞相關配置
func (h *ModerationConfigHandler) GetModerationConfigs(c *gin.Context) {
	allConfigs, err := h.configService.GetAllConfigItems()
	if err != nil {
		common.HandleDatabaseError(c, err, "獲取配置")
		return
	}

	filtered := make(map[string]string)
	for _, cfg := range allConfigs {
		if !moderationConfigKeys[cfg.ConfigKey] {
			continue
		}
		if cfg.IsSecret && cfg.ConfigValue != "" {
			filtered[cfg.ConfigKey] = "******"
		} else {
			filtered[cfg.ConfigKey] = cfg.ConfigValue
		}
	}

	common.Success(c, filtered)
}

// UpdateModerationConfig 更新敏感詞相關配置
func (h *ModerationConfigHandler) UpdateModerationConfig(c *gin.Context) {
	key := c.Param("key")

	if !moderationConfigKeys[key] {
		common.Error(c, common.CodeInvalidParams, "不允許修改此配置項: "+key)
		return
	}

	isSecret, _ := h.configService.IsSecretKey(key)
	beforeValue, _ := h.configService.GetConfig(key)

	var req struct {
		Value string `json:"value" binding:"required"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	username := c.GetString("username")
	if err := h.configService.SetConfig(key, req.Value, username); err != nil {
		common.HandleDatabaseError(c, err, "更新配置")
		return
	}

	logBefore := beforeValue
	logAfter := req.Value
	if isSecret {
		logBefore = "******"
		logAfter = "******"
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpConfigChange,
		ResourceType:  model.ResConfig,
		ResourceID:    key,
		ResourceName:  key,
		BeforeValue:   map[string]interface{}{"value": logBefore},
		AfterValue:    map[string]interface{}{"value": logAfter},
	}, c)

	common.Success(c, nil)
}

// TestTelegram 測試 Telegram 連接
func (h *ModerationConfigHandler) TestTelegram(c *gin.Context) {
	info, err := h.telegramService.GetBotInfo()
	if err != nil {
		common.Error(c, common.CodeInternalError, "連接失敗: "+err.Error())
		return
	}

	common.Success(c, info)
}
