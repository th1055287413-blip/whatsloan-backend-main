# Referral Agent Tracking Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 追蹤推薦連結是由哪個業務員（Agent）分享的，並在統一聊天列表中顯示來源。

**Architecture:** 推薦碼保持 per-account 不變。Agent 端取得 referral profile 時，share URL 動態追加 `&aid=<agent_id>`。前端落地頁將 `agent_id` 傳入 pairing API，透過 Redis session 透傳到 `ReferralRegistration.SourceAgentID`。統一聊天列表透過帳號的 referral 資訊批次查詢後 map 回各 chat row。

**Tech Stack:** Go, Gin, GORM, PostgreSQL, Redis

---

### Task 1: ReferralRegistration 加 SourceAgentID 欄位

**Files:**
- Modify: `internal/model/referral_registration.go:8-17`

**Step 1: 加欄位**

在 `ReferralRegistration` struct 的 `OperatorAdminID` 後面加上：

```go
SourceAgentID     *uint     `gorm:"index" json:"source_agent_id,omitempty"`
```

完整 struct：

```go
type ReferralRegistration struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	ReferralCode      string    `gorm:"size:12;not null;index" json:"referral_code"`
	SourceAccountID   uint      `gorm:"not null;index" json:"source_account_id"`
	NewAccountID      uint      `gorm:"uniqueIndex;not null" json:"new_account_id"`
	OperatorAdminID   *uint     `gorm:"index" json:"operator_admin_id,omitempty"`
	SourceAgentID     *uint     `gorm:"index" json:"source_agent_id,omitempty"`
	PromotionDomainID *uint     `gorm:"index" json:"promotion_domain_id,omitempty"`
	RegisteredAt      time.Time `gorm:"index" json:"registered_at"`
	Metadata          JSONB     `gorm:"type:jsonb;default:'{}'" json:"metadata"`
}
```

**Step 2: Build 驗證**

Run: `go build -o /dev/null ./cmd/api/...`

**Step 3: Commit**

```bash
git add internal/model/referral_registration.go
git commit -m "feat: add source_agent_id to ReferralRegistration model"
```

---

### Task 2: ReferralSessionInfo 加 SourceAgentID

**Files:**
- Modify: `internal/service/whatsapp/referral_session_service.go:12-19`

**Step 1: 加欄位**

在 `ReferralSessionInfo` struct 的 `SourceKey` 後面加上：

```go
SourceAgentID     *uint  `json:"source_agent_id,omitempty"`
```

**Step 2: Build 驗證**

Run: `go build -o /dev/null ./cmd/api/...`

**Step 3: Commit**

```bash
git add internal/service/whatsapp/referral_session_service.go
git commit -m "feat: add source_agent_id to ReferralSessionInfo"
```

---

### Task 3: GetPairingCodeRequest 加 AgentID + session handler 透傳

**Files:**
- Modify: `internal/handler/whatsapp/session_handler.go:40-44` (request struct)
- Modify: `internal/handler/whatsapp/session_handler.go:122-127` (store session with agent_id)

**Step 1: Request struct 加欄位**

```go
type GetPairingCodeRequest struct {
	PhoneNumber  string `json:"phone_number" binding:"required"`
	ChannelCode  string `json:"channel_code"`
	ReferralCode string `json:"referral_code"`
	SourceKey    string `json:"source_key"`
	AgentID      *uint  `json:"agent_id"` // 推薦來源業務員
}
```

**Step 2: 存 Redis 時帶上 AgentID**

在 `session_handler.go:122-127`，建立 `sessionInfo` 時加入 `SourceAgentID`：

```go
sessionInfo := &whatsapp.ReferralSessionInfo{
	ReferralCode:      req.ReferralCode,
	SourceAccountID:   validation.SourceAccountID,
	PromotionDomainID: validation.PromotionDomainID,
	SourceKey:         req.SourceKey,
	SourceAgentID:     req.AgentID, // 新增
}
```

**Step 3: Build 驗證**

Run: `go build -o /dev/null ./cmd/api/...`

**Step 4: Commit**

```bash
git add internal/handler/whatsapp/session_handler.go
git commit -m "feat: capture agent_id in pairing request and store in referral session"
```

---

### Task 4: Gateway event handler 寫入 SourceAgentID

**Files:**
- Modify: `internal/gateway/whatsapp_event_handler.go:564-570`

**Step 1: 建立 ReferralRegistration 時帶入 SourceAgentID**

在 `whatsapp_event_handler.go:564-570`，將：

```go
registration := model.ReferralRegistration{
	ReferralCode:      referralInfo.ReferralCode,
	SourceAccountID:   referralInfo.SourceAccountID,
	NewAccountID:      accountID,
	PromotionDomainID: referralInfo.PromotionDomainID,
	RegisteredAt:      now,
}
```

改為：

```go
registration := model.ReferralRegistration{
	ReferralCode:      referralInfo.ReferralCode,
	SourceAccountID:   referralInfo.SourceAccountID,
	NewAccountID:      accountID,
	SourceAgentID:     referralInfo.SourceAgentID,
	PromotionDomainID: referralInfo.PromotionDomainID,
	RegisteredAt:      now,
}
```

**Step 2: Build 驗證**

Run: `go build -o /dev/null ./cmd/api/...`

**Step 3: Commit**

```bash
git add internal/gateway/whatsapp_event_handler.go
git commit -m "feat: persist source_agent_id in referral registration on login"
```

---

### Task 5: Agent 端 referral-profile 回傳帶 agent_id 的 share URL

**Files:**
- Modify: `internal/handler/whatsapp/referral_handler.go:123-142`

**Step 1: 修改 GetReferralProfile handler**

在 Agent 端呼叫時，從 context 取 agent_id，動態追加到 share_url。注意此 handler 同時被 Admin 和 Agent 路由使用，需判斷有無 agent_id。

```go
func (h *ReferralHandler) GetReferralProfile(c *gin.Context) {
	accountIDStr := c.Param("id")
	accountID, err := strconv.ParseUint(accountIDStr, 10, 32)
	if err != nil {
		common.Error(c, common.CodeInvalidParams, "Invalid account ID")
		return
	}

	ctx := c.Request.Context()
	profile, err := h.referralService.GetReferralProfile(ctx, uint(accountID))
	if err != nil {
		logger.Ctx(ctx).Errorw("获取推荐信息失败", "account_id", accountID, "error", err)
		common.Error(c, common.CodeInternalError, "Failed to get referral profile")
		return
	}

	// Agent 端：在 share_url 追加 agent_id 讓裂變追蹤到具體業務員
	if agentIDVal, exists := c.Get("agent_id"); exists {
		if agentID, ok := agentIDVal.(uint); ok {
			separator := "&"
			if !strings.Contains(profile.ShareURL, "?") {
				separator = "?"
			}
			profile.ShareURL = fmt.Sprintf("%s%said=%d", profile.ShareURL, separator, agentID)
		}
	}

	common.Success(c, profile)
}
```

需要在檔案頂部 import 加入 `"strings"` 和 `"fmt"`（確認是否已存在）。

**Step 2: Build 驗證**

Run: `go build -o /dev/null ./cmd/api/...`

**Step 3: Commit**

```bash
git add internal/handler/whatsapp/referral_handler.go
git commit -m "feat: append agent_id to share_url when accessed from agent endpoint"
```

---

### Task 6: 統一聊天列表顯示來源

**Files:**
- Modify: `internal/service/agent/operations.go:28-33` (UnifiedChatRow struct)
- Modify: `internal/service/agent/operations.go:696-701` (post-query enrichment)

**Step 1: 擴展 UnifiedChatRow**

```go
type UnifiedChatRow struct {
	model.WhatsAppChat
	IsPinned        bool       `json:"is_pinned" gorm:"-"`
	PinnedAt        *time.Time `json:"pinned_at,omitempty"`
	SourceType      string     `json:"source_type" gorm:"-"`                    // "referral" | "channel" | "organic"
	SourceAgentName *string    `json:"source_agent_name,omitempty" gorm:"-"`
}
```

**Step 2: 在 GetUnifiedChats 的後處理階段加入來源資訊**

在 `for i := range rows` 迴圈（目前 line 697-699）後面，批次查帳號的 referral 資訊並 map 回 chat row：

```go
// 從 PinnedAt 推導 IsPinned
for i := range rows {
	rows[i].IsPinned = rows[i].PinnedAt != nil
}

// 批次查帳號來源資訊
accountIDSet := make(map[uint]bool)
for _, row := range rows {
	accountIDSet[row.AccountID] = true
}
uniqueAccountIDs := make([]uint, 0, len(accountIDSet))
for id := range accountIDSet {
	uniqueAccountIDs = append(uniqueAccountIDs, id)
}

// 查帳號的 channel_id 和 referred_by_account_id
type accountSource struct {
	ID                  uint  `gorm:"column:id"`
	ChannelID           *uint `gorm:"column:channel_id"`
	ReferredByAccountID *uint `gorm:"column:referred_by_account_id"`
}
var sources []accountSource
s.db.Model(&model.WhatsAppAccount{}).
	Select("id, channel_id, referred_by_account_id").
	Where("id IN ?", uniqueAccountIDs).
	Find(&sources)

sourceMap := make(map[uint]accountSource)
for _, src := range sources {
	sourceMap[src.ID] = src
}

// 查 referral_registrations 中有 source_agent_id 的記錄，取得 agent 名稱
// 用 referred_by_account_id 不為 NULL 的帳號 ID 去查
referredAccountIDs := make([]uint, 0)
for _, src := range sources {
	if src.ReferredByAccountID != nil {
		referredAccountIDs = append(referredAccountIDs, src.ID)
	}
}

type agentNameRow struct {
	NewAccountID uint   `gorm:"column:new_account_id"`
	Username     string `gorm:"column:username"`
}
agentNameMap := make(map[uint]string)
if len(referredAccountIDs) > 0 {
	var agentNames []agentNameRow
	s.db.Raw(`
		SELECT rr.new_account_id, a.username
		FROM referral_registrations rr
		JOIN agents a ON a.id = rr.source_agent_id AND a.deleted_at IS NULL
		WHERE rr.new_account_id IN ? AND rr.source_agent_id IS NOT NULL
	`, referredAccountIDs).Scan(&agentNames)
	for _, an := range agentNames {
		agentNameMap[an.NewAccountID] = an.Username
	}
}

// 填充 SourceType 和 SourceAgentName
for i := range rows {
	src, ok := sourceMap[rows[i].AccountID]
	if !ok {
		rows[i].SourceType = "organic"
		continue
	}
	if src.ReferredByAccountID != nil {
		rows[i].SourceType = "referral"
		if name, exists := agentNameMap[rows[i].AccountID]; exists {
			rows[i].SourceAgentName = &name
		}
	} else if src.ChannelID != nil {
		rows[i].SourceType = "channel"
	} else {
		rows[i].SourceType = "organic"
	}
}
```

**Step 3: Build 驗證**

Run: `go build -o /dev/null ./cmd/api/...`

**Step 4: Commit**

```bash
git add internal/service/agent/operations.go
git commit -m "feat: enrich unified chat list with source_type and source_agent_name"
```

---

### Task 7: 整合驗證

**Step 1: 完整 build**

Run: `go build -o /dev/null ./cmd/api/... && go build -o /dev/null ./cmd/connector/...`

**Step 2: 確認 referral handler import**

確認 `internal/handler/whatsapp/referral_handler.go` 的 import 包含 `"strings"` 和 `"fmt"`。

**Step 3: Code review 檢查**

- `ReferralRegistration` model 欄位順序一致
- `ReferralSessionInfo` JSON tag 正確
- `GetPairingCodeRequest.AgentID` 是 `*uint`（nullable）
- Gateway event handler 正確透傳 `SourceAgentID`
- Agent referral profile URL 拼接邏輯正確（處理有無 `?` 的情況）
- 統一聊天列表的批次查詢不會 N+1
- `gorm:"-"` tag 確保新欄位不從 DB 掃描
