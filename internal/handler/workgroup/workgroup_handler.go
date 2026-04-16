package workgroup

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	systemSvc "whatsapp_golang/internal/service/system"
	workgroupSvc "whatsapp_golang/internal/service/workgroup"
)

// WorkgroupHandler Admin 工作組管理處理器
type WorkgroupHandler struct {
	svc          workgroupSvc.WorkgroupService
	opLogService systemSvc.OperationLogService
}

// NewWorkgroupHandler 建立工作組處理器
func NewWorkgroupHandler(svc workgroupSvc.WorkgroupService, opLogService systemSvc.OperationLogService) *WorkgroupHandler {
	return &WorkgroupHandler{svc: svc, opLogService: opLogService}
}

// List 工作組列表
func (h *WorkgroupHandler) List(c *gin.Context) {
	p := common.ParsePaginationParams(c)
	filters := common.ParseFilterParamsWithKeyword(c, []string{"status", "type"})

	items, total, err := h.svc.List(p.Page, p.PageSize, filters)
	if err != nil {
		common.HandleServiceError(c, err, "工作組")
		return
	}
	common.PaginatedList(c, items, total, p.Page, p.PageSize)
}

// GetByID 工作組詳情
func (h *WorkgroupHandler) GetByID(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	wg, err := h.svc.GetByID(id)
	if err != nil {
		common.HandleServiceError(c, err, "工作組")
		return
	}
	common.Success(c, wg)
}

type createWorkgroupRequest struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Type        string `json:"type" binding:"required,oneof=sales marketing admin"`
	Description string `json:"description"`
}

// Create 建立工作組
func (h *WorkgroupHandler) Create(c *gin.Context) {
	var req createWorkgroupRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	adminID, _ := c.Get("user_id")
	wg := &model.Workgroup{
		Code:        req.Code,
		Name:        req.Name,
		Type:        req.Type,
		Description: req.Description,
		CreatedBy:   adminID.(uint),
	}

	if err := h.svc.Create(wg); err != nil {
		common.HandleServiceError(c, err, "工作組")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpCreate,
		ResourceType:  model.ResWorkgroup,
		ResourceID:    fmt.Sprint(wg.ID),
		ResourceName:  wg.Name,
		AfterValue:    map[string]interface{}{"code": wg.Code, "name": wg.Name},
	}, c)

	common.Created(c, wg)
}

type updateWorkgroupRequest struct {
	Code        *string `json:"code"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
}

// Update 更新工作組
func (h *WorkgroupHandler) Update(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req updateWorkgroupRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	updates := make(map[string]interface{})
	if req.Code != nil {
		updates["code"] = *req.Code
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}

	if err := h.svc.Update(id, updates); err != nil {
		common.HandleServiceError(c, err, "工作組")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResWorkgroup,
		ResourceID:    fmt.Sprint(id),
		AfterValue:    updates,
	}, c)

	common.Success(c, nil)
}

// Archive 封存工作組
func (h *WorkgroupHandler) Archive(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	if err := h.svc.Archive(id); err != nil {
		common.HandleServiceError(c, err, "工作組")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpUpdate,
		ResourceType:  model.ResWorkgroup,
		ResourceID:    fmt.Sprint(id),
		AfterValue:    map[string]interface{}{"status": "archived"},
	}, c)

	common.Success(c, nil)
}

// GetAccounts 工作組帳號列表
func (h *WorkgroupHandler) GetAccounts(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}
	p := common.ParsePaginationParams(c)

	items, total, err := h.svc.GetAccounts(id, p.Page, p.PageSize)
	if err != nil {
		common.HandleServiceError(c, err, "工作組帳號")
		return
	}
	common.PaginatedList(c, items, total, p.Page, p.PageSize)
}

type assignAccountsRequest struct {
	AccountIDs []uint `json:"account_ids" binding:"required,min=1"`
}

// AssignAccounts 分配帳號到工作組
func (h *WorkgroupHandler) AssignAccounts(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req assignAccountsRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	adminID, _ := c.Get("user_id")
	if err := h.svc.AssignAccounts(id, req.AccountIDs, adminID.(uint)); err != nil {
		common.HandleServiceError(c, err, "帳號分配")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpCreate,
		ResourceType:  model.ResWorkgroupAcct,
		ResourceID:    fmt.Sprint(id),
		AfterValue:    map[string]interface{}{"account_ids": req.AccountIDs},
	}, c)

	common.Success(c, nil)
}

type removeAccountsRequest struct {
	AccountIDs []uint `json:"account_ids" binding:"required,min=1"`
}

// RemoveAccounts 移除帳號
func (h *WorkgroupHandler) RemoveAccounts(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req removeAccountsRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	if err := h.svc.RemoveAccounts(id, req.AccountIDs); err != nil {
		common.HandleServiceError(c, err, "帳號移除")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpDelete,
		ResourceType:  model.ResWorkgroupAcct,
		ResourceID:    fmt.Sprint(id),
		AfterValue:    map[string]interface{}{"account_ids": req.AccountIDs},
	}, c)

	common.Success(c, nil)
}

// GetAssignableAccountsCount 按條件查詢可分配帳號數量
func (h *WorkgroupHandler) GetAssignableAccountsCount(c *gin.Context) {
	filter := parseConditionFilter(c)
	workgroupType := c.Query("workgroup_type")

	count, err := h.svc.CountAssignableByCondition(workgroupType, filter)
	if err != nil {
		common.HandleServiceError(c, err, "可分配帳號")
		return
	}
	common.Success(c, gin.H{"count": count})
}

type assignByConditionRequest struct {
	TagIDs              []uint `json:"tag_ids"`
	AuthorizedMinutesGT *int   `json:"authorized_minutes_gt"`
	Count               int    `json:"count" binding:"required,min=1"`
}

// AssignAccountsByCondition 按條件批量分配帳號到工作組
func (h *WorkgroupHandler) AssignAccountsByCondition(c *gin.Context) {
	id, ok := common.MustParseID(c)
	if !ok {
		return
	}

	var req assignByConditionRequest
	if !common.BindAndValidate(c, &req) {
		return
	}

	filter := workgroupSvc.AssignableConditionFilter{
		TagIDs:              req.TagIDs,
		AuthorizedMinutesGT: req.AuthorizedMinutesGT,
	}

	adminID, _ := c.Get("user_id")
	assigned, err := h.svc.AssignAccountsByCondition(id, filter, req.Count, adminID.(uint))
	if err != nil {
		common.HandleServiceError(c, err, "帳號分配")
		return
	}

	h.opLogService.LogAsync(&model.LogEntry{
		OperationType: model.OpCreate,
		ResourceType:  model.ResWorkgroupAcct,
		ResourceID:    fmt.Sprint(id),
		AfterValue: map[string]interface{}{
			"assigned_count":       assigned,
			"tag_ids":              req.TagIDs,
			"authorized_minutes_gt": req.AuthorizedMinutesGT,
		},
	}, c)

	common.Success(c, gin.H{"assigned_count": assigned})
}

func parseConditionFilter(c *gin.Context) workgroupSvc.AssignableConditionFilter {
	filter := workgroupSvc.AssignableConditionFilter{}

	// 解析 tag_ids: ?tag_ids=1&tag_ids=2 或 ?tag_ids[]=1&tag_ids[]=2
	tagIDStrs := c.QueryArray("tag_ids")
	if len(tagIDStrs) == 0 {
		tagIDStrs = c.QueryArray("tag_ids[]")
	}
	for _, s := range tagIDStrs {
		if id, err := strconv.ParseUint(s, 10, 32); err == nil {
			filter.TagIDs = append(filter.TagIDs, uint(id))
		}
	}

	if v := c.Query("authorized_minutes_gt"); v != "" {
		if mins, err := strconv.Atoi(v); err == nil {
			filter.AuthorizedMinutesGT = &mins
		}
	}

	return filter
}

// GetAssignableAccounts 可分配帳號列表
func (h *WorkgroupHandler) GetAssignableAccounts(c *gin.Context) {
	p := common.ParsePaginationParams(c)

	// 解析 user_data 篩選條件
	userDataFilters := make(map[string]string)
	for _, key := range []string{"occupation", "education", "monthlyIncome", "maritalStatus"} {
		if val := c.Query(key); val != "" {
			userDataFilters[key] = val
		}
	}

	filters := workgroupSvc.AssignableAccountsFilter{
		Page:            p.Page,
		PageSize:        p.PageSize,
		Keyword:         c.Query("keyword"),
		Status:          c.Query("status"),
		WorkgroupType:   c.Query("workgroup_type"),
		UserDataFilters: userDataFilters,
	}

	items, total, err := h.svc.GetAssignableAccounts(filters)
	if err != nil {
		common.HandleServiceError(c, err, "可分配帳號")
		return
	}
	common.PaginatedList(c, items, total, p.Page, p.PageSize)
}
