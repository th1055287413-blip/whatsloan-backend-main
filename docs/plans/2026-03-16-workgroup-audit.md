# Workgroup Audit Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let workgroup leaders query their members' key operations (send message, archive/unarchive chat, revoke/delete message).

**Architecture:** Extend existing `admin_operation_logs` table with `agent_id` and `workgroup_id` columns. Add logging to agent handlers. Add a leader-only query API.

**Tech Stack:** Go, Gin, GORM, PostgreSQL

---

### Task 1: Extend Model & DB Schema

**Files:**
- Modify: `internal/model/operation_log.go`
- Modify: `internal/service/system/operation_log.go`

**Step 1: Add agent fields to AdminOperationLog model**

In `internal/model/operation_log.go`, add to `AdminOperationLog` struct after line 92 (`OperatorUsername`):

```go
// Agent info (nullable, only set for agent operations)
AgentID     *uint  `json:"agent_id" gorm:"index:idx_op_logs_agent;index:idx_op_logs_wg_agent"`
WorkgroupID *uint  `json:"workgroup_id" gorm:"index:idx_op_logs_wg_agent"`
AgentName   string `json:"agent_name" gorm:"size:50"`
```

**Step 2: Add agent fields to LogEntry**

In `internal/model/operation_log.go`, add to `LogEntry` struct after `OperatorUsername`:

```go
AgentID     *uint
WorkgroupID *uint
AgentName   string
```

**Step 3: Add agent fields to OperationLogFilter**

In `internal/model/operation_log.go`, add to `OperationLogFilter` struct:

```go
AgentID     *uint  `form:"agent_id"`
WorkgroupID *uint  `form:"workgroup_id"`
```

**Step 4: Update buildLog() to map agent fields**

In `internal/service/system/operation_log.go`, in `buildLog()` method, add agent field mapping after line 58 (`OperatorUsername`):

```go
AgentID:     entry.AgentID,
WorkgroupID: entry.WorkgroupID,
AgentName:   entry.AgentName,
```

And add auto-extraction from gin context (after the existing `OperatorUsername` auto-extraction block, around line 101):

```go
// Extract agent info from context if not provided
if log.AgentID == nil {
    if agentID, exists := c.Get("agent_id"); exists {
        if aid, ok := agentID.(uint); ok {
            log.AgentID = &aid
        }
    }
}
if log.WorkgroupID == nil {
    if wgID, exists := c.Get("workgroup_id"); exists {
        if wid, ok := wgID.(uint); ok {
            log.WorkgroupID = &wid
        }
    }
}
if log.AgentName == "" {
    if agent, exists := c.Get("agent"); exists {
        if a, ok := agent.(*model.Agent); ok {
            log.AgentName = a.Username
        }
    }
}
```

Note: requires adding `"whatsapp_golang/internal/model"` import — but it's already imported via `model.LogEntry`.

**Step 5: Update GetList() to filter by agent fields**

In `internal/service/system/operation_log.go`, in `GetList()` method, add after the existing filter blocks (around line 128):

```go
if filter.AgentID != nil {
    query = query.Where("agent_id = ?", *filter.AgentID)
}
if filter.WorkgroupID != nil {
    query = query.Where("workgroup_id = ?", *filter.WorkgroupID)
}
```

**Step 6: Verify compilation**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 7: Commit**

```bash
git add internal/model/operation_log.go internal/service/system/operation_log.go
git commit -m "feat: add agent_id and workgroup_id to operation log model"
```

---

### Task 2: Add logging to Agent OperationsHandler

**Files:**
- Modify: `internal/handler/agent/operations_handler.go`
- Modify: `internal/app/handlers.go:133` (wire opLogService)

**Step 1: Add opLogService to OperationsHandler**

In `internal/handler/agent/operations_handler.go`, add field and update constructor:

```go
type OperationsHandler struct {
	svc            agentSvc.AgentOperationsService
	chatTagService contentSvc.ChatTagService
	messageAction  messagingSvc.MessageActionService
	opLogService   systemSvc.OperationLogService
}

func NewOperationsHandler(svc agentSvc.AgentOperationsService, chatTagService contentSvc.ChatTagService, messageAction messagingSvc.MessageActionService, opLogService systemSvc.OperationLogService) *OperationsHandler {
	return &OperationsHandler{svc: svc, chatTagService: chatTagService, messageAction: messageAction, opLogService: opLogService}
}
```

Add import: `systemSvc "whatsapp_golang/internal/service/system"`

**Step 2: Add logging to SendMessage**

After the success check (line 166, before `common.Success`), add:

```go
h.opLogService.LogAsync(&model.LogEntry{
    OperationType: model.OpSend,
    ResourceType:  model.ResMessage,
    ResourceID:    fmt.Sprintf("account:%d", accountID),
    AfterValue: map[string]interface{}{
        "account_id":   accountID,
        "contact_phone": req.ContactPhone,
        "content_type": req.MessageType,
    },
}, c)
```

Add import: `"fmt"` and ensure `"whatsapp_golang/internal/model"` is imported.

**Step 3: Add logging to RevokeMessage**

After the success check (line 282, before `common.Success`), add:

```go
h.opLogService.LogAsync(&model.LogEntry{
    OperationType: model.OpRevoke,
    ResourceType:  model.ResMessage,
    ResourceID:    fmt.Sprintf("%d", messageID),
    AfterValue: map[string]interface{}{
        "account_id": accountID,
        "message_id": messageID,
    },
}, c)
```

**Step 4: Add logging to DeleteMessageForMe**

After the success check (line 320, before `common.Success`), add:

```go
h.opLogService.LogAsync(&model.LogEntry{
    OperationType: model.OpDelete,
    ResourceType:  model.ResMessage,
    ResourceID:    fmt.Sprintf("%d", messageID),
    AfterValue: map[string]interface{}{
        "message_id": messageID,
        "deleted_by": deletedBy,
    },
}, c)
```

**Step 5: Update handler wiring in handlers.go**

In `internal/app/handlers.go:133`, change:

```go
AgentOperations: handlerAgent.NewOperationsHandler(svc.AgentOperations, svc.ChatTag, svc.MessageAction),
```

to:

```go
AgentOperations: handlerAgent.NewOperationsHandler(svc.AgentOperations, svc.ChatTag, svc.MessageAction, svc.OperationLog),
```

**Step 6: Verify compilation**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 7: Commit**

```bash
git add internal/handler/agent/operations_handler.go internal/app/handlers.go
git commit -m "feat: add operation logging to agent send/revoke/delete handlers"
```

---

### Task 3: Update shared ChatHandler for agent context

**Files:**
- Modify: `internal/handler/whatsapp/chat_handler.go`

**Step 1: Update ArchiveChat logging**

The existing `LogAsync` call in `ArchiveChat` (line 174-184) doesn't include agent info. The `buildLog()` auto-extraction (from Task 1) will handle this automatically since agent context is in `gin.Context`. No code change needed here — the auto-extraction in `buildLog()` already picks up `agent_id`, `workgroup_id`, and agent username from context.

**Step 2: Verify that admin calls still work correctly**

When called via admin routes, `agent_id` won't be in context, so `AgentID`/`WorkgroupID` stay nil. This is correct.

**Step 3: Verify compilation**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 4: Commit (skip if no changes needed)**

The `buildLog()` auto-extraction from Task 1 handles this. No commit needed unless manual changes were made.

---

### Task 4: Add leader activity-logs query API

**Files:**
- Create: `internal/handler/agent/activity_log_handler.go`
- Modify: `internal/app/handlers.go` (add ActivityLog handler)
- Modify: `internal/app/routes.go` (add route)

**Step 1: Create ActivityLogHandler**

Create `internal/handler/agent/activity_log_handler.go`:

```go
package agent

import (
	"github.com/gin-gonic/gin"
	"whatsapp_golang/internal/handler/common"
	"whatsapp_golang/internal/model"
	systemSvc "whatsapp_golang/internal/service/system"
)

// ActivityLogHandler 組長審計日誌處理器
type ActivityLogHandler struct {
	opLogService systemSvc.OperationLogService
}

// NewActivityLogHandler 建立審計日誌處理器
func NewActivityLogHandler(opLogService systemSvc.OperationLogService) *ActivityLogHandler {
	return &ActivityLogHandler{opLogService: opLogService}
}

// allowedOpTypes 組長可查詢的操作類型
var allowedOpTypes = map[string]bool{
	model.OpSend:      true,
	model.OpRevoke:    true,
	model.OpDelete:    true,
	model.OpArchive:   true,
	model.OpUnarchive: true,
}

// GetActivityLogs 查詢組員操作紀錄
func (h *ActivityLogHandler) GetActivityLogs(c *gin.Context) {
	wgID, ok := getWorkgroupID(c)
	if !ok {
		return
	}

	var filter model.OperationLogFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		common.Error(c, common.CodeInvalidParams, "參數錯誤")
		return
	}
	filter.SetDefaults()
	filter.WorkgroupID = &wgID

	// Validate operation_type if provided
	if filter.OperationType != "" && !allowedOpTypes[filter.OperationType] {
		common.Error(c, common.CodeInvalidParams, "不支援的操作類型")
		return
	}

	logs, total, err := h.opLogService.GetList(&filter)
	if err != nil {
		common.Error(c, common.CodeInternalError, "查詢失敗")
		return
	}

	common.PaginatedList(c, logs, total, filter.Page, filter.PageSize)
}
```

**Step 2: Add handler to Handlers struct**

In `internal/app/handlers.go`, add to `Handlers` struct (after `AgentLeader`):

```go
AgentActivityLog *handlerAgent.ActivityLogHandler
```

In `initHandlers()` return block, add:

```go
AgentActivityLog: handlerAgent.NewActivityLogHandler(svc.OperationLog),
```

**Step 3: Add route**

In `internal/app/routes.go`, inside `setupAgentRoutes`, add activity-logs route in the leader section. After the workgroup settings block (around line 998), add:

```go
// 組長審計日誌
agentAuth.GET("/activity-logs", middleware.RequireAgentRole("leader"), a.handlers.AgentActivityLog.GetActivityLogs)
```

**Step 4: Verify compilation**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 5: Commit**

```bash
git add internal/handler/agent/activity_log_handler.go internal/app/handlers.go internal/app/routes.go
git commit -m "feat: add leader activity-logs query API for workgroup audit"
```

---

### Task 5: Verify end-to-end & final commit

**Step 1: Full compilation check**

Run: `go build -o /dev/null ./cmd/api/...`
Expected: SUCCESS

**Step 2: Run any existing tests**

Run: `go test ./internal/handler/agent/... ./internal/service/system/... ./internal/model/... -v -count=1 2>&1 | tail -30`
Expected: All pass (or pre-existing failures only)

**Step 3: Review all changes**

Run: `git diff --stat`
Verify: only the expected files changed

**Step 4: Final commit (if any remaining changes)**

Only if there are uncommitted fixes from test failures.
