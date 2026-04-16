<!-- OPENSPEC:START -->
# OpenSpec Instructions

These instructions are for AI assistants working in this project.

Always open `@/openspec/AGENTS.md` when the request:
- Mentions planning or proposals (words like proposal, spec, change, plan)
- Introduces new capabilities, breaking changes, architecture shifts, or big performance/security work
- Sounds ambiguous and you need the authoritative spec before coding

Use `@/openspec/AGENTS.md` to learn:
- How to create and apply change proposals
- Spec format and conventions
- Project structure and guidelines

Keep this managed block so 'openspec update' can refresh the instructions.

<!-- OPENSPEC:END -->

## Go Build 規範

檢查 Go 編譯時，使用以下命令避免產生二進制檔案：

```bash
go build -o /dev/null ./cmd/api/...
```

不要使用 `go build ./cmd/api/...`，這會在當前目錄產生 `api` 執行檔。

## Seed 資料 (`internal/seeds/init.json`)

新增或修改 `system_configs`、角色、權限、AI 標籤定義等初始資料時，**必須同步更新 `internal/seeds/init.json`**。此檔案透過 `go:embed` 嵌入，在首次部署時 seed 到資料庫。

- `init.json` 中的 `system_configs` 項目必須與 `internal/database/database.go` 的 `initSystemConfigs()` 保持一致
- 兩處都是 "if not exists then create" 邏輯，不會覆蓋已存在的值

## WhatsApp 帳號安全規則

- **絕對不可變更** `whatsapp_accounts.connector_id`：每個帳號綁定特定 Connector（因 proxy IP 不同），變更會導致 IP 切換觸發風控
- **絕對不可執行任何導致客戶需要重新配對裝置的操作**：包括刪除 whatsmeow session store 中的 device、清除加密金鑰等
- 帳號從記憶體中消失時（Connector 重啟等），應從 session store 自動恢復連線，而非要求重新配對