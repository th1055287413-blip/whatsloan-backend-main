# sync-status Specification

## Purpose
TBD - created by archiving change add-sync-status-tracking. Update Purpose after archive.
## Requirements
### Requirement: 同步步驟狀態追蹤

系統 SHALL 為每個 WhatsApp 帳號追蹤以下四個同步步驟的狀態：
- `account_connect` - 帳號連接
- `chat_sync` - 聊天列表同步
- `history_sync` - 歷史訊息同步
- `contact_sync` - 聯絡人同步

每個步驟 SHALL 記錄：
- 狀態 (`pending` / `queued` / `running` / `completed` / `failed`)
- 開始時間
- 完成時間
- 錯誤訊息（失敗時）
- 進度或數量（如適用）

#### Scenario: 任務入隊時更新狀態
- **WHEN** 同步任務加入 Redis 隊列
- **THEN** 該步驟狀態更新為 `queued`

#### Scenario: 任務開始處理時更新狀態
- **WHEN** 同步任務開始執行
- **THEN** 該步驟狀態更新為 `running`
- **AND** 記錄開始時間

#### Scenario: 任務完成時更新狀態
- **WHEN** 同步任務成功完成
- **THEN** 該步驟狀態更新為 `completed`
- **AND** 記錄完成時間
- **AND** 記錄同步數量（如適用）

#### Scenario: 任務失敗時更新狀態
- **WHEN** 同步任務執行失敗
- **THEN** 該步驟狀態更新為 `failed`
- **AND** 記錄錯誤訊息

---

### Requirement: 同步狀態 API

系統 SHALL 提供 API 端點讓後台查詢帳號同步狀態。

#### Scenario: 獲取單一帳號同步狀態
- **WHEN** 呼叫 `GET /accounts/:id/sync-status`
- **THEN** 回傳該帳號所有同步步驟的詳細狀態
- **AND** 包含每個步驟的狀態、時間、錯誤訊息、進度

#### Scenario: 帳號列表包含同步狀態摘要
- **WHEN** 呼叫 `GET /accounts` 取得帳號列表
- **THEN** 每個帳號包含簡要同步狀態欄位
- **AND** 包含整體同步狀態和最後完成時間

---

