## Context

Connector 目前作為 in-process 模組嵌入主 API 服務。通訊已經透過 Redis Streams 解耦（命令流 + 事件流），`cmd/mock-connector/` 也驗證了獨立運行的可行性。分離的主要工作在於：
1. 建立獨立的 Connector binary（一個進程 = 一個 Pool，管理多個 connector 實例）
2. 將主服務中直接操作 Pool 的邏輯改為透過 Redis Streams 的管理命令

### 架構概覽

```
Connector Service (1 個進程)
└── Pool
    ├── Connector "conn-1" → Manager → 多個 WhatsApp accounts
    ├── Connector "conn-2" → Manager → 多個 WhatsApp accounts
    └── Connector "conn-3" → Manager → 多個 WhatsApp accounts
```

啟動時 `Pool.RestoreAll()` 自動從 DB 恢復所有 connector 實例。API 端透過管理命令控制個別 connector 的 Start/Stop/Restart。

## Goals / Non-Goals

- Goals:
  - Connector 可獨立部署、重啟、擴展
  - 主 API 服務不再包含 whatsmeow 依賴和 WhatsApp 連線邏輯
  - 保持現有 Redis Streams 通訊協議不變
  - 零停機遷移：可以先部署獨立 Connector，再移除主服務中的 Pool

- Non-Goals:
  - 不拆分資料庫（Connector 和 API 繼續共享同一個 PostgreSQL）
  - 不引入 gRPC 或其他 RPC 框架（Redis Streams 已足夠）
  - 不改變 whatsmeow session store 的共享方式
  - 不改變前端任何邏輯

## Decisions

### 1. Connector 服務架構

**決定**: 獨立 binary + 輕量 HTTP 管理端點

Connector 服務是一個進程跑一個 `connector.Pool`，Pool 裡管理多個 connector 實例（每個對應一筆 `connector_configs` 記錄）。與現在主服務裡的架構完全相同，只是從 in-process 搬到獨立 binary。

Connector 服務包含：
- `connector.Pool` — 管理多個 Connector 實例（現有邏輯，不變）
- 管理命令 Stream 消費者 — 接收 Pool 層級的管理命令（新增）
- HTTP 端點 — 健康檢查 `/health`、Prometheus metrics `/metrics`

**替代方案**:
- 純 Redis Streams 無 HTTP：放棄，因為 K8s/Docker 健康檢查需要 HTTP
- gRPC 管理介面：過度設計，Redis Streams 已滿足需求

### 2. 管理命令協議

**決定**: 在現有 `protocol` 包中新增管理命令類型

管理命令的目標是「告訴 Connector 服務去操作它 Pool 裡的某個 connector 實例」，不是啟動/停止整個服務進程。

新增命令：
- `ManageStartConnector` — 啟動 Pool 中指定的 connector 實例
- `ManageStopConnector` — 停止 Pool 中指定的 connector 實例
- `ManageRestartConnector` — 重啟 Pool 中指定的 connector 實例

使用專用的管理命令 Stream：`connector:manage`（單一 Stream，因為只有一個 Connector 服務進程）

管理命令回應透過現有的事件 Stream 發布 `ManageCommandAck` 事件，API 端的 `EventConsumer` 收到後觸發 `NotifyCommandSuccess`，完成 request-reply 迴路。

API 端在 `ConnectorGateway` 新增 `SendManageCommand` / `SendManageCommandAndWait` 方法，發送目標為 `connector:manage` stream（而非 per-connector 業務命令 stream）。ACK 等待機制複用現有的 `commandResponses` channel map。

管理命令 Stream 使用 `XREADGROUP` + consumer group，預留未來多 Connector 實例的競爭消費能力。

**替代方案**:
- 複用現有業務命令 Stream：放棄，管理命令和業務命令語義不同，應分離
- HTTP API 直接呼叫：放棄，增加服務發現複雜度
- 每個實例獨立 Stream `connector:manage:{instanceID}`：放棄，目前只有一個 Connector 服務進程，不需要
- 獨立 ManageGateway 結構：放棄，ACK 機制與現有 Gateway 完全相同，擴展即可
- XREAD（無 consumer group）：放棄，XREADGROUP 預留多實例擴展能力

### 3. Connector 實例發現與狀態上報

**決定**: 擴展現有的 Redis heartbeat 機制

Connector 服務啟動時向 `protocol.ConnectorsSetKey` 註冊，主服務透過此 Set 發現可用的 Connector 實例。管理命令發送到管理 Stream。

心跳 payload 擴展為包含完整狀態資訊：`AccountCount`、`AccountIDs`、`Uptime`、`StartTime`、`EventWorkerStats`（queue depth）。API 端的 `ConnectorConfigService.GetConnectorStatus` 和 `MonitorHandler` 改為從 Redis 讀取心跳資料，不再依賴 in-process Pool。

### 4. 配置

**決定**: 複用現有 `config.yaml`

Connector 服務讀取同一份 `config.yaml`，只使用它需要的欄位（DB、Redis、WhatsApp、Connector）。不需要獨立配置檔。

### 5. 遷移策略

**決定**: 一步到位

直接建立獨立 Connector binary，同時移除主服務中的 in-process Pool。不做漸進式遷移，因為：
- 通訊層已完全解耦（Redis Streams）
- `mock-connector` 已驗證獨立運行可行
- 漸進式遷移增加複雜度（兩套 Pool 共存的鎖競爭問題）

### 6. 獨立版本號

**決定**: 在 `internal/config/version.go` 新增 `ConnectorVersion` 變數

API 保留現有 `Version`，Connector 使用 `ConnectorVersion`。各自的 Dockerfile 透過 ldflags 注入對應變數。開發時打開 `version.go` 即可看到兩個服務的當前版本。

### 7. CI/CD

**決定**: 各自獨立的 GitHub Actions workflow + prefixed git tags

- Tag 命名：API 用 `api/v*`，Connector 用 `connector/v*`
- 新增 `deploy-connector.yml`，trigger `connector/v*` tags
- 修改 `deploy-api.yml`，trigger 從 `v*` 改為 `api/v*`
- `docker-compose.yml` 兩個服務用各自的 image tag 環境變數（`API_TAG`、`CONNECTOR_TAG`）

**替代方案**:
- 單一 workflow 判斷 tag prefix 分流：增加 workflow 複雜度，不如各自獨立清楚

## Risks / Trade-offs

- **風險**: 管理命令丟失（Redis Stream 消費失敗）
  → 緩解: 管理命令使用 ACK 機制，API 端 `SendCommandAndWait` 有超時檢測
- **風險**: Connector 服務掛掉時帳號無法操作
  → 緩解: 現有的分散式鎖 + 心跳機制已處理此情況，帳號會在新實例啟動時自動恢復
- **Trade-off**: 共享 DB 意味著 schema 變更仍需協調
  → 可接受，短期內不需要拆分 DB
- **Trade-off**: MonitorHandler 和 BusinessCollector 分離後無法直接讀取 Pool 狀態
  → Connector 服務自己暴露 `/metrics`；MonitorHandler 改為從 Redis 讀取（heartbeat + routing 資訊已在 Redis）
