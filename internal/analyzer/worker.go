package analyzer

import (
	"context"
	"sync"
	"time"

	"whatsapp_golang/internal/logger"
)

// OnCompleteFunc 分析完成回調函式
type OnCompleteFunc func(task *AnalysisTask, results []Result)

// WorkerPool 分析 Worker Pool
type WorkerPool struct {
	queue       *AnalysisQueue
	analyzers   []Analyzer     // AI 分析器列表
	onComplete  OnCompleteFunc // 完成回調

	workerCount int
	stopCh      chan struct{}
	wg          sync.WaitGroup
	running     bool
	mu          sync.Mutex
}

// WorkerPoolConfig Worker Pool 配置
type WorkerPoolConfig struct {
	WorkerCount int
	Queue       *AnalysisQueue
	Analyzers   []Analyzer
	OnComplete  OnCompleteFunc
}

// NewWorkerPool 建立 Worker Pool
func NewWorkerPool(cfg *WorkerPoolConfig) *WorkerPool {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 5
	}

	return &WorkerPool{
		queue:       cfg.Queue,
		analyzers:   cfg.Analyzers,
		onComplete:  cfg.OnComplete,
		workerCount: cfg.WorkerCount,
		stopCh:      make(chan struct{}),
	}
}

// Start 啟動 Worker Pool
func (p *WorkerPool) Start() {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.stopCh = make(chan struct{})
	p.mu.Unlock()

	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	logger.Infow("AnalysisWorkerPool 已啟動", "worker_count", p.workerCount)
}

// Stop 停止 Worker Pool
func (p *WorkerPool) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	close(p.stopCh)
	p.mu.Unlock()

	p.wg.Wait()
	logger.Info("AnalysisWorkerPool 已停止")
}

// IsRunning 檢查是否運行中
func (p *WorkerPool) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// worker 工作協程
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.stopCh:
			logger.Debugw("Worker 收到停止信號", "worker_id", id)
			return
		default:
			ctx := context.Background()
			task, err := p.queue.Dequeue(ctx, 5*time.Second)
			if err != nil {
				logger.Warnw("Worker dequeue 錯誤", "worker_id", id, "error", err)
				continue
			}
			if task == nil {
				continue // 超時，繼續等待
			}

			p.processTask(ctx, id, task)
		}
	}
}

// processTask 處理分析任務
func (p *WorkerPool) processTask(ctx context.Context, workerID int, task *AnalysisTask) {
	logger.Debugw("Worker 開始處理任務", "worker_id", workerID, "task_id", task.ID)

	var allResults []Result

	for _, analyzer := range p.analyzers {
		results, err := analyzer.Analyze(ctx, task.Content)
		if err != nil {
			logger.Warnw("分析失敗", "analyzer", analyzer.Name(), "error", err)
			continue
		}
		allResults = append(allResults, results...)
	}

	if p.onComplete != nil {
		p.onComplete(task, allResults)
	}

	logger.Debugw("Worker 完成任務", "worker_id", workerID, "task_id", task.ID, "result_count", len(allResults))
}

// SetOnComplete 設定完成回調
func (p *WorkerPool) SetOnComplete(fn OnCompleteFunc) {
	p.onComplete = fn
}

// AddAnalyzer 新增分析器
func (p *WorkerPool) AddAnalyzer(a Analyzer) {
	p.analyzers = append(p.analyzers, a)
}
