## 1. 資料庫

- [x] 1.1 建立 `promotion_domains` 資料表
- [x] 1.2 `channels` 表新增 `promotion_domain_id` 欄位
- [x] 1.3 建立遷移腳本：插入預設域名並關聯現有渠道

## 2. 後端 - 推廣域名模組

- [x] 2.1 建立 `model/promotion_domain.go` 模型和 DTO
- [x] 2.2 建立 `service/promotion_domain_service.go` 服務層
- [x] 2.3 建立 `handler/system/promotion_domain_handler.go` 處理器
- [x] 2.4 註冊路由 `/promotion-domains`

## 3. 後端 - 渠道模組修改

- [x] 3.1 修改 `model/channel.go` 新增 PromotionDomainID 欄位和關聯
- [x] 3.2 修改 `Channel.GetPromotionURL()` 使用綁定域名
- [x] 3.3 修改 `channel_service.go` 的 CreateChannel/UpdateChannel 處理域名關聯
- [x] 3.4 修改 GetChannelList 預載入域名資訊

## 4. 前端 - 推廣域名頁面

- [x] 4.1 建立 `api/promotion-domain.ts` API 模組
- [x] 4.2 建立 `views/promotion-domain/PromotionDomainManagement.vue` 頁面
- [x] 4.3 新增路由配置

## 5. 前端 - 渠道頁面修改

- [x] 5.1 修改 `api/channel.ts` 新增域名相關類型
- [x] 5.2 修改 `ChannelManagement.vue` 表單新增域名選擇器
- [x] 5.3 修改渠道列表顯示綁定的域名資訊

## 6. 驗證

- [x] 6.1 測試推廣域名 CRUD 功能
- [x] 6.2 測試渠道綁定域名功能
- [x] 6.3 測試推廣連結生成正確性
- [x] 6.4 測試刪除域名時的關聯檢查
