package common

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 常見錯誤訊息模板
const (
	MsgInvalidID       = "無效的 ID"
	MsgInvalidParams   = "無效的參數"
	MsgNotFound        = "資源不存在"
	MsgCreateFailed    = "建立失敗"
	MsgUpdateFailed    = "更新失敗"
	MsgDeleteFailed    = "刪除失敗"
	MsgQueryFailed     = "查詢失敗"
	MsgUnauthorized    = "未授權"
	MsgForbidden       = "無權限"
	MsgInternalError   = "內部錯誤"
	MsgAlreadyExists   = "資源已存在"
	MsgOperationFailed = "操作失敗"
)

// HandleServiceError 統一處理服務層錯誤
func HandleServiceError(c *gin.Context, err error, resourceName string) {
	if err == nil {
		return
	}

	// 處理 GORM 特定錯誤
	if errors.Is(err, gorm.ErrRecordNotFound) {
		Error(c, CodeResourceNotFound, resourceName+"不存在")
		return
	}

	// 處理重複鍵錯誤
	if isDuplicateKeyError(err) {
		Error(c, CodeResourceExists, resourceName+"已存在")
		return
	}

	// 預設為內部錯誤
	Error(c, CodeInternalError, err.Error())
}

// HandleNotFoundError 處理資源不存在錯誤
func HandleNotFoundError(c *gin.Context, resourceName string) {
	Error(c, CodeResourceNotFound, resourceName+"不存在")
}

// HandleValidationError 處理驗證錯誤
func HandleValidationError(c *gin.Context, err error) {
	if err == nil {
		Error(c, CodeInvalidParams, MsgInvalidParams)
		return
	}
	Error(c, CodeInvalidParams, err.Error())
}

// HandleBindError 處理參數綁定錯誤
func HandleBindError(c *gin.Context, err error) {
	if err == nil {
		Error(c, CodeInvalidParams, MsgInvalidParams)
		return
	}

	// 簡化錯誤訊息
	msg := err.Error()
	if strings.Contains(msg, "cannot unmarshal") {
		msg = "參數格式錯誤"
	} else if strings.Contains(msg, "required") {
		msg = "缺少必要參數"
	}

	Error(c, CodeInvalidParams, msg)
}

// HandleDatabaseError 處理資料庫錯誤
func HandleDatabaseError(c *gin.Context, err error, operation string) {
	if err == nil {
		return
	}

	// 處理常見的資料庫錯誤
	if errors.Is(err, gorm.ErrRecordNotFound) {
		Error(c, CodeResourceNotFound, MsgNotFound)
		return
	}

	if isDuplicateKeyError(err) {
		Error(c, CodeResourceExists, MsgAlreadyExists)
		return
	}

	Error(c, CodeDatabaseError, operation+"失敗: "+err.Error())
}

// HandleUnauthorized 處理未授權錯誤
func HandleUnauthorized(c *gin.Context, message ...string) {
	msg := MsgUnauthorized
	if len(message) > 0 {
		msg = message[0]
	}
	Error(c, CodeAuthFailed, msg)
}

// HandleForbidden 處理無權限錯誤
func HandleForbidden(c *gin.Context, message ...string) {
	msg := MsgForbidden
	if len(message) > 0 {
		msg = message[0]
	}
	Error(c, CodePermissionDenied, msg)
}

// isDuplicateKeyError 檢查是否為重複鍵錯誤
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "duplicate") ||
		strings.Contains(errStr, "unique constraint") ||
		strings.Contains(errStr, "already exists")
}

// BindAndValidate 綁定並驗證請求參數
// 返回 false 表示綁定失敗且已發送錯誤響應
func BindAndValidate(c *gin.Context, obj interface{}) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		HandleBindError(c, err)
		return false
	}
	return true
}

// BindQueryAndValidate 綁定並驗證查詢參數
// 返回 false 表示綁定失敗且已發送錯誤響應
func BindQueryAndValidate(c *gin.Context, obj interface{}) bool {
	if err := c.ShouldBindQuery(obj); err != nil {
		HandleBindError(c, err)
		return false
	}
	return true
}
