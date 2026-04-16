package whatsmeow

import (
	"context"
	"sync"

	"github.com/go-redis/redis/v8"
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
	dedupeKey   string
}

// mediaDownloadPool per-account 媒體下載 worker pool
type mediaDownloadPool struct {
	ch        chan *mediaDownloadTask
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	manager   *Manager
	client    *whatsmeow.Client
	redis     *redis.Client
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
		redis:     m.redis,
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
		// 下載失敗，清除 dedup key 讓下次同步能重試
		if task.dedupeKey != "" {
			p.redis.Del(context.Background(), task.dedupeKey)
		}
		return
	}

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
	close(p.ch)
	p.wg.Wait()
	p.cancel()
}
