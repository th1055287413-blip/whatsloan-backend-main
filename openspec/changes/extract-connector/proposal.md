# Change: 將 Connector 從主服務分離為獨立 Binary

## Why

目前 Connector（WhatsApp 連線管理）嵌入在主 API 服務中，導致：
- Connector 重啟需要整個 API 服務重啟，影響所有用戶
- 無法獨立擴展 Connector 實例（例如按地區/代理分佈）
- Connector 的記憶體和 CPU 使用直接影響 API 回應時間
- 單一 binary 部署限制了運維靈活性

## What Changes

- 新增獨立的 `cmd/connector/main.go` 作為 Connector 服務入口（一個進程 = 一個 Pool，管理多個 connector 實例）
- Connector 服務透過 Redis Streams 接收業務命令（現有機制，無需變更）+ 管理命令（新增）
- Connector 服務暴露輕量 HTTP 端點供健康檢查和 Prometheus metrics
- 主 API 服務移除 in-process `connector.Pool`，改為透過 Redis Streams 管理命令控制 connector 生命週期
- 新增 `protocol` 層的管理命令（ManageStartConnector、ManageStopConnector、ManageRestartConnector）
- 更新 Docker Compose 配置，新增 connector 服務

## Impact

- Affected specs: 新增 `connector-service` capability
- Affected code:
  - `cmd/connector/` — 新增獨立入口
  - `internal/connector/` — 核心邏輯不變，新增管理命令消費者（XREADGROUP）、擴展心跳 payload
  - `internal/protocol/` — 新增管理命令類型 + 管理事件回應
  - `internal/config/version.go` — 新增 `ConnectorVersion` 變數
  - `internal/gateway/connector_gateway.go` — 新增 `SendManageCommand` / `SendManageCommandAndWait`
  - `internal/gateway/event_consumer.go` — 新增 `ManageCommandAck` 事件處理
  - `internal/service/connector/connector_config_service.go` — 移除 Pool 依賴，Start/Stop/Restart 改走管理命令，GetStatus 改讀 Redis 心跳
  - `internal/app/services.go` — 移除 ConnectorPool 初始化和背景恢復任務
  - `internal/app/app.go` — 移除 ConnectorPool.StopAll()
  - `internal/metrics/collector.go` — 移除 Pool 依賴，僅保留 Redis Stream 指標
  - `internal/handler/system/monitor_handler.go` — 移除 Pool 依賴，改從 Redis 心跳讀取狀態和 EventWorkerStats
  - `docker-compose.yml` — 新增 connector 服務定義，獨立 image tag
  - `.github/workflows/deploy-api.yml` — trigger 改為 `api/v*`
  - `.github/workflows/deploy-connector.yml` — 新增 connector 部署 workflow
- NOT affected:
  - `internal/handler/system/connector_config_handler.go` — 只依賴 `ConnectorConfigService` 介面，不需改動
