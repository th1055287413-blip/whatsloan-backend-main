# 歷史同步媒體下載

## 問題

`handleHistorySync` 只解析訊息內容並 publish，不下載媒體。導致歷史同步的 image/audio/video/document/sticker 全部 `media_url` 為空。

## 決策

- **方案 2：非同步補下載** — 歷史同步流程不變（快速 publish），另起 worker pool 非同步下載媒體，完成後通知 API 更新 DB。
- **不做 media retry**（phase 1）。過期的（404/410）直接放棄。
- **後續 phase 2** 可加入 `SendMediaRetryReceipt()` 讓手機端重傳過期媒體，屆時也可考慮改為查看時下載（方案 3）以節省資源。

## 流程

```
handleHistorySync
  ├─ 原流程不變：parse → buildPayload → publish batch（快速）
  └─ 額外：把媒體訊息 enqueue 到 media download queue

media download worker pool（per-account goroutine pool）
  ├─ 從 queue 取任務
  ├─ downloadAndUploadMedia（複用現有函式）
  ├─ 成功 → publish MediaDownloaded 事件
  └─ 失敗（410/404）→ 靜默跳過，log warning

API 端 EventHandler
  └─ OnMediaDownloaded → UPDATE whatsapp_messages SET media_url WHERE message_id AND account_id
```

## 新增 Event

```go
EvtMediaDownloaded EventType = "media_downloaded"

type MediaDownloadedPayload struct {
    MessageID string `json:"message_id"`
    ChatJID   string `json:"chat_jid"`
    MediaURL  string `json:"media_url"`
}
```

## 設計決策

1. **並發控制**：每帳號固定 3 goroutine worker，避免大量並發下載
2. **只處理媒體類型**：image、video、audio、document、sticker
3. **下載失敗不 retry**：404/410 直接放棄
4. **生命週期**：worker pool 綁定帳號的 event worker context，帳號移除時中斷
5. **不阻塞同步**：訊息先到前端可看文字，媒體非同步補上

## 影響範圍

| 檔案 | 變更 |
|------|------|
| `connector/whatsmeow/event.go` | `handleHistorySync` enqueue 媒體下載任務 |
| `connector/whatsmeow/manager.go` | 新增 media download worker pool |
| `protocol/events.go` | 新增 `EvtMediaDownloaded` 事件定義 |
| `connector/stream_producer.go` | 新增 `PublishMediaDownloaded` |
| `gateway/event_consumer.go` | 處理 `EvtMediaDownloaded` 事件 |
| `gateway/whatsapp_event_handler.go` | 新增 `OnMediaDownloaded` 更新 DB |
