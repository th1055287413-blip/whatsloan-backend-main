package common

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// PaginationParams 分頁參數
type PaginationParams struct {
	Page      int
	PageSize  int
	SortBy    string
	SortOrder string
}

// DefaultPagination 預設分頁參數
var DefaultPagination = PaginationParams{
	Page:      1,
	PageSize:  20,
	SortBy:    "created_at",
	SortOrder: "desc",
}

// ParseUintFromString 從字串解析 uint
func ParseUintFromString(s string) (uint, error) {
	id, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint(id), nil
}

// ParseUintParam 從 URL 路徑參數解析 uint
func ParseUintParam(c *gin.Context, paramName string) (uint, error) {
	idStr := c.Param(paramName)
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint(id), nil
}

// ParseUint64Param 從 URL 路徑參數解析 uint64
func ParseUint64Param(c *gin.Context, paramName string) (uint64, error) {
	idStr := c.Param(paramName)
	return strconv.ParseUint(idStr, 10, 64)
}

// ParseIntParam 從 URL 路徑參數解析 int
func ParseIntParam(c *gin.Context, paramName string) (int, error) {
	idStr := c.Param(paramName)
	return strconv.Atoi(idStr)
}

// MustParseUintParam 從 URL 路徑參數解析 uint，失敗時回傳錯誤響應
// 返回 false 表示解析失敗且已發送錯誤響應
func MustParseUintParam(c *gin.Context, paramName string) (uint, bool) {
	id, err := ParseUintParam(c, paramName)
	if err != nil {
		Error(c, CodeInvalidParams, "無效的 "+paramName)
		return 0, false
	}
	return id, true
}

// MustParseID 從 URL 路徑參數 "id" 解析 uint
// 返回 false 表示解析失敗且已發送錯誤響應
func MustParseID(c *gin.Context) (uint, bool) {
	return MustParseUintParam(c, "id")
}

// ParsePaginationParams 從查詢參數解析分頁參數
func ParsePaginationParams(c *gin.Context) PaginationParams {
	return ParsePaginationParamsWithDefaults(c, DefaultPagination)
}

// ParsePaginationParamsWithDefaults 從查詢參數解析分頁參數 (自定義預設值)
func ParsePaginationParamsWithDefaults(c *gin.Context, defaults PaginationParams) PaginationParams {
	page, _ := strconv.Atoi(c.DefaultQuery("page", strconv.Itoa(defaults.Page)))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(defaults.PageSize)))

	// 兼容舊版參數名
	if pageSize == defaults.PageSize {
		if size, err := strconv.Atoi(c.Query("size")); err == nil && size > 0 {
			pageSize = size
		}
		if limit, err := strconv.Atoi(c.Query("limit")); err == nil && limit > 0 {
			pageSize = limit
		}
	}

	// 驗證範圍
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaults.PageSize
	}
	if pageSize > 100 {
		pageSize = 100
	}

	sortBy := c.DefaultQuery("sort_by", defaults.SortBy)
	sortOrder := c.DefaultQuery("sort_order", defaults.SortOrder)
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = defaults.SortOrder
	}

	return PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    sortBy,
		SortOrder: sortOrder,
	}
}

// Offset 計算資料庫查詢的偏移量
func (p PaginationParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// Limit 回傳頁面大小
func (p PaginationParams) Limit() int {
	return p.PageSize
}

// FilterParams 過濾參數
type FilterParams map[string]interface{}

// ParseFilterParams 從查詢參數解析過濾參數
func ParseFilterParams(c *gin.Context, allowedFields []string) FilterParams {
	filters := make(FilterParams)
	for _, field := range allowedFields {
		if value := c.Query(field); value != "" {
			filters[field] = value
		}
	}
	return filters
}

// ParseFilterParamsWithKeyword 解析過濾參數並包含關鍵字搜尋
func ParseFilterParamsWithKeyword(c *gin.Context, allowedFields []string) FilterParams {
	filters := ParseFilterParams(c, allowedFields)

	// 常用的關鍵字參數
	if keyword := c.Query("keyword"); keyword != "" {
		filters["keyword"] = keyword
	}
	if search := c.Query("search"); search != "" {
		filters["keyword"] = search
	}
	if q := c.Query("q"); q != "" {
		filters["keyword"] = q
	}

	return filters
}

// Get 取得過濾參數值
func (f FilterParams) Get(key string) (interface{}, bool) {
	val, ok := f[key]
	return val, ok
}

// GetString 取得字串類型的過濾參數
func (f FilterParams) GetString(key string) string {
	if val, ok := f[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// Has 檢查是否有此過濾參數
func (f FilterParams) Has(key string) bool {
	_, ok := f[key]
	return ok
}
