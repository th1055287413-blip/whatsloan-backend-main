## 1. 基礎建設

- [ ] 1.1 更新 `internal/protocol/` 新增管理命令類型（ManageStartConnector / ManageStopConnector / ManageRestartConnector）和管理事件回應（ManageCommandAck）
- [ ] 1.2 新增管理命令 Stream 消費者（Connector 服務端，XREADGROUP 消費 `connector:manage`，執行 Pool 層級的 Start/Stop/Restart）
- [ ] 1.3 建立 `cmd/connector/main.go` — Connector 服務入口點（Pool + 管理命令消費者 + HTTP）
- [ ] 1.4 新增輕量 HTTP 端點（`/health`、`/metrics`）
- [ ] 1.5 擴展心跳 payload，寫入 AccountCount、AccountIDs、Uptime、StartTime、EventWorkerStats 到 Redis

## 2. 主服務改造

- [ ] 2.1 `connector_config_service.go` — 移除 `connector.Pool` 依賴，Start/Stop/Restart 改為透過 Gateway `SendManageCommandAndWait` 發送管理命令，GetStatus 改為從 Redis 心跳資料讀取
- [ ] 2.2 `connector_gateway.go` — 新增 `SendManageCommand` / `SendManageCommandAndWait`，發送到 `connector:manage` stream
- [ ] 2.3 `EventConsumer` — 新增 `ManageCommandAck` 事件處理，觸發 `NotifyCommandSuccess`
- [ ] 2.4 `internal/app/services.go` — 移除 ConnectorPool 初始化、ConnectorConfigService 的 Pool 注入、背景恢復 goroutine
- [ ] 2.5 `internal/app/app.go` — 移除 ConnectorPool.StopAll()、BusinessCollector 的 Pool 參數
- [ ] 2.6 `internal/metrics/collector.go` — 移除 `connector.Pool` 依賴，僅保留 Redis Stream 指標（Connector 服務自己暴露 /metrics）
- [ ] 2.7 `internal/handler/system/monitor_handler.go` — 移除 `connector.Pool` 依賴，改為從 Redis 心跳資料讀取 connector 狀態和 EventWorkerStats

## 3. 版本與部署配置

- [ ] 3.1 `internal/config/version.go` — 新增 `ConnectorVersion` 變數，Connector binary 透過 ldflags 注入
- [ ] 3.2 新增 `Dockerfile.connector`
- [ ] 3.3 更新 `docker-compose.yml` 新增 connector 服務定義，使用獨立 image tag 環境變數（`API_TAG`、`CONNECTOR_TAG`）
- [ ] 3.4 修改 `.github/workflows/deploy-api.yml` trigger 從 `v*` 改為 `api/v*`
- [ ] 3.5 新增 `.github/workflows/deploy-connector.yml`，trigger `connector/v*` tags
- [x] 3.6 更新 `.claude/commands/bump.md` 和 `release.md` 支援選擇 api / connector 服務

## 4. 驗證

- [ ] 4.1 `go build -o /dev/null ./cmd/api/...` 和 `go build -o /dev/null ./cmd/connector/...` 均通過
- [ ] 4.2 Connector 服務獨立啟動，自動恢復帳號連線
- [ ] 4.3 主服務透過 Redis Stream 發送管理命令，Connector 正確回應
- [ ] 4.4 Connector 重啟不影響主 API 服務
