package main

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"whatsapp_golang/internal/llm"
	"whatsapp_golang/internal/model"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	hours        = flag.Int("hours", 168, "掃描最近 N 小時")
	output       = flag.String("output", "scan_results.csv", "CSV 輸出路徑")
	workers      = flag.Int("workers", 3, "圖片分析並行數")
	visionModel  = flag.String("vision-model", "google/gemini-2.5-flash", "Vision model")
	textOnly     = flag.Bool("text-only", false, "跳過圖片分析")
	mediaDir     = flag.String("media-dir", "", "媒體目錄 (預設 $WHATSAPP_MEDIA_DIR 或 uploads)")
	mediaBaseURL = flag.String("media-base-url", "", "遠端媒體 base URL (設定後透過 HTTP 抓圖片，例如 https://server.example.com)")
	verbose      = flag.Bool("verbose", false, "詳細輸出")
)

// --- prompts ---

const textAnalysisPrompt = `你是金融交易偵測助手。分析以下 WhatsApp 聊天記錄，找出涉及金錢交易的訊息。

偵測範圍：轉帳、匯款、借錢、還錢、貸款、收款、付款、利息、欠款、薪資、費用支付等。

聊天記錄：
%s

回傳規則：
- 僅回傳涉及金錢交易的訊息，無則回傳 []
- amount 欄位：提取具體金額數字（如 "5000"、"1200.50"），無法判斷則為空字串
- summary：簡述交易類型與對象，不超過 30 字

格式：[{"message_id": "...", "summary": "...", "amount": "..."}]
只回傳 JSON。`

const imageAnalysisPrompt = `你是金融交易偵測助手。判斷此圖片是否為財務交易憑證（轉帳截圖、付款確認、銀行紀錄、匯款證明、發票帳單等）。

回傳規則：
- is_financial：是否為財務相關圖片
- summary：簡述交易內容，不超過 30 字
- amount：提取具體金額數字（如 "5000"），無法判斷則為空字串

只回傳 JSON：{"is_financial": true/false, "summary": "...", "amount": "..."}`

// --- main ---

func main() {
	godotenv.Load()
	flag.Parse()

	if *mediaDir == "" {
		*mediaDir = os.Getenv("WHATSAPP_MEDIA_DIR")
		if *mediaDir == "" {
			*mediaDir = "uploads"
		}
	}

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_USER", "postgres"),
		os.Getenv("DB_PASSWORD"),
		getEnv("DB_NAME", "whatsapp"),
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "資料庫連線失敗: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("資料庫連線成功")

	var apiKey string
	db.Raw("SELECT config_value FROM system_configs WHERE config_key = 'llm.api_key' LIMIT 1").Scan(&apiKey)
	if apiKey == "" {
		apiKey = os.Getenv("LLM_API_KEY")
	}
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "未找到 llm.api_key（system_configs 或 LLM_API_KEY 環境變數）")
		os.Exit(1)
	}

	llmClient := llm.NewClient(func() string { return apiKey }, 60)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n收到中斷信號，正在停止...")
		cancel()
	}()

	opts := scanOptions{
		Hours:        *hours,
		Workers:      *workers,
		VisionModel:  *visionModel,
		TextOnly:     *textOnly,
		MediaDir:     *mediaDir,
		MediaBaseURL: *mediaBaseURL,
		Verbose:      *verbose,
	}

	fmt.Printf("=== Financial Transaction Scanner ===\n")
	fmt.Printf("掃描範圍: 最近 %d 小時\n", opts.Hours)
	fmt.Printf("圖片分析: %v (workers: %d, model: %s)\n", !opts.TextOnly, opts.Workers, opts.VisionModel)
	if opts.MediaBaseURL != "" {
		fmt.Printf("媒體來源: %s (遠端)\n", opts.MediaBaseURL)
	} else {
		fmt.Printf("媒體來源: %s (本地)\n", opts.MediaDir)
	}
	absOutput, _ := filepath.Abs(*output)
	fmt.Printf("輸出檔案: %s\n", absOutput)
	fmt.Println()

	cw, err := newCSVWriter(absOutput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "建立 CSV 失敗: %v\n", err)
		os.Exit(1)
	}
	defer cw.close()

	scanner := &scanner{db: db, llmClient: llmClient, opts: opts, cw: cw}
	count, err := scanner.run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "掃描失敗: %v\n", err)
		os.Exit(1)
	}

	if count == 0 {
		fmt.Println("未偵測到金融交易相關內容")
		return
	}

	fmt.Printf("完成，共 %d 筆結果已寫入 %s\n", count, absOutput)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// --- scanner ---

type scanResult struct {
	AccountPhone   string
	AccountID      uint
	ChatJID        string
	ChatName       string
	DetectionType  string // "text" or "image"
	MatchedContent string
	Amount         string
	MessageID      string
	Timestamp      time.Time
	IsFromMe       bool
}

type scanOptions struct {
	Hours        int
	Workers      int
	VisionModel  string
	TextOnly     bool
	MediaDir     string
	MediaBaseURL string
	Verbose      bool
}

type scanner struct {
	db        *gorm.DB
	llmClient *llm.Client
	opts      scanOptions
	cw        *csvStreamWriter
}

type chatJob struct {
	account  model.WhatsAppAccount
	chat     model.WhatsAppChat // 代表聊天室（合併後取有名稱的那筆）
	textMsgs []model.WhatsAppMessage
	imgMsgs  []model.WhatsAppMessage
}

// lidMapping 從 whatsmeow_lid_map 建立 JID 正規化映射
type lidMapping struct {
	lidToPhone map[string]string // lid user → phone number
	phoneToLID map[string]string // phone number → lid user
}

func loadLIDMapping(db *gorm.DB) lidMapping {
	type row struct {
		LID string `gorm:"column:lid"`
		PN  string `gorm:"column:pn"`
	}
	var rows []row
	db.Table("whatsmeow_lid_map").Find(&rows)

	m := lidMapping{
		lidToPhone: make(map[string]string, len(rows)),
		phoneToLID: make(map[string]string, len(rows)),
	}
	for _, r := range rows {
		m.lidToPhone[r.LID] = r.PN
		m.phoneToLID[r.PN] = r.LID
	}
	return m
}

// canonicalJID 將 LID 或 PhoneJID 正規化為同一個 key（優先用電話號碼）
func (m *lidMapping) canonicalJID(jid string) string {
	if strings.HasSuffix(jid, "@lid") {
		lid := strings.TrimSuffix(jid, "@lid")
		if pn, ok := m.lidToPhone[lid]; ok {
			return pn + "@s.whatsapp.net"
		}
	}
	return jid
}

func (s *scanner) run(ctx context.Context) (int, error) {
	var accounts []model.WhatsAppAccount
	if err := s.db.Where("status = 'connected'").Find(&accounts).Error; err != nil {
		return 0, fmt.Errorf("query accounts: %w", err)
	}

	if s.opts.Verbose {
		fmt.Printf("找到 %d 個帳號\n", len(accounts))
	}

	lm := loadLIDMapping(s.db)
	since := time.Now().Add(-time.Duration(s.opts.Hours) * time.Hour)
	var jobs []chatJob

	for _, acc := range accounts {
		var chats []model.WhatsAppChat
		if err := s.db.Where("account_id = ? AND is_group = false", acc.ID).Find(&chats).Error; err != nil {
			fmt.Printf("查詢帳號 %d 聊天室失敗: %v\n", acc.ID, err)
			continue
		}

		// 用 canonical JID 合併同一聯絡人的多筆 chat
		merged := make(map[string]*chatJob)

		for _, chat := range chats {
			var messages []model.WhatsAppMessage
			if err := s.db.Where(
				"chat_id = ? AND is_revoked = false AND timestamp >= ? AND type IN ?",
				chat.ID, since, []string{"text", "image"},
			).Order("timestamp DESC").Limit(100).Find(&messages).Error; err != nil {
				continue
			}
			if len(messages) == 0 {
				continue
			}

			key := lm.canonicalJID(chat.JID)
			job, exists := merged[key]
			if !exists {
				job = &chatJob{account: acc, chat: chat}
				merged[key] = job
			} else if job.chat.Name == "" && chat.Name != "" {
				job.chat = chat // 優先用有名稱的 chat
			}

			for _, msg := range messages {
				switch msg.Type {
				case "text":
					job.textMsgs = append(job.textMsgs, msg)
				case "image":
					if !s.opts.TextOnly && msg.MediaURL != "" {
						job.imgMsgs = append(job.imgMsgs, msg)
					}
				}
			}
		}

		for _, job := range merged {
			if len(job.textMsgs) > 0 || len(job.imgMsgs) > 0 {
				jobs = append(jobs, *job)
			}
		}
	}

	if s.opts.Verbose {
		fmt.Printf("待分析聊天室: %d 個\n", len(jobs))
	}

	return s.processChats(ctx, jobs), nil
}

// --- worker pool ---

func (s *scanner) processChats(ctx context.Context, jobs []chatJob) int {
	total := len(jobs)
	ch := make(chan chatJob, total)
	var count atomic.Int64
	var done atomic.Int64
	var wg sync.WaitGroup

	w := s.opts.Workers
	if w <= 0 {
		w = 3
	}

	for i := 0; i < w; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range ch {
				if ctx.Err() != nil {
					return
				}
				r := s.analyzeChat(ctx, job)
				n := done.Add(1)
				fmt.Printf("\r分析中 [%d/%d]...", n, total)
				if len(r) > 0 {
					s.cw.writeResults(r)
					count.Add(int64(len(r)))
				}
			}
		}()
	}

	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	if total > 0 {
		fmt.Println()
	}

	return int(count.Load())
}

func (s *scanner) analyzeChat(ctx context.Context, job chatJob) []scanResult {
	// 文字分析先跑，有命中就直接回傳
	if len(job.textMsgs) > 0 {
		if results := s.analyzeTextBatch(ctx, job); len(results) > 0 {
			return results
		}
	}

	// 圖片逐張分析，命中一張就回傳
	for _, msg := range job.imgMsgs {
		if ctx.Err() != nil {
			break
		}
		if r, ok := s.analyzeImage(ctx, job.account, job.chat, msg); ok {
			return []scanResult{r}
		}
	}

	return nil
}

// --- text batch LLM analysis ---

type textDetection struct {
	MessageID string `json:"message_id"`
	Summary   string `json:"summary"`
	Amount    string `json:"amount"`
}

func (s *scanner) analyzeTextBatch(ctx context.Context, job chatJob) []scanResult {
	var sb strings.Builder
	for _, msg := range job.textMsgs {
		direction := "對方"
		if msg.IsFromMe {
			direction = "我方"
		}
		fmt.Fprintf(&sb, "[%s] (%s) %s: %s\n",
			msg.Timestamp.Format("2006-01-02 15:04:05"),
			msg.MessageID,
			direction,
			msg.Content,
		)
	}

	prompt := fmt.Sprintf(textAnalysisPrompt, sb.String())

	messages := []llm.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := s.llmClient.ChatCompletion(ctx, messages)
	if err != nil {
		if s.opts.Verbose {
			fmt.Printf("文字分析失敗: chat=%s err=%v\n", job.chat.JID, err)
		}
		return nil
	}

	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var detections []textDetection
	if err := json.Unmarshal([]byte(resp), &detections); err != nil {
		if s.opts.Verbose {
			fmt.Printf("解析文字分析回應失敗: chat=%s err=%v raw=%s\n", job.chat.JID, err, resp)
		}
		return nil
	}

	msgMap := make(map[string]model.WhatsAppMessage, len(job.textMsgs))
	for _, m := range job.textMsgs {
		msgMap[m.MessageID] = m
	}

	var results []scanResult
	for _, d := range detections {
		msg, ok := msgMap[d.MessageID]
		if !ok {
			continue
		}
		results = append(results, scanResult{
			AccountPhone:   job.account.PhoneNumber,
			AccountID:      job.account.ID,
			ChatJID:        job.chat.JID,
			ChatName:       job.chat.Name,
			DetectionType:  "text",
			MatchedContent: d.Summary,
			Amount:         d.Amount,
			MessageID:      d.MessageID,
			Timestamp:      msg.Timestamp,
			IsFromMe:       msg.IsFromMe,
		})
	}

	if s.opts.Verbose && len(results) > 0 {
		fmt.Printf("聊天室 %s: 偵測到 %d 筆文字交易\n", job.chat.JID, len(results))
	}

	return results
}

// --- image vision analysis ---

type visionResponse struct {
	IsFinancial bool   `json:"is_financial"`
	Summary     string `json:"summary"`
	Amount      string `json:"amount"`
}

const maxImageSize = 10 * 1024 * 1024 // 10 MB

func (s *scanner) loadImage(ctx context.Context, mediaURL string) (llm.ContentPart, bool) {
	if s.opts.MediaBaseURL != "" {
		return s.loadImageHTTP(ctx, mediaURL)
	}
	return s.loadImageLocal(mediaURL)
}

func (s *scanner) loadImageLocal(mediaURL string) (llm.ContentPart, bool) {
	localPath := filepath.Join(s.opts.MediaDir, strings.TrimPrefix(mediaURL, "/media/"))

	info, err := os.Stat(localPath)
	if err != nil || info.Size() > maxImageSize {
		if s.opts.Verbose && err != nil {
			fmt.Printf("跳過圖片 (不存在): %s\n", localPath)
		}
		return llm.ContentPart{}, false
	}

	part, err := llm.LoadImageAsBase64(localPath)
	if err != nil {
		if s.opts.Verbose {
			fmt.Printf("讀取圖片失敗: %s: %v\n", localPath, err)
		}
		return llm.ContentPart{}, false
	}
	return part, true
}

func (s *scanner) loadImageHTTP(ctx context.Context, mediaURL string) (llm.ContentPart, bool) {
	url := strings.TrimRight(s.opts.MediaBaseURL, "/") + mediaURL

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return llm.ContentPart{}, false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if s.opts.Verbose {
			fmt.Printf("下載圖片失敗: %s: %v\n", url, err)
		}
		return llm.ContentPart{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if s.opts.Verbose {
			fmt.Printf("下載圖片失敗: %s: HTTP %d\n", url, resp.StatusCode)
		}
		return llm.ContentPart{}, false
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize+1))
	if err != nil || int64(len(data)) > maxImageSize {
		return llm.ContentPart{}, false
	}

	ext := filepath.Ext(mediaURL)
	mime := mimeFromExt(ext)
	return llm.ImageBase64Part(mime, base64.StdEncoding.EncodeToString(data)), true
}

func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func (s *scanner) analyzeImage(ctx context.Context, acc model.WhatsAppAccount, chat model.WhatsAppChat, msg model.WhatsAppMessage) (scanResult, bool) {
	imgPart, ok := s.loadImage(ctx, msg.MediaURL)
	if !ok {
		return scanResult{}, false
	}

	messages := []interface{}{
		llm.MultimodalMessage{
			Role: "user",
			Content: []llm.ContentPart{
				imgPart,
				llm.TextPart(imageAnalysisPrompt),
			},
		},
	}

	resp, err := s.llmClient.ChatCompletionMultimodal(ctx, s.opts.VisionModel, messages)
	if err != nil {
		if s.opts.Verbose {
			fmt.Printf("Vision API 失敗: msg=%s err=%v\n", msg.MessageID, err)
		}
		return scanResult{}, false
	}

	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var vr visionResponse
	if err := json.Unmarshal([]byte(resp), &vr); err != nil {
		if s.opts.Verbose {
			fmt.Printf("解析 Vision 回應失敗: %v, raw=%s\n", err, resp)
		}
		return scanResult{}, false
	}

	if !vr.IsFinancial {
		return scanResult{}, false
	}

	if s.opts.Verbose {
		fmt.Printf("偵測到金融圖片: msg=%s summary=%s\n", msg.MessageID, vr.Summary)
	}

	return scanResult{
		AccountPhone:   acc.PhoneNumber,
		AccountID:      acc.ID,
		ChatJID:        chat.JID,
		ChatName:       chat.Name,
		DetectionType:  "image",
		MatchedContent: vr.Summary,
		Amount:         vr.Amount,
		MessageID:      msg.MessageID,
		Timestamp:      msg.Timestamp,
		IsFromMe:       msg.IsFromMe,
	}, true
}

// --- CSV streaming output ---

var csvHeader = []string{
	"account_phone", "account_id", "chat_jid", "chat_name",
	"detection_type", "matched_content", "amount", "message_id", "timestamp", "is_from_me",
}

type csvStreamWriter struct {
	f *os.File
	w *csv.Writer
	mu sync.Mutex
}

func newCSVWriter(path string) (*csvStreamWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create csv: %w", err)
	}
	// BOM for Excel UTF-8 compatibility
	f.Write([]byte{0xEF, 0xBB, 0xBF})

	w := csv.NewWriter(f)
	if err := w.Write(csvHeader); err != nil {
		f.Close()
		return nil, err
	}
	w.Flush()

	return &csvStreamWriter{f: f, w: w}, nil
}

func (cw *csvStreamWriter) writeResults(results []scanResult) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	for _, r := range results {
		cw.w.Write([]string{
			r.AccountPhone,
			strconv.FormatUint(uint64(r.AccountID), 10),
			r.ChatJID,
			r.ChatName,
			r.DetectionType,
			r.MatchedContent,
			r.Amount,
			r.MessageID,
			r.Timestamp.Format("2006-01-02 15:04:05"),
			strconv.FormatBool(r.IsFromMe),
		})
	}
	cw.w.Flush()
}

func (cw *csvStreamWriter) close() {
	cw.w.Flush()
	cw.f.Close()
}
