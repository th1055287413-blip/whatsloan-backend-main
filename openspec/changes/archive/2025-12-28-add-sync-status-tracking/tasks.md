# Tasks: 同步狀態追蹤功能

## 1. 資料層

- [x] 1.1 新增 `model/sync_status.go`，定義 `WhatsAppSyncStatus` 模型和常量
- [x] 1.2 執行資料庫遷移，建立 `whatsapp_sync_status` 表

## 2. 服務層

- [x] 2.1 新增 `service/whatsapp/sync_status.go`，實作狀態更新方法
- [x] 2.2 修改 `service/whatsapp/service.go`，注入 SyncStatusService
- [x] 2.3 修改 `service/whatsapp/sync_task_handler.go`，處理時更新狀態（入隊狀態更新整合在此）

## 3. API 層

- [x] 3.1 新增 `handler/whatsapp/sync_status_handler.go`，實作 GetSyncStatus
- [x] 3.2 修改 `handler/whatsapp/account_handler.go`，帳號列表加入同步狀態
- [x] 3.3 修改 `app/routes.go`，新增 `/accounts/:id/sync-status` 路由

## 4. 前端

- [x] 4.1 修改 `frontend/src/api/whatsapp.ts`，新增同步狀態 API 類型定義
- [x] 4.2 修改 `frontend/src/views/users/UserList.vue`，新增帳號同步狀態區域

## 5. 驗證

- [x] 5.1 後端編譯通過 (`go build -o /dev/null ./cmd/api/...`)
- [ ] 5.2 部署後實際測試同步流程
