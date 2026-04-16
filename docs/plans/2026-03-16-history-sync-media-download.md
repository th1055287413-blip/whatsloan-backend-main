# History Sync Media Download Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 歷史同步訊息非同步下載媒體，下載完成後通知 API 更新 DB。

**Architecture:** `handleHistorySync` 照常快速 publish 訊息，同時將媒體訊息 enqueue 到 per-account media download worker pool。Worker 下載完成後 publish `MediaDownloaded` 事件，API 端收到後更新 `whatsapp_messages.media_url`。

**Tech Stack:** Go, whatsmeow, Redis Streams (existing event bus)

---

### Task 1: 新增 protocol 定義（Event + Payload）

**Files:**
- Modify: `internal/protocol/events.go:66` (新增 EventType 常量)
- Modify: `internal/protocol/events.go:134` (新增 Payload 結構)

**Step 1: 新增 EventType 常量**

在 `events.go:66` 的 `)` 之前新增：

```go
	// EvtMediaDownloaded 媒體下載完成（歷史同步非同步下載）
	EvtMediaDownloaded EventType = "media_downloaded"
```

**Step 2: 新增 Payload 結構**

在 `MessageReceivedPayload` 結構之後（約 line 134）新增：

```go
// MediaDownloadedPayload 媒體下載完成的 payload
type MediaDownloadedPayload struct {
	MessageID string `json:"message_id"`
	ChatJID   string `json:"chat_jid"`
	MediaURL  string `json:"media_url"`
}
```

**Step 3: 驗證編譯**

Run: `go build -o /dev/null ./cmd/api/... && go build -o /dev/null ./cmd/connector/...`

**Step 4: Commit**

```
git add internal/protocol/events.go
git commit -m "feat: add MediaDownloaded event type and payload"
```

---

### Task 2: 新增 Publisher 方法

**Files:**
- Modify: `internal/connector/whatsmeow/publisher.go:31` (interface 新增方法)
- Modify: `internal/connector/stream_producer.go` (實作方法)

**Step 1: 在 EventPublisher interface 新增方法**

在 `publisher.go` 的 `PublishChatArchiveBatch` 之後、`}` 之前新增：

```go
	PublishMediaDownloaded(ctx context.Context, accountID uint, payload *protocol.MediaDownloadedPayload) error
```

**Step 2: 在 StreamProducer 實作方法**

在 `stream_producer.go` 末尾新增：

```go
// PublishMediaDownloaded 發送媒體下載完成事件
func (p *StreamProducer) PublishMediaDownloaded(ctx context.Context, accountID uint, payload *protocol.MediaDownloadedPayload) error {
	event, err := protocol.NewEvent(protocol.EvtMediaDownloaded, p.connectorID, accountID, payload)
	if err != nil {
		return err
	}
	return p.SendEvent(ctx, event)
}
```

**Step 3: 驗證編譯**

Run: `go build -o /dev/null ./cmd/connector/...`

**Step 4: Commit**

```
git add internal/connector/whatsmeow/publisher.go internal/connector/stream_producer.go
git commit -m "feat: add PublishMediaDownloaded to publisher"
```

---

### Task 3: 實作 media download worker pool

**Files:**
- Create: `internal/connector/whatsmeow/media_worker.go`

**Step 1: 建立 media_worker.go**

```go
package whatsmeow

import (
	"context"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	"go.uber.org/zap"

	"whatsapp_golang/internal/protocol"
)

const mediaWorkerPoolSize = 3

// mediaDownloadTask 媒體下載任務
type mediaDownloadTask struct {
	msg         *events.Message
	contentType string
	messageID   string
	chatJID     string
}

// mediaDownloadPool per-account 媒體下載 worker pool
type mediaDownloadPool struct {
	ch        chan *mediaDownloadTask
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	manager   *Manager
	client    *whatsmeow.Client
	accountID uint
	log       *zap.SugaredLogger
}

// newMediaDownloadPool 建立並啟動 media download pool
func newMediaDownloadPool(ctx context.Context, m *Manager, client *whatsmeow.Client, accountID uint) *mediaDownloadPool {
	poolCtx, cancel := context.WithCancel(ctx)
	p := &mediaDownloadPool{
		ch:        make(chan *mediaDownloadTask, 500),
		ctx:       poolCtx,
		cancel:    cancel,
		manager:   m,
		client:    client,
		accountID: accountID,
		log:       m.log,
	}
	for i := 0; i < mediaWorkerPoolSize; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// enqueue 投遞下載任務（非阻塞，滿了就丟棄）
func (p *mediaDownloadPool) enqueue(task *mediaDownloadTask) {
	select {
	case p.ch <- task:
	default:
		p.log.Warnw("media download queue 已滿，丟棄任務", "account_id", p.accountID, "msg_id", task.messageID)
	}
}

// worker 從 channel 取任務並下載
func (p *mediaDownloadPool) worker() {
	defer p.wg.Done()
	for task := range p.ch {
		if p.ctx.Err() != nil {
			continue // drain
		}
		p.download(task)
	}
}

// download 執行單筆下載
func (p *mediaDownloadPool) download(task *mediaDownloadTask) {
	mediaURL := p.manager.downloadAndUploadMedia(p.ctx, p.client, task.msg, task.contentType)
	if mediaURL == "" {
		return // 下載失敗，downloadAndUploadMedia 已 log warning
	}

	// 通知 API 更新 DB
	if err := p.manager.publisher.PublishMediaDownloaded(p.ctx, p.accountID, &protocol.MediaDownloadedPayload{
		MessageID: task.messageID,
		ChatJID:   task.chatJID,
		MediaURL:  mediaURL,
	}); err != nil {
		p.log.Warnw("發送 MediaDownloaded 事件失敗", "account_id", p.accountID, "msg_id", task.messageID, "error", err)
	}
}

// stop 停止 pool 並等待所有 worker 結束
func (p *mediaDownloadPool) stop() {
	p.cancel()
	close(p.ch)
	p.wg.Wait()
}
```

**Step 2: 驗證編譯**

Run: `go build -o /dev/null ./cmd/connector/...`

**Step 3: Commit**

```
git add internal/connector/whatsmeow/media_worker.go
git commit -m "feat: add media download worker pool"
```

---

### Task 4: handleHistorySync 加入 enqueue 媒體下載

**Files:**
- Modify: `internal/connector/whatsmeow/event.go:396-502` (`handleHistorySync`)

**Step 1: 在 handleHistorySync 中建立 pool 並 enqueue 任務**

在 `handleHistorySync` 函式中，`client` 取得之後（line 416 之後），建立 media pool：

```go
	// 建立媒體下載 worker pool（使用 event worker 的 context，帳號移除時自動中斷）
	mediaPool := newMediaDownloadPool(ctx, m, client, accountID)
	defer mediaPool.stop()
```

在 `batch = append(batch, buildMessagePayload(...))` 之後（line 483 之後），加入 enqueue：

```go
			// 媒體類型加入非同步下載隊列
			switch contentType {
			case "image", "video", "audio", "document", "sticker":
				mediaPool.enqueue(&mediaDownloadTask{
					msg:         msgEvt,
					contentType: contentType,
					messageID:   msgEvt.Info.ID,
					chatJID:     msgEvt.Info.Chat.String(),
				})
			}
```

**Step 2: 驗證編譯**

Run: `go build -o /dev/null ./cmd/connector/...`

**Step 3: Commit**

```
git add internal/connector/whatsmeow/event.go
git commit -m "feat: enqueue media download tasks during history sync"
```

---

### Task 5: API 端處理 MediaDownloaded 事件

**Files:**
- Modify: `internal/gateway/event_consumer.go` (dispatch 新事件)
- Modify: `internal/gateway/whatsapp_event_handler.go` (handler 方法)

**Step 1: 在 event_consumer.go 的 switch 中新增 case**

在 `case protocol.EvtChatArchiveBatch:` 附近（或 switch 末尾的 default 之前）新增：

```go
	case protocol.EvtMediaDownloaded:
		var payload protocol.MediaDownloadedPayload
		if err := event.ParsePayload(&payload); err != nil {
			return err
		}
		return c.handler.OnMediaDownloaded(ctx, event, &payload)
```

**Step 2: 在 whatsapp_event_handler.go 新增 handler**

```go
// OnMediaDownloaded 處理媒體下載完成事件（更新歷史訊息的 media_url）
func (h *WhatsAppEventHandler) OnMediaDownloaded(ctx context.Context, event *protocol.Event, payload *protocol.MediaDownloadedPayload) error {
	result := h.db.WithContext(ctx).
		Model(&model.WhatsAppMessage{}).
		Where("message_id = ? AND account_id = ? AND (media_url IS NULL OR media_url = '')", payload.MessageID, event.AccountID).
		Update("media_url", payload.MediaURL)

	if result.Error != nil {
		logger.Ctx(ctx).Warnw("更新媒體 URL 失敗", "message_id", payload.MessageID, "error", result.Error)
		return result.Error
	}

	if result.RowsAffected > 0 {
		logger.Ctx(ctx).Debugw("媒體 URL 已更新", "message_id", payload.MessageID, "media_url", payload.MediaURL)
	}

	return nil
}
```

**Step 3: 確認 WhatsAppMessage model 有 MediaURL 欄位**

檢查 `internal/model/whatsapp.go` 中 `WhatsAppMessage` 結構確認有 `MediaURL` 欄位（對應 `media_url` 欄位）。

**Step 4: 驗證編譯**

Run: `go build -o /dev/null ./cmd/api/... && go build -o /dev/null ./cmd/connector/...`

**Step 5: Commit**

```
git add internal/gateway/event_consumer.go internal/gateway/whatsapp_event_handler.go
git commit -m "feat: handle MediaDownloaded event to update media_url in DB"
```

---

### Task 6: 端到端驗證 + bump

**Step 1: 完整編譯檢查**

Run: `go build -o /dev/null ./cmd/api/... && go build -o /dev/null ./cmd/connector/...`

**Step 2: 檢查 go vet**

Run: `go vet ./internal/...`

**Step 3: 執行 /bump**

先 `git pull --rebase origin main`，再執行 bump。
