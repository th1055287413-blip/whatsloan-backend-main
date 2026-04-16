# Change: 新增推廣域名管理功能

## Why

目前渠道的推廣連結使用硬編碼的域名（`ws.m6a2dd.com`），無法靈活配置多個推廣域名。業務需求：
1. 支援管理多個推廣域名（主站、代理站等）
2. 每個渠道必須綁定一個推廣域名
3. 推廣連結應使用渠道綁定的域名生成

## What Changes

### 後端
- **ADDED**: 新增 `promotion_domains` 資料表，儲存推廣域名資訊
- **MODIFIED**: `channels` 表新增 `promotion_domain_id` 外鍵欄位（必填）
- **ADDED**: 新增推廣域名 CRUD API 端點
- **MODIFIED**: 渠道 API 需處理域名關聯

### 前端
- **ADDED**: 新增推廣域名管理頁面（CRUD 操作）
- **MODIFIED**: 渠道管理頁面新增域名選擇下拉框（必選）
- **MODIFIED**: 渠道列表的推廣連結使用綁定的域名顯示

## Impact

- Affected specs: `promotion-domain-management` (new), `channel-management` (modified)
- Affected code:
  - Backend: `internal/model/`, `internal/handler/system/`, `internal/service/`
  - Frontend: `src/views/channel/`, `src/api/`, `src/views/` (new page)
- **BREAKING**: 現有渠道需要遷移綁定預設域名
