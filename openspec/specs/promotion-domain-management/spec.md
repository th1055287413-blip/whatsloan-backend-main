# promotion-domain-management Specification

## Purpose
TBD - created by archiving change add-promotion-domain-management. Update Purpose after archive.
## Requirements
### Requirement: Promotion Domain Data Model
系統 SHALL 提供推廣域名資料模型，包含以下欄位：
- `id`: 主鍵
- `name`: 域名名稱（如：主站、代理站A），最大 100 字元
- `domain`: 域名（如：ws.example.com），唯一且必填
- `status`: 狀態（enabled/disabled），預設 enabled
- `remark`: 備註說明
- `created_at`: 建立時間
- `updated_at`: 更新時間
- `deleted_at`: 軟刪除時間

#### Scenario: 建立推廣域名記錄
- **WHEN** 管理員提供有效的域名資訊
- **THEN** 系統建立新的推廣域名記錄
- **AND** 自動設定 created_at 和 updated_at

#### Scenario: 域名唯一性驗證
- **WHEN** 嘗試建立已存在的域名
- **THEN** 系統返回錯誤訊息「域名已存在」

---

### Requirement: Promotion Domain CRUD API
系統 SHALL 提供推廣域名的完整 CRUD API 端點。

#### Scenario: 取得推廣域名列表
- **WHEN** 發送 GET /promotion-domains 請求
- **THEN** 返回分頁的推廣域名列表
- **AND** 支援 keyword 搜索（名稱、域名）
- **AND** 支援 status 篩選

#### Scenario: 取得推廣域名詳情
- **WHEN** 發送 GET /promotion-domains/:id 請求
- **THEN** 返回指定域名的詳細資訊

#### Scenario: 新增推廣域名
- **WHEN** 發送 POST /promotion-domains 請求，包含 name 和 domain
- **THEN** 建立新的推廣域名並返回詳情
- **AND** domain 欄位自動移除 https:// 前綴（如有）

#### Scenario: 修改推廣域名
- **WHEN** 發送 PUT /promotion-domains/:id 請求
- **THEN** 更新指定域名的資訊
- **AND** 更新 updated_at 時間戳

#### Scenario: 刪除推廣域名
- **WHEN** 發送 DELETE /promotion-domains/:id 請求
- **AND** 該域名沒有關聯的渠道
- **THEN** 軟刪除該域名

#### Scenario: 刪除有關聯渠道的域名
- **WHEN** 發送 DELETE /promotion-domains/:id 請求
- **AND** 該域名有關聯的渠道
- **THEN** 返回錯誤訊息「該域名下還有 N 個渠道，請先處理」

---

### Requirement: Promotion Domain Status Management
系統 SHALL 支援推廣域名的狀態管理。

#### Scenario: 啟用推廣域名
- **WHEN** 發送 PUT /promotion-domains/:id/status 請求，status 為 enabled
- **THEN** 域名狀態變更為 enabled

#### Scenario: 禁用推廣域名
- **WHEN** 發送 PUT /promotion-domains/:id/status 請求，status 為 disabled
- **THEN** 域名狀態變更為 disabled
- **AND** 關聯渠道的推廣連結仍可正常生成

---

### Requirement: Promotion Domain Management Page
系統 SHALL 提供推廣域名管理的前端頁面。

#### Scenario: 顯示域名列表
- **WHEN** 使用者進入推廣域名管理頁面
- **THEN** 顯示所有域名的列表
- **AND** 顯示每個域名關聯的渠道數量

#### Scenario: 新增域名
- **WHEN** 使用者點擊「新增域名」按鈕
- **THEN** 顯示新增表單彈窗
- **AND** 表單包含名稱、域名、備註欄位

#### Scenario: 編輯域名
- **WHEN** 使用者點擊域名列表中的「編輯」按鈕
- **THEN** 顯示編輯表單彈窗並載入現有資料

#### Scenario: 刪除域名
- **WHEN** 使用者點擊域名列表中的「刪除」按鈕
- **AND** 域名有關聯渠道
- **THEN** 顯示警告訊息並阻止刪除

#### Scenario: 搜索和篩選
- **WHEN** 使用者輸入搜索關鍵字或選擇狀態篩選
- **THEN** 列表即時過濾顯示匹配結果

