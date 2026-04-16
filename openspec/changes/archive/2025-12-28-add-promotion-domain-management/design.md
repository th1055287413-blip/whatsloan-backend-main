## Context

目前系統的渠道推廣連結使用硬編碼域名 `ws.m6a2dd.com`。業務需求支援多個推廣域名，並讓每個渠道綁定特定域名。

**現有程式碼**:
- `backend/internal/model/channel.go:26-32`: `GetPromotionURL()` 使用硬編碼默認域名
- `backend/internal/service/channel_service.go:413`: 調用 `GetPromotionURL(host)` 生成連結

## Goals / Non-Goals

### Goals
- 支援多推廣域名的 CRUD 管理
- 渠道必須關聯一個推廣域名
- 推廣連結使用綁定的域名生成

### Non-Goals
- 域名 DNS 驗證（超出範圍）
- SSL 證書管理（由 Nginx 處理）

## Decisions

### 1. 資料表設計

```sql
CREATE TABLE promotion_domains (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,           -- 名稱（主站、代理站A）
    domain VARCHAR(255) NOT NULL UNIQUE,  -- 域名（ws.example.com）
    status VARCHAR(20) NOT NULL DEFAULT 'enabled',  -- enabled/disabled
    remark TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP                  -- 軟刪除
);

-- channels 表新增欄位
ALTER TABLE channels ADD COLUMN promotion_domain_id BIGINT NOT NULL REFERENCES promotion_domains(id);
```

**選擇理由**:
- 使用外鍵確保資料完整性
- 軟刪除保留歷史記錄
- `domain` 欄位唯一索引防止重複

### 2. API 設計

遵循現有 RESTful 風格：

| Method | Path | Description |
|--------|------|-------------|
| GET | /promotion-domains | 列表（支持分頁、搜索） |
| GET | /promotion-domains/:id | 詳情 |
| POST | /promotion-domains | 新增 |
| PUT | /promotion-domains/:id | 修改 |
| DELETE | /promotion-domains/:id | 刪除 |
| PUT | /promotion-domains/:id/status | 狀態切換 |

### 3. 推廣連結生成邏輯

修改 `Channel.GetPromotionURL()`:
```go
func (c *Channel) GetPromotionURL() string {
    if c.PromotionDomain != nil {
        return fmt.Sprintf("https://%s?ad=%s", c.PromotionDomain.Domain, c.ChannelCode)
    }
    return ""
}
```

### 4. 渠道 API 修改

- 創建/更新渠道時必須提供 `promotion_domain_id`
- 查詢渠道列表時 preload 關聯的域名資訊
- 刪除域名前檢查是否有渠道關聯

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| 現有渠道無域名關聯 | 遷移腳本：創建預設域名並關聯所有現有渠道 |
| 刪除域名時渠道失效 | 刪除前檢查關聯數，強制處理（轉移/禁止） |
| 域名格式錯誤 | 後端驗證域名格式（正則） |

## Migration Plan

1. 創建 `promotion_domains` 表
2. 插入預設域名記錄（使用現有硬編碼值 `ws.m6a2dd.com`）
3. `channels` 表新增 `promotion_domain_id` 欄位（nullable）
4. 更新所有現有渠道關聯預設域名
5. 修改 `promotion_domain_id` 為 NOT NULL

**Rollback**: 刪除新增欄位和表

## Open Questions

無
