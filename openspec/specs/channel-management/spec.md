# channel-management Specification

## Purpose
TBD - created by archiving change add-promotion-domain-management. Update Purpose after archive.
## Requirements
### Requirement: Channel Promotion Domain Association
渠道 SHALL 必須關聯一個推廣域名。

#### Scenario: 建立渠道時指定推廣域名
- **WHEN** 管理員建立新渠道
- **THEN** 必須選擇一個推廣域名
- **AND** 渠道記錄包含 promotion_domain_id 外鍵

#### Scenario: 建立渠道未指定推廣域名
- **WHEN** 管理員嘗試建立渠道但未提供 promotion_domain_id
- **THEN** 系統返回錯誤訊息「請選擇推廣域名」

#### Scenario: 編輯渠道更換推廣域名
- **WHEN** 管理員編輯渠道並更換推廣域名
- **THEN** 渠道的推廣連結使用新域名生成

---

### Requirement: Channel Promotion URL from Domain
渠道的推廣連結 SHALL 使用綁定的推廣域名生成。

#### Scenario: 生成推廣連結
- **WHEN** 查詢渠道列表或詳情
- **THEN** 推廣連結格式為 `https://{promotion_domain.domain}?ad={channel_code}`
- **AND** 推廣連結使用渠道綁定的域名

#### Scenario: 域名被禁用時
- **WHEN** 渠道綁定的推廣域名狀態為 disabled
- **THEN** 推廣連結仍正常生成
- **AND** 前端顯示域名狀態標記

---

### Requirement: Channel Form Domain Selector
渠道管理頁面 SHALL 提供推廣域名選擇功能。

#### Scenario: 新增渠道表單
- **WHEN** 使用者開啟新增渠道彈窗
- **THEN** 表單包含推廣域名下拉選擇框
- **AND** 下拉框為必填項
- **AND** 只顯示 status 為 enabled 的域名

#### Scenario: 編輯渠道表單
- **WHEN** 使用者開啟編輯渠道彈窗
- **THEN** 推廣域名下拉框顯示當前綁定的域名
- **AND** 可以更換為其他域名

#### Scenario: 渠道列表顯示域名資訊
- **WHEN** 使用者查看渠道列表
- **THEN** 推廣連結欄位顯示完整的 URL
- **AND** 可識別使用的是哪個推廣域名

