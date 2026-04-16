# Unified Chat List & Pinned Chats Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a cross-account unified chat list and per-agent pinned chats feature for business team agents.

**Architecture:** New `AgentPinnedChat` model with a separate table. Unified chat list query aggregates all accessible accounts' chats with phone_jid dedup, LEFT JOIN pinned status. Pin/unpin APIs on `AgentOperationsService`. Cleanup on agent deletion.

**Tech Stack:** Go, Gin, GORM, PostgreSQL

**Design doc:** `docs/plans/2026-03-17-unified-chat-list-design.md`

---

### Task 1: Add `AgentPinnedChat` Model

**Files:**
- Modify: `internal/model/agent.go` (append new model at end of file)

**Step 1: Add the model and constant**

Append to `internal/model/agent.go`:

```go
// MaxPinnedChats Agent 釘選聊天數量上限
const MaxPinnedChats = 10

// AgentPinnedChat Agent 釘選的聊天
type AgentPinnedChat struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	AgentID   uint      `gorm:"not null;uniqueIndex:idx_agent_pinned_chat" json:"agent_id"`
	ChatID    uint      `gorm:"not null;uniqueIndex:idx_agent_pinned_chat" json:"chat_id"`
	PinnedAt  time.Time `json:"pinned_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (AgentPinnedChat) TableName() string {
	return "agent_pinned_chats"
}
```

**Step 2: Register in AutoMigrate**

In `internal/database/database.go`, find the `tables` slice in `Migrate()` (around line 81-114). Add `&model.AgentPinnedChat{}` after `&model.Agent{}` (line 109):

```go
&model.Agent{},                // 業務員（外部用戶）
&model.AgentPinnedChat{},      // Agent 釘選聊天
```

**Step 3: Verify build**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 4: Commit**

```
feat: add AgentPinnedChat model with max pin limit constant
```

---

### Task 2: Add Service Methods (Interface + Implementation)

**Files:**
- Modify: `internal/service/agent/operations.go`

**Step 1: Add methods to `AgentOperationsService` interface**

In `internal/service/agent/operations.go`, add to the interface (around line 27-48):

```go
// Unified chat list
GetUnifiedChats(agentID uint, page, pageSize int, search string, archived *bool) ([]UnifiedChatRow, int64, error)

// Pin/Unpin
PinChat(agentID, chatID uint) error
UnpinChat(agentID, chatID uint) error
```

**Step 2: Define `UnifiedChatRow` struct**

Add above the interface definition:

```go
// UnifiedChatRow 統一聊天列表的回傳結構，包含釘選狀態
type UnifiedChatRow struct {
	model.WhatsAppChat
	IsPinned bool       `json:"is_pinned"`
	PinnedAt *time.Time `json:"pinned_at,omitempty"`
}
```

**Step 3: Implement `GetUnifiedChats`**

Add the method to `agentOperationsService`:

```go
func (s *agentOperationsService) GetUnifiedChats(agentID uint, page, pageSize int, search string, archived *bool) ([]UnifiedChatRow, int64, error) {
	accountIDs, err := s.getAccessibleAccountIDs(agentID)
	if err != nil {
		return nil, 0, err
	}
	if len(accountIDs) == 0 {
		return []UnifiedChatRow{}, 0, nil
	}

	// Step 1: get deduplicated chat IDs across all accounts
	var allChatIDs []uint
	if err := s.db.Model(&model.WhatsAppMessage{}).
		Select("DISTINCT chat_id").
		Where("account_id IN ?", accountIDs).
		Pluck("chat_id", &allChatIDs).Error; err != nil {
		return nil, 0, err
	}
	if len(allChatIDs) == 0 {
		return []UnifiedChatRow{}, 0, nil
	}

	var dedupedIDs []uint
	if err := s.db.Raw(`
		SELECT DISTINCT ON (c.account_id, COALESCE(m.pn || '@s.whatsapp.net', c.jid)) c.id
		FROM whatsapp_chats c
		LEFT JOIN whatsmeow_lid_map m ON c.jid LIKE '%@lid' AND m.lid = REPLACE(c.jid, '@lid', '')
		WHERE c.account_id IN (?) AND c.id IN (?)
		  AND (c.jid LIKE '%@s.whatsapp.net' OR c.jid LIKE '%@g.us' OR c.jid LIKE '%@lid')
		ORDER BY c.account_id, COALESCE(m.pn || '@s.whatsapp.net', c.jid), c.last_time DESC
	`, accountIDs, allChatIDs).Scan(&dedupedIDs).Error; err != nil {
		return nil, 0, err
	}
	if len(dedupedIDs) == 0 {
		return []UnifiedChatRow{}, 0, nil
	}

	// Step 2: build base query with optional filters
	baseQuery := s.db.Table("whatsapp_chats c").
		Select("c.*, p.pinned_at IS NOT NULL AS is_pinned, p.pinned_at").
		Joins("LEFT JOIN agent_pinned_chats p ON p.chat_id = c.id AND p.agent_id = ?", agentID).
		Where("c.id IN ?", dedupedIDs)

	if search != "" {
		pattern := "%" + search + "%"
		baseQuery = baseQuery.Where("c.name ILIKE ? OR c.jid ILIKE ?", pattern, pattern)
	}
	if archived != nil {
		baseQuery = baseQuery.Where("c.archived = ?", *archived)
	}

	// Step 3: count
	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Step 4: fetch with pinned-first ordering
	var rows []UnifiedChatRow
	offset := (page - 1) * pageSize
	if err := baseQuery.
		Order("is_pinned DESC, p.pinned_at DESC, c.last_time DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	return rows, total, nil
}
```

**Step 4: Implement `PinChat`**

```go
func (s *agentOperationsService) PinChat(agentID, chatID uint) error {
	if err := s.VerifyChatAccess(agentID, chatID); err != nil {
		return err
	}

	// Check limit
	var count int64
	s.db.Model(&model.AgentPinnedChat{}).Where("agent_id = ?", agentID).Count(&count)
	if count >= model.MaxPinnedChats {
		return fmt.Errorf("釘選數量已達上限 (%d)", model.MaxPinnedChats)
	}

	now := time.Now()
	result := s.db.Where("agent_id = ? AND chat_id = ?", agentID, chatID).
		FirstOrCreate(&model.AgentPinnedChat{
			AgentID:  agentID,
			ChatID:   chatID,
			PinnedAt: now,
		})
	return result.Error
}
```

**Step 5: Implement `UnpinChat`**

```go
func (s *agentOperationsService) UnpinChat(agentID, chatID uint) error {
	if err := s.VerifyChatAccess(agentID, chatID); err != nil {
		return err
	}

	return s.db.Where("agent_id = ? AND chat_id = ?", agentID, chatID).
		Delete(&model.AgentPinnedChat{}).Error
}
```

**Step 6: Add missing imports if needed**

Ensure `"time"` and `"fmt"` are in the imports (both should already exist).

**Step 7: Verify build**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 8: Commit**

```
feat: add unified chat list and pin/unpin service methods
```

---

### Task 3: Add Handler Methods

**Files:**
- Modify: `internal/handler/agent/operations_handler.go`

**Step 1: Add `GetUnifiedChats` handler**

```go
// GetUnifiedChats 跨帳號統一聊天列表
func (h *OperationsHandler) GetUnifiedChats(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}

	p := common.ParsePaginationParams(c)
	search := c.Query("keyword")
	var archived *bool
	if v := c.Query("archived"); v != "" {
		b := v == "true"
		archived = &b
	}

	rows, total, err := h.svc.GetUnifiedChats(agentID, p.Page, p.PageSize, search, archived)
	if err != nil {
		common.HandleServiceError(c, err, "對話")
		return
	}

	common.PaginatedList(c, rows, total, p.Page, p.PageSize)
}
```

**Step 2: Add `PinChat` handler**

```go
// PinChat 釘選聊天
func (h *OperationsHandler) PinChat(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}

	chatID, ok := common.MustParseUintParam(c, "chatId")
	if !ok {
		return
	}

	if err := h.svc.PinChat(agentID, chatID); err != nil {
		common.HandleServiceError(c, err, "釘選")
		return
	}

	common.Success(c, nil)
}
```

**Step 3: Add `UnpinChat` handler**

```go
// UnpinChat 取消釘選
func (h *OperationsHandler) UnpinChat(c *gin.Context) {
	agentID, ok := getAgentID(c)
	if !ok {
		return
	}

	chatID, ok := common.MustParseUintParam(c, "chatId")
	if !ok {
		return
	}

	if err := h.svc.UnpinChat(agentID, chatID); err != nil {
		common.HandleServiceError(c, err, "取消釘選")
		return
	}

	common.Success(c, nil)
}
```

**Step 4: Check if `MustParseUintParam` exists**

Search for `MustParseUintParam` in `internal/handler/common/`. If it doesn't exist, use `MustParseID` with path param name `chatId` — check the existing pattern (e.g., archive/unarchive handler uses `chatId` param). Adapt accordingly:

```go
chatIDStr := c.Param("chatId")
chatID, err := strconv.ParseUint(chatIDStr, 10, 64)
if err != nil {
	common.Error(c, common.CodeInvalidParams, "無效的聊天 ID")
	return
}
```

**Step 5: Verify build**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 6: Commit**

```
feat: add unified chat list and pin/unpin handlers
```

---

### Task 4: Register Routes

**Files:**
- Modify: `internal/app/routes.go` (in `setupAgentRoutes`, around line 1030)

**Step 1: Add routes**

Add after the existing `agentAuth` chat routes (after line 1016, before the media upload section):

```go
// 統一聊天列表 & 釘選
agentAuth.GET("/chats", a.handlers.AgentOperations.GetUnifiedChats)
agentAuth.POST("/chats/:chatId/pin", middleware.AgentWritePermission(), a.handlers.AgentOperations.PinChat)
agentAuth.DELETE("/chats/:chatId/pin", middleware.AgentWritePermission(), a.handlers.AgentOperations.UnpinChat)
```

Note: These use `/chats` (not `/accounts/:id/chats`) since they are cross-account operations.

**Step 2: Verify build**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 3: Commit**

```
feat: register unified chat list and pin/unpin routes
```

---

### Task 5: Agent Deletion Cleanup

**Files:**
- Modify: `internal/service/agent/management.go`

**Step 1: Add cleanup to `Delete` method (line 110-116)**

Change from:
```go
func (s *agentManagementService) Delete(id uint) error {
	s.db.Model(&model.WorkgroupAccount{}).Where("assigned_agent_id = ?", id).
		Update("assigned_agent_id", nil)

	return s.db.Delete(&model.Agent{}, id).Error
}
```

To:
```go
func (s *agentManagementService) Delete(id uint) error {
	s.db.Model(&model.WorkgroupAccount{}).Where("assigned_agent_id = ?", id).
		Update("assigned_agent_id", nil)

	s.db.Where("agent_id = ?", id).Delete(&model.AgentPinnedChat{})

	return s.db.Delete(&model.Agent{}, id).Error
}
```

**Step 2: Add cleanup to `DeleteMember` method (line 154-165)**

Change from:
```go
func (s *agentManagementService) DeleteMember(workgroupID, memberID uint) error {
	var agent model.Agent
	if err := s.db.Where("id = ? AND workgroup_id = ? AND role = ?", memberID, workgroupID, "member").First(&agent).Error; err != nil {
		return errors.New("組員不存在或不屬於此工作組")
	}

	s.db.Model(&model.WorkgroupAccount{}).Where("assigned_agent_id = ?", memberID).
		Update("assigned_agent_id", nil)

	return s.db.Delete(&agent).Error
}
```

To:
```go
func (s *agentManagementService) DeleteMember(workgroupID, memberID uint) error {
	var agent model.Agent
	if err := s.db.Where("id = ? AND workgroup_id = ? AND role = ?", memberID, workgroupID, "member").First(&agent).Error; err != nil {
		return errors.New("組員不存在或不屬於此工作組")
	}

	s.db.Model(&model.WorkgroupAccount{}).Where("assigned_agent_id = ?", memberID).
		Update("assigned_agent_id", nil)

	s.db.Where("agent_id = ?", memberID).Delete(&model.AgentPinnedChat{})

	return s.db.Delete(&agent).Error
}
```

**Step 3: Add import for model package if not already imported**

Check imports at top of `management.go` — likely already has `"whatsapp_golang/internal/model"`.

**Step 4: Verify build**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 5: Commit**

```
feat: clean up pinned chats on agent deletion
```

---

### Task 6: Integration Verification

**Step 1: Full build check**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 2: Run existing tests**

Run: `go test ./internal/service/agent/... -v -count=1`
Expected: Existing tests still pass (note: `gateway_test.go` has a known pre-existing issue, ignore it)

**Step 3: Verify API version bump needed**

Check `internal/config/version.go` or similar for API version constant. Consider bumping if needed.

**Step 4: Final commit (if version bump)**

```
chore: bump api version for unified chat list feature
```
