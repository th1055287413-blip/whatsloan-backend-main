package tag

import (
	"strconv"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	tagSvc "whatsapp_golang/internal/service/tag"

	"github.com/gin-gonic/gin"
)

// TagHandler 標籤處理器
type TagHandler struct {
	tagService tagSvc.TagService
}

// NewTagHandler 創建標籤處理器實例
func NewTagHandler(tagService tagSvc.TagService) *TagHandler {
	return &TagHandler{
		tagService: tagService,
	}
}

// CreateTag 創建標籤
func (h *TagHandler) CreateTag(c *gin.Context) {
	var tag model.AccountTag
	if !common.BindAndValidate(c, &tag) {
		return
	}

	if tag.Name == "" {
		common.Error(c, common.CodeInvalidParams, "標籤名稱不能為空")
		return
	}
	if tag.Color == "" {
		common.Error(c, common.CodeInvalidParams, "標籤顏色不能為空")
		return
	}

	if err := h.tagService.CreateTag(&tag); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "創建標籤成功", tag)
}

// UpdateTag 更新標籤
func (h *TagHandler) UpdateTag(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var tag model.AccountTag
	if !common.BindAndValidate(c, &tag) {
		return
	}

	if err := h.tagService.UpdateTag(id, &tag); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "更新標籤成功", tag)
}

// DeleteTag 刪除標籤
func (h *TagHandler) DeleteTag(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.tagService.DeleteTag(id); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "刪除標籤成功", nil)
}

// GetTag 獲取標籤詳情
func (h *TagHandler) GetTag(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	tag, err := h.tagService.GetTag(id)
	if err != nil {
		common.HandleNotFoundError(c, "標籤")
		return
	}

	common.SuccessWithMessage(c, "獲取標籤成功", tag)
}

// GetTagList 獲取標籤列表
func (h *TagHandler) GetTagList(c *gin.Context) {
	params := common.ParsePaginationParams(c)
	tagTypeStr := c.Query("tag_type")
	query := c.Query("query")
	color := c.Query("color")
	minAccountCountStr := c.Query("min_account_count")

	var tagType *model.TagType
	if tagTypeStr != "" {
		tt := model.TagType(tagTypeStr)
		tagType = &tt
	}

	var minAccountCount *int
	if minAccountCountStr != "" {
		count, err := strconv.Atoi(minAccountCountStr)
		if err == nil {
			minAccountCount = &count
		}
	}

	filters := map[string]interface{}{
		"tag_type": tagType,
	}
	if query != "" {
		filters["query"] = query
	}
	if color != "" {
		filters["color"] = color
	}
	if minAccountCount != nil {
		filters["min_account_count"] = *minAccountCount
	}

	tags, total, err := h.tagService.GetTagList(params.Page, params.PageSize, filters)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.PaginatedList(c, tags, total, params.Page, params.PageSize)
}

// AddAccountTags 為帳號添加標籤
func (h *TagHandler) AddAccountTags(c *gin.Context) {
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req struct {
		TagIDs []uint `json:"tag_ids" binding:"required"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.tagService.AddAccountTags(accountID, req.TagIDs); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "添加標籤成功", nil)
}

// RemoveAccountTag 移除帳號標籤
func (h *TagHandler) RemoveAccountTag(c *gin.Context) {
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	tagID, ok := common.MustParseUintParam(c, "tagId")
	if !ok {
		return
	}

	if err := h.tagService.RemoveAccountTag(accountID, tagID); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "移除標籤成功", nil)
}

// GetAccountTags 獲取帳號的所有標籤
func (h *TagHandler) GetAccountTags(c *gin.Context) {
	accountID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	tags, err := h.tagService.GetAccountTags(accountID)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "獲取帳號標籤成功", tags)
}

// BatchAddAccountTags 批量為帳號添加標籤
func (h *TagHandler) BatchAddAccountTags(c *gin.Context) {
	var req struct {
		AccountIDs []uint `json:"account_ids" binding:"required"`
		TagIDs     []uint `json:"tag_ids" binding:"required"`
	}
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.tagService.BatchAddAccountTags(req.AccountIDs, req.TagIDs); err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "批量添加標籤成功", nil)
}

// GetTagStatistics 獲取標籤統計資訊
func (h *TagHandler) GetTagStatistics(c *gin.Context) {
	tagID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	stats, err := h.tagService.GetTagStatistics(tagID)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "獲取標籤統計成功", stats)
}

// GetAllTagsStatistics 獲取所有標籤的統計資訊
func (h *TagHandler) GetAllTagsStatistics(c *gin.Context) {
	statsList, err := h.tagService.GetAllTagsStatistics()
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "獲取標籤統計成功", statsList)
}

// GetTagTrendData 獲取標籤趨勢數據
func (h *TagHandler) GetTagTrendData(c *gin.Context) {
	tagID, ok := common.MustParseID(c)
	if !ok {
		return
	}

	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))

	trendData, err := h.tagService.GetTagTrendData(tagID, days)
	if err != nil {
		common.Error(c, common.CodeInternalError, err.Error())
		return
	}

	common.SuccessWithMessage(c, "獲取標籤趨勢數據成功", trendData)
}

