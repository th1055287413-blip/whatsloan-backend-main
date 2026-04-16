package whatsmeow

import (
	"context"
	"sync/atomic"

	"go.uber.org/zap"
)

const eventWorkerBufferSize = 1000

// eventWorker 異步事件處理 worker，每個帳號一個，避免阻塞 whatsmeow node handler
type eventWorker struct {
	ch        chan interface{}
	done      chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	stopped   atomic.Bool
	accountID uint
	log       *zap.SugaredLogger
	handler   func(context.Context, interface{})
}

// newEventWorker 建立並啟動 event worker
func newEventWorker(accountID uint, log *zap.SugaredLogger, handler func(context.Context, interface{})) *eventWorker {
	ctx, cancel := context.WithCancel(context.Background())
	w := &eventWorker{
		ch:        make(chan interface{}, eventWorkerBufferSize),
		done:      make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
		accountID: accountID,
		log:       log,
		handler:   handler,
	}
	go w.run()
	return w
}

// send 非阻塞投遞事件到 worker channel
func (w *eventWorker) send(evt interface{}) {
	if w.stopped.Load() {
		return
	}
	select {
	case w.ch <- evt:
		if qLen := len(w.ch); qLen > 0 && qLen%100 == 0 {
			w.log.Warnw("event worker queue 堆積", "account_id", w.accountID, "queue_len", qLen, "capacity", eventWorkerBufferSize)
		}
	default:
		w.log.Warnw("event worker 背壓，同步處理", "account_id", w.accountID, "queue_len", len(w.ch))
		w.handler(w.ctx, evt)
	}
}

// run worker goroutine，順序處理 channel 中的事件
func (w *eventWorker) run() {
	defer close(w.done)
	for evt := range w.ch {
		if w.ctx.Err() != nil {
			continue // context 已取消，drain 但不處理
		}
		w.handler(w.ctx, evt)
	}
}

// stop 停止 worker 並等待 drain 完成
func (w *eventWorker) stop() {
	w.stopped.Store(true) // 擋住新事件
	w.cancel()            // 取消 context，正在跑的 handler 提前返回
	close(w.ch)           // 觸發 for-range 結束
	<-w.done              // 等 goroutine 退出
}
