package common

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIResponse 統一 API 響應格式
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// PaginatedData 分頁數據格式
type PaginatedData struct {
	List       interface{} `json:"list"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

// LegacyPaginatedResponse 舊版分頁響應格式 (向後相容)
type LegacyPaginatedResponse struct {
	Items interface{} `json:"items"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Size  int         `json:"size"`
	Pages int         `json:"pages"`
}

// 響應碼常量
const (
	CodeSuccess          = 0
	CodeInvalidParams    = 1001
	CodeAuthFailed       = 1002
	CodePermissionDenied = 1003
	CodeResourceNotFound = 1004
	CodeResourceExists   = 1005
	CodeWhatsAppError    = 2001
	CodeQRExpired        = 2002
	CodeSessionNotFound  = 2003
	CodeRateLimited      = 2004
	CodeInternalError    = 5001
	CodeDatabaseError    = 5002
)

// Success 成功響應
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, APIResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    data,
	})
}

// SuccessWithMessage 帶訊息的成功響應
func SuccessWithMessage(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, APIResponse{
		Code:    CodeSuccess,
		Message: message,
		Data:    data,
	})
}

// Error 錯誤響應
func Error(c *gin.Context, code int, message string) {
	httpStatus := codeToHTTPStatus(code)
	c.JSON(httpStatus, APIResponse{
		Code:    code,
		Message: message,
		Data:    nil,
	})
}

// ErrorWithData 帶數據的錯誤響應
func ErrorWithData(c *gin.Context, code int, message string, data interface{}) {
	httpStatus := codeToHTTPStatus(code)
	c.JSON(httpStatus, APIResponse{
		Code:    code,
		Message: message,
		Data:    data,
	})
}

// codeToHTTPStatus 將業務碼轉換為 HTTP 狀態碼
func codeToHTTPStatus(code int) int {
	switch code {
	case CodeInvalidParams:
		return http.StatusBadRequest
	case CodeAuthFailed:
		return http.StatusUnauthorized
	case CodePermissionDenied:
		return http.StatusForbidden
	case CodeResourceNotFound:
		return http.StatusNotFound
	case CodeResourceExists:
		return http.StatusConflict
	case CodeWhatsAppError, CodeQRExpired, CodeSessionNotFound:
		return http.StatusBadRequest
	case CodeRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// PaginatedList 新版分頁響應 (統一格式)
func PaginatedList(c *gin.Context, list interface{}, total int64, page, pageSize int) {
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	Success(c, PaginatedData{
		List:       list,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// Paginated 舊版分頁響應 (向後相容)
func Paginated(c *gin.Context, items interface{}, total int64, page, size int) {
	pages := int(total) / size
	if int(total)%size > 0 {
		pages++
	}

	Success(c, LegacyPaginatedResponse{
		Items: items,
		Total: total,
		Page:  page,
		Size:  size,
		Pages: pages,
	})
}

// SuccessList 列表響應 (無分頁)
func SuccessList(c *gin.Context, list interface{}, total int64) {
	Success(c, gin.H{
		"list":  list,
		"total": total,
	})
}

// Created 創建成功響應
func Created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, APIResponse{
		Code:    CodeSuccess,
		Message: "created",
		Data:    data,
	})
}

// NoContent 無內容響應 (用於刪除成功)
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
