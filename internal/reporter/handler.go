package reporter

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"whatsapp_golang/internal/model"
	systemSvc "whatsapp_golang/internal/service/system"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Handler reporter HTTP handler
type Handler struct {
	db        *gorm.DB
	umamiSvc  systemSvc.UmamiService
	configSvc systemSvc.ConfigService
}

// NewHandler 建立 reporter handler
func NewHandler(db *gorm.DB, umamiSvc systemSvc.UmamiService, configSvc systemSvc.ConfigService) *Handler {
	return &Handler{
		db:        db,
		umamiSvc:  umamiSvc,
		configSvc: configSvc,
	}
}

// ChannelReportRequest 報表查詢請求
type ChannelReportRequest struct {
	ChannelCode string `json:"channel_code" binding:"required"`
	Password    string `json:"password" binding:"required"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
}

// ChannelReportResponse 報表查詢回應
type ChannelReportResponse struct {
	Stats       *systemSvc.UmamiStats    `json:"stats"`
	Funnel      []systemSvc.FunnelResult `json:"funnel"`
	FunnelError string                 `json:"funnel_error,omitempty"`
}

// GetReport 取得 channel 報表
func (h *Handler) GetReport(c *gin.Context) {
	var req ChannelReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// 查詢 channel，Preload PromotionDomain
	var channel model.Channel
	if err := h.db.Preload("PromotionDomain").
		Where("channel_code = ? AND deleted_at IS NULL", req.ChannelCode).
		First(&channel).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid channel code or password"})
		return
	}

	// 驗證密碼
	if channel.ViewerPassword == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid channel code or password"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(channel.ViewerPassword), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid channel code or password"})
		return
	}

	// 從 PromotionDomain pixels 取 umami 設定
	umamiPixel := getUmamiPixel(channel.PromotionDomain)
	if umamiPixel == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "umami pixel not configured for this domain"})
		return
	}

	websiteID := getPixelParam(umamiPixel, "data-website-id")
	if websiteID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "umami website ID not configured for this domain"})
		return
	}

	// funnelReportID 從 domain pixel params 讀取（不同 domain 有不同 funnel）
	funnelReportID := getPixelParam(umamiPixel, "funnel-report-id")

	// 計算日期範圍
	startAt, endAt := h.parseDateRange(req.StartDate, req.EndDate)

	// 並行呼叫 Umami API
	var (
		stats     *systemSvc.UmamiStats
		funnel    []systemSvc.FunnelResult
		statsErr  error
		funnelErr error
		wg        sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		stats, statsErr = h.umamiSvc.GetStats(websiteID, req.ChannelCode, startAt, endAt)
	}()

	go func() {
		defer wg.Done()
		if funnelReportID == "" {
			funnelErr = fmt.Errorf("funnel_report_id not configured (pixel/db/env all empty)")
			return
		}
		steps, window, err := h.umamiSvc.GetFunnelSteps(funnelReportID)
		if err != nil {
			funnelErr = err
			return
		}
		funnel, funnelErr = h.umamiSvc.RunFunnel(websiteID, req.ChannelCode, steps, window, startAt, endAt)
	}()

	wg.Wait()

	if statsErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stats: " + statsErr.Error()})
		return
	}

	// funnel 錯誤不阻擋回應，但回傳錯誤訊息
	resp := ChannelReportResponse{
		Stats:  stats,
		Funnel: funnel,
	}
	if funnelErr != nil {
		resp.FunnelError = funnelErr.Error()
	}

	c.JSON(http.StatusOK, resp)
}

// getUmamiPixel 從 PromotionDomain 的 pixels 找到 umami pixel
func getUmamiPixel(domain *model.PromotionDomain) *model.ChannelPixel {
	if domain == nil {
		return nil
	}
	for i := range domain.Pixels {
		if domain.Pixels[i].Platform == "umami" {
			return &domain.Pixels[i]
		}
	}
	return nil
}

// getPixelParam 從 pixel params 取值
func getPixelParam(pixel *model.ChannelPixel, key string) string {
	if pixel == nil || pixel.Params == nil {
		return ""
	}
	if v, ok := pixel.Params[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// parseDateRange 解析日期範圍，預設當天
func (h *Handler) parseDateRange(startDate, endDate string) (int64, int64) {
	loc, _ := time.LoadLocation("Asia/Taipei")
	now := time.Now().In(loc)
	layout := "2006-01-02"

	end := now
	if endDate != "" {
		if t, err := time.ParseInLocation(layout, endDate, loc); err == nil {
			end = t.Add(24*time.Hour - time.Second)
		}
	}

	start := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc)
	if startDate != "" {
		if t, err := time.ParseInLocation(layout, startDate, loc); err == nil {
			start = t
		}
	}

	return start.UnixMilli(), end.UnixMilli()
}
