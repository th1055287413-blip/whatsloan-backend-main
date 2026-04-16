# 推薦連結追蹤引入業務員

## 背景

現有推薦碼系統是 per-account 的，無法分辨是哪個業務員（Agent）分享的連結。組長需要在統一聊天列表中看到每個客人的來源：渠道自然進入、還是某個業務員引入。

## 方案

推薦碼保持 per-account 不變，Agent 端取得的 share URL 動態追加 `&aid=<agent_id>`。客人透過連結進來時，agent_id 隨推薦碼一起透傳到 `ReferralRegistration`。

## 資料流

```
Agent 前端 GET /agent/accounts/:id/referral-profile
  → API 回傳 share_url 帶 &aid=<agent_id>
  → Agent 複製連結給客人
  → 客人點擊 → 落地頁 → POST /sessions/pairing-code 帶 agent_id
  → session_handler 存入 Redis（ReferralSessionInfo.SourceAgentID）
  → 配對成功 → gateway event_handler 從 Redis 讀取
  → 寫入 ReferralRegistration.SourceAgentID
  → 統一聊天列表 JOIN 顯示來源
```

## 資料變更

### ReferralSessionInfo（Redis 暫存）

新增 `SourceAgentID *uint` 欄位。

### ReferralRegistration（DB 表）

新增 `source_agent_id` 欄位（nullable, indexed）。

### GetPairingCodeRequest

新增 `AgentID *uint` 欄位，前端落地頁從 URL `aid` 參數取得後傳入。

### Agent 端 referral-profile API

Handler 從 context 取 agent_id，在回傳的 `share_url` 後追加 `&aid=<agent_id>`。不修改 DB 中 `referral_codes.share_url`（那是不帶 agent 的基礎 URL）。

### Session handler

從 `req.AgentID` 存入 `ReferralSessionInfo.SourceAgentID`。

### Gateway event handler

建立 `ReferralRegistration` 時帶入 `referralInfo.SourceAgentID`。

## 統一聊天列表顯示

`UnifiedChatRow` 新增：

- `SourceType string` — `"referral"` | `"channel"` | `"organic"`
- `SourceAgentName *string` — 引入的業務員名稱

判斷邏輯（Go 層）：

- `referred_by_account_id IS NOT NULL` → `"referral"`
- `channel_id IS NOT NULL` → `"channel"`
- 都沒有 → `"organic"`

`source_agent_name` 透過 `referral_registrations.source_agent_id → agents.username` 取得。

來源是帳號級屬性，先批次查帳號的 referral info 再 map 回 chat row，避免 per-row JOIN。

## 不在範圍

- 裂變統計頁加 agent 維度篩選
- Per-agent 推薦碼
