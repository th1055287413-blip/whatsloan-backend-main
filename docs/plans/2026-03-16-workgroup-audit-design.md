# 工作組組長審計組員操作

## 目標

讓工作組組長能查詢底下組員的關鍵操作紀錄：發送訊息、封存/解封聊天、收回/刪除訊息。

## 方案

擴展現有 `admin_operation_logs` 表，新增 agent 相關欄位，複用現有 `OperationLogService` 基礎設施。

## DB Schema 變更

`admin_operation_logs` 新增：

| 欄位 | 類型 | 說明 |
|------|------|------|
| `agent_id` | uint, nullable, indexed | FK → agents.id |
| `workgroup_id` | uint, nullable, indexed | FK → workgroups.id |

新增複合索引：`idx_op_logs_wg_agent (workgroup_id, agent_id, created_at DESC)`

## Model 變更

- `AdminOperationLog`: 加 `AgentID *uint`, `WorkgroupID *uint`
- `LogEntry`: 加 `AgentID *uint`, `WorkgroupID *uint`
- `OperationLogFilter`: 加 `AgentID *uint`, `WorkgroupID *uint`
- `buildLog()`: 映射 agent 欄位，自動從 gin context 取 `agent_id`

## 埋點

### Agent 專屬 handler（新增 opLogService 注入 + LogAsync 呼叫）

| 操作 | Handler 方法 | OpType |
|------|-------------|--------|
| 發送訊息 | `agent/OperationsHandler.SendMessage` | `send` |
| 收回訊息 | `agent/OperationsHandler.RevokeMessage` | `revoke` |
| 刪除訊息 | `agent/OperationsHandler.DeleteMessageForMe` | `delete` |

### 共用 handler（修改現有 LogAsync 呼叫，加入 agent 識別）

| 操作 | Handler 方法 | 改動 |
|------|-------------|------|
| 封存聊天 | `whatsapp/ChatHandler.ArchiveChat` | LogEntry 填入 agent_id/workgroup_id |
| 解封聊天 | `whatsapp/ChatHandler.UnarchiveChat` | 同上 |

判斷方式：檢查 gin context 中是否存在 `agent_id`，有則為 agent 操作。

## 查詢 API

```
GET /agent/activity-logs
```

- 權限：僅 role=leader 的 agent
- 查詢範圍：自動限定 workgroup_id = 組長所屬工作組
- 篩選：agent_id, operation_type, start_time, end_time, page, page_size
- 回傳：分頁列表（操作類型、操作者 username、資源資訊、時間、詳情）

## 不做的事

- 不建新表
- 不做統計彙總 API
- 不動現有 admin 查詢 API 行為
