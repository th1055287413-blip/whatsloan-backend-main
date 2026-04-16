package realtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"

	"github.com/gin-gonic/gin"
)

// SSEEvent SSE 事件結構
type SSEEvent struct {
	Type      string      `json:"type"`       // 事件類型
	AccountID uint        `json:"account_id"` // 帳號ID
	Data      interface{} `json:"data"`       // 事件數據
	Timestamp time.Time   `json:"timestamp"`  // 時間戳
}

// SSEClient SSE 客戶端連接
type SSEClient struct {
	AccountID  uint            // 主帳號（單帳號模式，用於日誌）
	AccountIDs map[uint]bool   // 訂閱的帳號集合（多帳號模式）
	Events     chan *SSEEvent
}

// SSEHandler SSE 處理器
type SSEHandler struct {
	clients    map[*SSEClient]bool
	register   chan *SSEClient
	unregister chan *SSEClient
	broadcast  chan *SSEEvent
	mu         sync.RWMutex
}

var (
	// SSE 全局 SSE 處理器實例
	SSE *SSEHandler
)

// NewSSEHandler 創建 SSE 處理器
func NewSSEHandler() *SSEHandler {
	handler := &SSEHandler{
		clients:    make(map[*SSEClient]bool),
		register:   make(chan *SSEClient),
		unregister: make(chan *SSEClient),
		broadcast:  make(chan *SSEEvent, 256),
	}

	go handler.run()

	return handler
}

// InitSSEHandler 初始化全局 SSE 處理器
func InitSSEHandler() {
	SSE = NewSSEHandler()
	logger.Infow("SSE 處理器已初始化")
}

// run 運行 SSE 事件處理循環
func (h *SSEHandler) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			logger.WithAccount(client.AccountID).Infow("SSE 客戶端已連接", "client_count", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Events)
				logger.WithAccount(client.AccountID).Infow("SSE 客戶端已斷開", "client_count", len(h.clients))
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			h.mu.RLock()
			delivered := 0
			for client := range h.clients {
				// 檢查客戶端是否訂閱該帳號的事件
				if client.AccountIDs[event.AccountID] {
					select {
					case client.Events <- event:
						delivered++
					default:
						// 客戶端緩衝區滿，跳過
						logger.WithAccount(client.AccountID).Warnw("SSE 客戶端緩衝區滿，跳過事件", "event_type", event.Type)
					}
				}
			}
			if delivered == 0 {
				logger.WithAccount(event.AccountID).Debugw("SSE 廣播無匹配客戶端", "event_type", event.Type)
			} else {
				logger.WithAccount(event.AccountID).Debugw("SSE 事件已推送", "event_type", event.Type, "client_count", delivered)
			}
			h.mu.RUnlock()
		}
	}
}

// HandleSSE SSE 連接處理
func (h *SSEHandler) HandleSSE(c *gin.Context) {
	// 從查詢參數獲取 accountID
	accountIDStr := c.Query("account_id")
	if accountIDStr == "" {
		common.Error(c, common.CodeInvalidParams, "缺少 account_id 參數")
		return
	}

	var accountID uint
	if _, err := fmt.Sscanf(accountIDStr, "%d", &accountID); err != nil {
		common.Error(c, common.CodeInvalidParams, "無效的 account_id")
		return
	}

	// 設定 SSE 響應頭
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // 禁用 nginx 緩衝

	// 創建客戶端
	client := &SSEClient{
		AccountID:  accountID,
		AccountIDs: map[uint]bool{accountID: true},
		Events:     make(chan *SSEEvent, 64),
	}

	// 註冊客戶端
	h.register <- client

	// 確保連接關閉時註銷客戶端
	defer func() {
		h.unregister <- client
	}()

	// 發送連接成功事件
	connectEvent := &SSEEvent{
		Type:      "connected",
		AccountID: accountID,
		Data: map[string]interface{}{
			"message": "SSE 連接成功",
		},
		Timestamp: time.Now(),
	}
	h.sendEvent(c.Writer, connectEvent)
	c.Writer.(http.Flusher).Flush()

	// 心跳計時器
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	// 監聽事件
	clientGone := c.Request.Context().Done()
	for {
		select {
		case <-clientGone:
			logger.WithAccount(accountID).Infow("SSE 客戶端斷開連接")
			return

		case event := <-client.Events:
			h.sendEvent(c.Writer, event)
			c.Writer.(http.Flusher).Flush()

		case <-heartbeat.C:
			// 發送心跳保持連接
			h.sendEvent(c.Writer, &SSEEvent{
				Type:      "heartbeat",
				AccountID: accountID,
				Data:      nil,
				Timestamp: time.Now(),
			})
			c.Writer.(http.Flusher).Flush()
		}
	}
}

// sendEvent 發送 SSE 事件
func (h *SSEHandler) sendEvent(w http.ResponseWriter, event *SSEEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		logger.Errorw("SSE 事件序列化失敗", "event_type", event.Type, "error", err)
		return
	}

	fmt.Fprintf(w, "event: %s\n", event.Type)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// Broadcast 廣播事件到指定帳號
func (h *SSEHandler) Broadcast(accountID uint, eventType string, data interface{}) {
	event := &SSEEvent{
		Type:      eventType,
		AccountID: accountID,
		Data:      data,
		Timestamp: time.Now(),
	}

	select {
	case h.broadcast <- event:
		logger.WithAccount(accountID).Debugw("SSE 廣播事件", "event_type", eventType)
	default:
		logger.WithAccount(accountID).Warnw("SSE 廣播通道已滿，丟棄事件", "event_type", eventType)
	}
}

// HandleAgentSSE Agent 專用 SSE 連接，必帶 account_ids（逗號分隔）
func (h *SSEHandler) HandleAgentSSE(c *gin.Context) {
	_, exists := c.Get("agent_id")
	if !exists {
		common.Error(c, common.CodeAuthFailed, "未登入")
		return
	}

	raw := c.Query("account_ids")
	if raw == "" {
		common.Error(c, common.CodeInvalidParams, "缺少 account_ids 參數")
		return
	}

	accountIDs := make(map[uint]bool)
	var primaryAccountID uint
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			common.Error(c, common.CodeInvalidParams, "無效的 account_id: "+s)
			return
		}
		accountIDs[uint(id)] = true
		if primaryAccountID == 0 {
			primaryAccountID = uint(id)
		}
	}

	if len(accountIDs) == 0 {
		common.Error(c, common.CodeInvalidParams, "缺少 account_ids 參數")
		return
	}

	// 設定 SSE 響應頭
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	client := &SSEClient{
		AccountID:  primaryAccountID,
		AccountIDs: accountIDs,
		Events:     make(chan *SSEEvent, 64),
	}

	h.register <- client
	defer func() {
		h.unregister <- client
	}()

	idList := make([]uint, 0, len(accountIDs))
	for id := range accountIDs {
		idList = append(idList, id)
	}
	connectEvent := &SSEEvent{
		Type:      "connected",
		AccountID: primaryAccountID,
		Data: map[string]interface{}{
			"message":     "SSE 連接成功",
			"account_ids": idList,
		},
		Timestamp: time.Now(),
	}
	h.sendEvent(c.Writer, connectEvent)
	c.Writer.(http.Flusher).Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	clientGone := c.Request.Context().Done()
	for {
		select {
		case <-clientGone:
			logger.Infow("SSE Agent 客戶端斷開連接", "account_ids", raw)
			return

		case event := <-client.Events:
			h.sendEvent(c.Writer, event)
			c.Writer.(http.Flusher).Flush()

		case <-heartbeat.C:
			h.sendEvent(c.Writer, &SSEEvent{
				Type:      "heartbeat",
				AccountID: primaryAccountID,
				Data:      nil,
				Timestamp: time.Now(),
			})
			c.Writer.(http.Flusher).Flush()
		}
	}
}

// BroadcastGlobal 全局廣播函數，供其他包調用
func BroadcastSSE(accountID uint, eventType string, data interface{}) {
	if SSE != nil {
		SSE.Broadcast(accountID, eventType, data)
	}
}
