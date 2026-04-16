# Change: 新增同步狀態追蹤功能

## Why

目前將同步功能加入 Redis 隊列處理後，後台管理員無法知道帳號同步的即時狀態（是否排隊中、同步進行中、已完成或失敗），導致難以監控和排查問題。

## What Changes

- **新增** `whatsapp_sync_status` 資料表，追蹤每個帳號的各同步步驟狀態
- **新增** 同步狀態服務 (`SyncStatusService`)，提供狀態更新方法
- **修改** Redis 隊列入隊邏輯，入隊時更新狀態為 `queued`
- **修改** 同步任務處理器，處理開始/完成/失敗時更新對應狀態
- **新增** API 端點 `GET /accounts/:id/sync-status` 獲取同步詳情
- **修改** 帳號列表 API，加入簡要同步狀態欄位

## Impact

- Affected specs: `sync-status` (新增)
- Affected code:
  - `backend/internal/model/` - 新增 sync_status 模型
  - `backend/internal/service/whatsapp/` - 新增 sync_status 服務、修改 sync_task_handler
  - `backend/internal/queue/sync_queue.go` - 修改入隊方法
  - `backend/internal/handler/whatsapp/` - 新增/修改 handler
  - `backend/internal/app/routes.go` - 新增路由
