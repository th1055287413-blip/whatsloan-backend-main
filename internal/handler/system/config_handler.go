package system

import (
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"
	systemSvc "whatsapp_golang/internal/service/system"

	"github.com/gin-gonic/gin"
)

const secretMask = "******"

// ConfigHandler 系統配置處理器
type ConfigHandler struct {
	configService   systemSvc.ConfigService
	telegramService contentSvc.TelegramService
	opLogService    systemSvc.OperationLogService
}

// NewConfigHandler 創建處理器
func NewConfigHandler(
	configService systemSvc.ConfigService,
	telegramService contentSvc.TelegramService,
	opLogService systemSvc.OperationLogService,
) *ConfigHandler {
	return &ConfigHandler{
		configService:   configService,
		telegramService: telegramService,
		opLogService:    opLogService,
	}
}

// GetConfigs 獲取所有配置
func (h *ConfigHandler) GetConfigs(c *gin.Context) {
	configs, err := h.configService.GetAllConfigItems()
	if err != nil {
		common.HandleDatabaseError(c, err, "獲取配置")
		return
	}

	for i := range configs {
		if configs[i].IsSecret && configs[i].ConfigValue != "" {
			configs[i].ConfigValue = secretMask
		}
	}

	common.Success(c, configs)
}

// GetConfig 獲取單個配置
func (h *ConfigHandler) GetConfig(c *gin.Context) {
	key := c.Param("key")
	value, err := h.configService.GetConfig(key)
	if err != nil {
		common.HandleNotFoundError(c, "配置")
		return
	}

	if isSecret, _ := h.configService.IsSecretKey(key); isSecret && value != "" {
		value = secretMask
	}

	common.Success(c, gin.H{"key": key, "value": value})
}

// UpdateConfig 更新配置
func (h *ConfigHandler) UpdateConfig(c *gin.Context) {
	key := c.Param("key")

	var req struct {
		Value string `json:"value" binding:"required"`
	}

	if !common.BindAndValidate(c, &req) {
		return
	}

	// 判斷是否為 secret，決定 log 內容
	isSecret, _ := h.configService.IsSecretKey(key)
	beforeValue, _ := h.configService.GetConfig(key)

	username := c.GetString("username")
	if err := h.configService.SetConfig(key, req.Value, username); err != nil {
		common.HandleDatabaseError(c, err, "更新配置")
		return
	}

	logBefore := beforeValue
	logAfter := req.Value
	if isSecret {
		logBefore = secretMask
		logAfter = secretMask
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
func (h *ConfigHandler) TestTelegram(c *gin.Context) {
	info, err := h.telegramService.GetBotInfo()
	if err != nil {
		common.Error(c, common.CodeInternalError, "連接失敗: "+err.Error())
		return
	}

	common.Success(c, info)
}
