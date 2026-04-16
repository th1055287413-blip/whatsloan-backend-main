package content

import (
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"

	"github.com/gin-gonic/gin"
)

// TranslationHandler 翻譯處理器
type TranslationHandler struct {
	translationService contentSvc.TranslationService
}

// NewTranslationHandler 創建翻譯處理器實例
func NewTranslationHandler(translationService contentSvc.TranslationService) *TranslationHandler {
	return &TranslationHandler{
		translationService: translationService,
	}
}

// Translate 翻譯文本
// @Summary 翻譯文本
// @Tags Translation
// @Accept json
// @Produce json
// @Param request body model.TranslationRequest true "翻譯請求"
// @Success 200 {object} model.TranslationResponse
// @Router /api/translation/translate [post]
func (h *TranslationHandler) Translate(c *gin.Context) {
	var req model.TranslationRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	translatedText, cached, err := h.translationService.TranslateWithCache(req.Text, req.TargetLanguage, req.SourceLanguage)
	if err != nil {
		common.HandleServiceError(c, err, "翻譯")
		return
	}

	common.SuccessWithMessage(c, "翻譯成功", model.TranslationResponse{
		TranslatedText: translatedText,
		SourceLanguage: req.SourceLanguage,
		Cached:         cached,
	})
}

// BatchTranslate 批量翻譯
// @Summary 批量翻譯
// @Tags Translation
// @Accept json
// @Produce json
// @Param request body model.BatchTranslationRequest true "批量翻譯請求"
// @Success 200 {object} []model.TranslationResponse
// @Router /api/translation/batch [post]
func (h *TranslationHandler) BatchTranslate(c *gin.Context) {
	var req model.BatchTranslationRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	results, err := h.translationService.BatchTranslate(req.Texts, req.TargetLanguage)
	if err != nil {
		common.HandleServiceError(c, err, "批量翻譯")
		return
	}

	common.SuccessWithMessage(c, "批量翻譯成功", results)
}

// getUserID 從上下文獲取用戶 ID
func getUserID(c *gin.Context) uint {
	if uid, exists := c.Get("user_id"); exists {
		return uid.(uint)
	}
	return 1 // 預設值
}

// GetLanguageConfigs 獲取語言配置列表
// @Summary 獲取語言配置列表
// @Tags Language
// @Produce json
// @Success 200 {array} model.LanguageConfig
// @Router /api/languages [get]
func (h *TranslationHandler) GetLanguageConfigs(c *gin.Context) {
	userID := getUserID(c)

	configs, err := h.translationService.GetLanguageConfigs(userID)
	if err != nil {
		common.HandleServiceError(c, err, "語言配置")
		return
	}

	common.SuccessWithMessage(c, "獲取語言配置成功", configs)
}

// CreateLanguageConfig 創建語言配置
// @Summary 創建語言配置
// @Tags Language
// @Accept json
// @Produce json
// @Param request body model.LanguageConfig true "語言配置"
// @Success 200 {object} model.LanguageConfig
// @Router /api/languages [post]
func (h *TranslationHandler) CreateLanguageConfig(c *gin.Context) {
	userID := getUserID(c)

	var config model.LanguageConfig
	if !common.BindAndValidate(c, &config) {
		return
	}

	config.UserID = userID

	if err := h.translationService.CreateLanguageConfig(&config); err != nil {
		common.HandleServiceError(c, err, "語言配置")
		return
	}

	common.SuccessWithMessage(c, "創建語言配置成功", config)
}

// UpdateLanguageConfig 更新語言配置
// @Summary 更新語言配置
// @Tags Language
// @Accept json
// @Produce json
// @Param id path int true "語言配置ID"
// @Param request body map[string]interface{} true "更新字段"
// @Success 200 {object} gin.H
// @Router /api/languages/:id [put]
func (h *TranslationHandler) UpdateLanguageConfig(c *gin.Context) {
	userID := getUserID(c)

	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var updates map[string]interface{}
	if !common.BindAndValidate(c, &updates) {
		return
	}

	if err := h.translationService.UpdateLanguageConfig(id, userID, updates); err != nil {
		common.HandleServiceError(c, err, "語言配置")
		return
	}

	common.SuccessWithMessage(c, "更新成功", nil)
}

// DeleteLanguageConfig 刪除語言配置
// @Summary 刪除語言配置
// @Tags Language
// @Param id path int true "語言配置ID"
// @Success 200 {object} gin.H
// @Router /api/languages/:id [delete]
func (h *TranslationHandler) DeleteLanguageConfig(c *gin.Context) {
	userID := getUserID(c)

	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.translationService.DeleteLanguageConfig(id, userID); err != nil {
		common.HandleServiceError(c, err, "語言配置")
		return
	}

	common.SuccessWithMessage(c, "刪除成功", nil)
}

// GetTranslationConfig 獲取翻譯配置
// @Summary 獲取翻譯配置
// @Tags Translation
// @Produce json
// @Success 200 {object} model.TranslationConfig
// @Router /api/translation/config [get]
func (h *TranslationHandler) GetTranslationConfig(c *gin.Context) {
	userID := getUserID(c)

	config, err := h.translationService.GetTranslationConfig(userID)
	if err != nil {
		common.HandleServiceError(c, err, "翻譯配置")
		return
	}

	common.SuccessWithMessage(c, "獲取翻譯配置成功", config)
}

// UpdateTranslationConfig 更新翻譯配置
// @Summary 更新翻譯配置
// @Tags Translation
// @Accept json
// @Produce json
// @Param request body map[string]interface{} true "更新字段"
// @Success 200 {object} gin.H
// @Router /api/translation/config [put]
func (h *TranslationHandler) UpdateTranslationConfig(c *gin.Context) {
	userID := getUserID(c)

	var updates map[string]interface{}
	if !common.BindAndValidate(c, &updates) {
		return
	}

	if err := h.translationService.UpdateTranslationConfig(userID, updates); err != nil {
		common.HandleServiceError(c, err, "翻譯配置")
		return
	}

	common.SuccessWithMessage(c, "更新成功", nil)
}
