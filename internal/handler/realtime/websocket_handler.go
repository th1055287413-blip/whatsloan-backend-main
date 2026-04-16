package realtime

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/logger"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// WebSocketMessage WebSocket 消息結構
type WebSocketMessage struct {
	Type      string      `json:"type"`       // 消息類型：new_message, message_status, etc.
	AccountID uint        `json:"account_id"` // 帳號ID
	Data      interface{} `json:"data"`       // 消息數據
	Timestamp time.Time   `json:"timestamp"`  // 時間戳
}

// WebSocketHandler WebSocket 處理器
type WebSocketHandler struct {
	clients    map[*websocket.Conn]uint // 客戶端連接映射到 accountID
	broadcast  chan *WebSocketMessage   // 廣播通道
	register   chan *ClientConnection   // 註冊通道
	unregister chan *websocket.Conn     // 註銷通道
	mu         sync.RWMutex             // 讀寫鎖
}

// ClientConnection 客戶端連接資訊
type ClientConnection struct {
	Conn      *websocket.Conn
	AccountID uint
}

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // 允許所有來源，生產環境需要更嚴格的檢查
		},
	}

	// WSHandler 全局 WebSocket 處理器實例
	WSHandler *WebSocketHandler
)

// NewWebSocketHandler 創建 WebSocket 處理器
func NewWebSocketHandler() *WebSocketHandler {
	handler := &WebSocketHandler{
		clients:    make(map[*websocket.Conn]uint),
		broadcast:  make(chan *WebSocketMessage, 256),
		register:   make(chan *ClientConnection),
		unregister: make(chan *websocket.Conn),
	}

	// 啟動消息處理協程
	go handler.run()

	return handler
}

// InitWebSocketHandler 初始化全局 WebSocket 處理器
func InitWebSocketHandler() {
	WSHandler = NewWebSocketHandler()
	logger.Infow("WebSocket 處理器已初始化")
}

// run 運行 WebSocket 消息處理循環
func (h *WebSocketHandler) run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn.Conn] = conn.AccountID
			h.mu.Unlock()
			logger.WithAccount(conn.AccountID).Infow("WebSocket 客戶端已連接", "client_count", len(h.clients))

		case conn := <-h.unregister:
			h.mu.Lock()
			if accountID, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
				logger.WithAccount(accountID).Infow("WebSocket 客戶端已斷開", "client_count", len(h.clients))
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			for client, accountID := range h.clients {
				// 只推送給對應帳號的客戶端
				if accountID == message.AccountID {
					err := client.WriteJSON(message)
					if err != nil {
						logger.WithAccount(accountID).Errorw("WebSocket 發送訊息失敗", "error", err)
						h.unregister <- client
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// HandleWebSocket WebSocket 連接處理
func (h *WebSocketHandler) HandleWebSocket(c *gin.Context) {
	// WebSocket 連接已通過 authRequired 中間件驗證
	// 可以從上下文中獲取用戶資訊（如果需要）

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

	// 升級 HTTP 連接為 WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Ctx(c.Request.Context()).Errorw("WebSocket 升級失敗", "error", err)
		return
	}

	// 註冊客戶端
	h.register <- &ClientConnection{
		Conn:      conn,
		AccountID: accountID,
	}

	// 發送歡迎消息
	welcomeMsg := &WebSocketMessage{
		Type:      "connected",
		AccountID: accountID,
		Data: map[string]interface{}{
			"message": "WebSocket 連接成功",
		},
		Timestamp: time.Now(),
	}
	conn.WriteJSON(welcomeMsg)

	// 讀取客戶端消息（主要用於心跳檢測）
	go func() {
		defer func() {
			h.unregister <- conn
		}()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					logger.WithAccount(accountID).Errorw("WebSocket 讀取錯誤", "error", err)
				}
				break
			}
		}
	}()
}

// BroadcastNewMessage 廣播新消息
func (h *WebSocketHandler) BroadcastNewMessage(accountID uint, message interface{}) {
	msg := &WebSocketMessage{
		Type:      "new_message",
		AccountID: accountID,
		Data:      message,
		Timestamp: time.Now(),
	}

	// 非阻塞發送
	select {
	case h.broadcast <- msg:
		logger.WithAccount(accountID).Debugw("廣播新訊息")
	default:
		logger.WithAccount(accountID).Warnw("廣播通道已滿，丟棄訊息")
	}
}

// BroadcastMessageStatus 廣播消息狀態更新
func (h *WebSocketHandler) BroadcastMessageStatus(accountID uint, messageID string, status string) {
	msg := &WebSocketMessage{
		Type:      "message_status",
		AccountID: accountID,
		Data: map[string]interface{}{
			"message_id": messageID,
			"status":     status,
		},
		Timestamp: time.Now(),
	}

	select {
	case h.broadcast <- msg:
	default:
		logger.WithAccount(accountID).Warnw("廣播通道已滿，丟棄狀態更新", "message_id", messageID)
	}
}

// BroadcastMessage 全局廣播函數，供其他包調用
func BroadcastMessage(accountID uint, message interface{}) {
	if WSHandler != nil {
		WSHandler.BroadcastNewMessage(accountID, message)
	}
}

// BroadcastBatchSendProgress 廣播批量發送進度
func (h *WebSocketHandler) BroadcastBatchSendProgress(accountID uint, data interface{}) {
	msg := &WebSocketMessage{
		Type:      "batch_send_progress",
		AccountID: accountID,
		Data:      data,
		Timestamp: time.Now(),
	}

	// 非阻塞發送
	select {
	case h.broadcast <- msg:
	default:
		logger.WithAccount(accountID).Warnw("廣播通道已滿，丟棄批量發送進度訊息")
	}
}

// BroadcastEvent 通用事件廣播（用於 message_revoked, message_edited 等事件）
func (h *WebSocketHandler) BroadcastEvent(accountID uint, eventType string, data interface{}) {
	msg := &WebSocketMessage{
		Type:      eventType,
		AccountID: accountID,
		Data:      data,
		Timestamp: time.Now(),
	}

	select {
	case h.broadcast <- msg:
		logger.WithAccount(accountID).Debugw("廣播事件", "event_type", eventType)
	default:
		logger.WithAccount(accountID).Warnw("廣播通道已滿，丟棄事件", "event_type", eventType)
	}
}

// BroadcastEventGlobal 全局事件廣播函數，供其他包調用
func BroadcastEventGlobal(accountID uint, eventType string, data interface{}) {
	if WSHandler != nil {
		WSHandler.BroadcastEvent(accountID, eventType, data)
	}
}
