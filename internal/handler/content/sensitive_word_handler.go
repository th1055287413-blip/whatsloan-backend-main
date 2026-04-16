package content

import (
	"bytes"
	"fmt"
	"net/http"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"

	"github.com/gin-gonic/gin"
)

// SensitiveWordHandler 敏感詞處理器
type SensitiveWordHandler struct {
	svc contentSvc.SensitiveWordService
}

// NewSensitiveWordHandler 創建處理器
func NewSensitiveWordHandler(svc contentSvc.SensitiveWordService) *SensitiveWordHandler {
	return &SensitiveWordHandler{svc: svc}
}

// CreateWord 創建敏感詞
func (h *SensitiveWordHandler) CreateWord(c *gin.Context) {
	var word model.SensitiveWord
	if !common.BindAndValidate(c, &word) {
		return
	}

	// 設置創建人
	if username, exists := c.Get("username"); exists {
		word.CreatedBy = username.(string)
	}

	if err := h.svc.CreateWord(&word); err != nil {
		common.HandleDatabaseError(c, err, "創建敏感詞")
		return
	}

	common.Success(c, word)
}

// UpdateWord 更新敏感詞
func (h *SensitiveWordHandler) UpdateWord(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var word model.SensitiveWord
	if !common.BindAndValidate(c, &word) {
		return
	}

	if err := h.svc.UpdateWord(id, &word); err != nil {
		common.HandleDatabaseError(c, err, "更新敏感詞")
		return
	}

	common.Success(c, word)
}

// DeleteWord 刪除敏感詞
func (h *SensitiveWordHandler) DeleteWord(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.svc.DeleteWord(id); err != nil {
		common.HandleDatabaseError(c, err, "刪除敏感詞")
		return
	}

	common.Success(c, nil)
}

// GetWord 獲取敏感詞
func (h *SensitiveWordHandler) GetWord(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	word, err := h.svc.GetWord(id)
	if err != nil {
		common.HandleNotFoundError(c, "敏感詞")
		return
	}

	common.Success(c, word)
}

// ListWords 列出敏感詞
func (h *SensitiveWordHandler) ListWords(c *gin.Context) {
	params := common.ParsePaginationParams(c)

	filter := common.ParseFilterParams(c, []string{"category", "matchType"})
	if enabled := c.Query("enabled"); enabled != "" {
		filter["enabled"] = enabled == "true"
	}

	words, total, err := h.svc.ListWords(params.Page, params.PageSize, filter)
	if err != nil {
		common.HandleDatabaseError(c, err, "查詢敏感詞")
		return
	}

	common.PaginatedList(c, words, total, params.Page, params.PageSize)
}

// BatchImport 批量導入
func (h *SensitiveWordHandler) BatchImport(c *gin.Context) {
	var req struct {
		Words []*model.SensitiveWord `json:"words" binding:"required"`
	}

	if !common.BindAndValidate(c, &req) {
		return
	}

	// 設置創建人
	if username, exists := c.Get("username"); exists {
		for _, word := range req.Words {
			word.CreatedBy = username.(string)
		}
	}

	if err := h.svc.BatchImport(req.Words); err != nil {
		common.HandleDatabaseError(c, err, "導入敏感詞")
		return
	}

	common.Success(c, gin.H{"count": len(req.Words)})
}

// BatchDelete 批量刪除
func (h *SensitiveWordHandler) BatchDelete(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}

	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.svc.BatchDelete(req.IDs); err != nil {
		common.HandleDatabaseError(c, err, "刪除敏感詞")
		return
	}

	common.Success(c, nil)
}

// Export 導出敏感詞
func (h *SensitiveWordHandler) Export(c *gin.Context) {
	words, _, err := h.svc.ListWords(1, 10000, nil)
	if err != nil {
		common.HandleDatabaseError(c, err, "導出敏感詞")
		return
	}

	// 轉換為 CSV 格式
	var buf bytes.Buffer
	buf.WriteString("敏感詞,匹配類型,分類,優先級,說明\n")
	for _, word := range words {
		buf.WriteString(fmt.Sprintf("%s,%s,%s,%d,%s\n",
			word.Word, word.MatchType, word.Category, word.Priority, word.Description))
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=sensitive_words.csv")
	c.Data(http.StatusOK, "text/csv", buf.Bytes())
}

// RefreshCache 刷新緩存
func (h *SensitiveWordHandler) RefreshCache(c *gin.Context) {
	if err := h.svc.RefreshCache(); err != nil {
		common.Error(c, common.CodeInternalError, "刷新失敗: "+err.Error())
		return
	}

	common.Success(c, nil)
}
