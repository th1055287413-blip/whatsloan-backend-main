# Unified Chat List & Pinned Chats Design

## Problem

業務組 Agent 同時管理多個 WhatsApp 帳號，需要在不同帳號的聊天室之間頻繁切換。目前聊天列表以帳號為單位查詢（`GET /account/:id/chats`），缺乏跨帳號的統一工作視圖。

## Goals

1. **跨帳號統一聊天列表**：Agent 在一個介面中看到所有可存取帳號的聊天
2. **釘選聊天**：Agent 可釘選重要聊天至列表頂部，跨帳號快速存取
3. **釘選數量上限**：寫死常數限制每個 Agent 的釘選數量

## Design

### New Model: `AgentPinnedChat`

```go
type AgentPinnedChat struct {
    ID        uint      `gorm:"primaryKey"`
    AgentID   uint      `gorm:"index:idx_agent_pinned,unique"`
    ChatID    uint      `gorm:"index:idx_agent_pinned,unique"`
    PinnedAt  time.Time
    CreatedAt time.Time
}
```

- 聯合唯一索引 `(agent_id, chat_id)` 防止重複釘選
- 不需要 `DeletedAt`（無軟刪除需求）
- 釘選上限常數：`MaxPinnedChats = 10`

### New APIs

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/agent/chats` | 跨帳號統一聊天列表 |
| `POST` | `/agent/chats/:chatId/pin` | 釘選聊天 |
| `DELETE` | `/agent/chats/:chatId/pin` | 取消釘選 |

### Unified Chat List Query (`GET /agent/chats`)

**Query parameters:**
- `keyword` (optional): 搜尋聊天名稱/JID
- `archived` (optional): 篩選歸檔狀態
- `page`, `page_size`: 分頁

**Query logic:**

1. 取得 agent 可存取的所有 `account_ids`（複用現有 `getAccessibleAccountIDs`）
2. 查詢所有帳號的聊天，`phone_jid` dedup（`DISTINCT ON`）
3. `LEFT JOIN agent_pinned_chats` 取得釘選狀態
4. 排序：`is_pinned DESC, pinned_at DESC, last_time DESC`
5. 支援 keyword 搜尋、archived 篩選、分頁

**Response 額外欄位：**
- `account_id`: 聊天所屬帳號（前端需要知道透過哪個帳號發訊息）
- `is_pinned`: 是否被當前 agent 釘選

### Pin Chat (`POST /agent/chats/:chatId/pin`)

1. 驗證 agent 有權限存取該 chat 所屬的 account
2. `COUNT` agent 現有釘選數 `>= MaxPinnedChats` → 回傳錯誤
3. `INSERT` agent_pinned_chats（`ON CONFLICT DO NOTHING` 處理冪等）

### Unpin Chat (`DELETE /agent/chats/:chatId/pin`)

1. 驗證 agent 有權限存取該 chat
2. `DELETE FROM agent_pinned_chats WHERE agent_id = ? AND chat_id = ?`

### Agent Deletion Cleanup

Agent 使用 GORM 軟刪除（`deleted_at`），DB 層 `ON DELETE CASCADE` 不會觸發。在 `Delete()` 和 `DeleteMember()` service 方法中，軟刪除 agent 前一併硬刪除釘選記錄：

```go
s.db.Where("agent_id = ?", id).Delete(&model.AgentPinnedChat{})
```

### Edge Cases

| 情境 | 處理方式 |
|------|----------|
| Agent 失去帳號存取權 | 統一列表自然不回傳該帳號的聊天，釘選記錄保留（lazy cleanup），不佔上限計數——查詢時 JOIN 過濾 |
| 聊天被歸檔 | `archived=false` 篩選時不顯示，釘選關係保留 |
| Agent 被刪除 | service 層硬刪除所有 `agent_pinned_chats` 記錄 |
| 重複釘選 | 聯合唯一索引 + `ON CONFLICT DO NOTHING` 保證冪等 |
| 釘選數量上限 | 常數 `MaxPinnedChats = 10`，PIN 前 COUNT 檢查 |

### Migration

1. 建立 `agent_pinned_chats` 表
2. GORM AutoMigrate 即可，無需資料回填
