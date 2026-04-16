# 工作組前端（Agent 端）— 開發需求文件

## 業務背景

這是一個**獨立的前端應用**，給外部業務團隊使用（不是後台管理員）。

### 角色關係

```
公司後台 Admin（管理員）
  │
  ├── 建立「工作組」（例如：台北團隊、高雄團隊）
  ├── 將 WhatsApp 帳號分配到工作組
  └── 建立「組長」帳號
         │
         ▼
┌──────────────────────────────────────────────┐
│  本前端的範圍                                   │
│                                                │
│  組長（Leader）                                  │
│    ├── 管理組員：建立/編輯/刪除組員帳號            │
│    ├── 分配帳號：把工作組的帳號分配給具體組員        │
│    └── 操作帳號：看對話、發訊息（看全組帳號）        │
│                                                │
│  組員（Member）                                  │
│    └── 操作帳號：看對話、發訊息（只看分配給自己的）   │
└──────────────────────────────────────────────┘
```

### 關鍵差異

| | 組長 (Leader) | 組員 (Member) |
|---|---|---|
| 管理組員 | 可以 | 不可以 |
| 看到的帳號 | 工作組內全部帳號 | 只有分配給自己的 |
| 分配帳號給組員 | 可以 | 不可以 |
| 發送訊息 | 可以 | 可以 |
| 看客戶資料 | 工作組內全部 | 只有分配到的帳號對應的 |

> 後端已自動根據角色做權限隔離，前端只需要根據 `role` 決定是否顯示「組員管理」相關功能。

---

## 認證系統

Agent 認證與 Admin 後台**完全獨立**，使用不同的 JWT 和 API 路徑。

### 登入

**API**: `POST /api/agent/auth/login`（公開，不需 token）

登入需要三個欄位：**工作組代碼** + **帳號** + **密碼**。

> 同一個 username（如 `agent001`）可以存在於不同工作組，透過工作組代碼區分。
> 工作組代碼由 Admin 建立工作組時設定，組長會告知組員。

Request:
```json
{
  "workgroup_code": "taipei",
  "username": "leader01",
  "password": "abc123456"
}
```

Response:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIs...",
    "agent": {
      "id": 1,
      "username": "leader01",
      "workgroup_id": 1,
      "workgroup_code": "taipei",
      "workgroup_name": "台北團隊",
      "role": "leader",
      "status": "active"
    }
  }
}
```

- 登入頁面有 3 個輸入框：工作組代碼、帳號、密碼
- 登入後，將 `token` 存入 localStorage
- 後續所有 API 請求帶 `Authorization: Bearer <token>`
- `role` 欄位決定 UI 要不要顯示組長功能

### 錯誤處理

| code | 說明 | 建議處理 |
|------|------|---------|
| 40101 | Token 無效/過期 | 跳轉登入頁 |
| 40001 | 工作組代碼錯誤 / 帳號或密碼錯誤 | 顯示錯誤提示（message 欄位有具體原因） |

### 登出

**API**: `POST /api/agent/auth/logout`

### 取得個人資料

**API**: `GET /api/agent/auth/profile`

Response:
```json
{
  "code": 0,
  "data": {
    "id": 1,
    "username": "leader01",
    "workgroup_id": 1,
    "role": "leader",
    "status": "active"
  }
}
```

> 注意：profile 不帶 `workgroup_code`/`workgroup_name`，這些只在登入回應中提供。前端可在登入時快取。

### 修改密碼

**API**: `POST /api/agent/auth/change-password`

Request:
```json
{
  "old_password": "abc123456",
  "new_password": "newpwd789"
}
```
- `new_password` 最少 6 字元

---

## 頁面規劃

### 所有角色共用

| 頁面 | 路徑建議 | 說明 |
|------|---------|------|
| 登入 | `/login` | 帳號密碼登入 |
| 我的帳號 | `/accounts` | 分配給我的 WhatsApp 帳號列表 |
| 對話列表 | `/accounts/:id/chats` | 某帳號下的所有對話 |
| 訊息詳情 | `/accounts/:id/chats/:jid` | 某對話的訊息列表 + 發送訊息 |
| 客戶資料 | `/user-data` | 可存取的客戶資料列表 |
| 個人設定 | `/profile` | 改密碼 |

### 組長 (Leader) 獨有

| 頁面 | 路徑建議 | 說明 |
|------|---------|------|
| 組員管理 | `/members` | 建立/編輯/刪除組員 |
| 組員帳號分配 | `/members/:id/accounts` | 將帳號分配給組員 |

> 前端根據登入時的 `role === "leader"` 決定是否在側邊欄顯示「組員管理」。

---

## API 詳細規格

### 通用說明

**Base URL**: `/api/agent`

**認證**: 所有 API（除 login）都需要 `Authorization: Bearer <token>`

**分頁參數**（query string）:
| 參數 | 預設值 | 說明 |
|------|--------|------|
| `page` | 1 | 頁碼 |
| `page_size` | 20 | 每頁筆數 |

**統一回應格式**:

成功:
```json
{ "code": 0, "message": "success", "data": { ... } }
```

分頁:
```json
{
  "code": 0,
  "data": {
    "list": [...],
    "total": 100,
    "page": 1,
    "page_size": 20,
    "total_pages": 5
  }
}
```

建立成功 (HTTP 201):
```json
{ "code": 0, "data": { "id": 1, ... } }
```

刪除成功 (HTTP 204): 無回應 body

錯誤:
```json
{ "code": 40001, "message": "錯誤訊息" }
```

---

### 1. 我的帳號列表

**API**: `GET /api/agent/accounts?page=1&page_size=20`

> 後端自動根據角色返回不同範圍：Leader 看全組、Member 看自己的。

Response:
```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 42,
        "phone_number": "886912345678",
        "push_name": "John",
        "status": "connected",
        "avatar": "https://...",
        "is_online": true,
        "last_seen": "2026-02-12T08:00:00Z",
        "message_count": 1500,
        "created_at": "2026-01-01T00:00:00Z"
      }
    ],
    "total": 10,
    "page": 1,
    "page_size": 20,
    "total_pages": 1
  }
}
```

**表格/卡片欄位建議**:
| 欄位 | 來源 | 說明 |
|------|------|------|
| 頭像 | `avatar` | 顯示圖片 |
| 暱稱 | `push_name` | 主要顯示名稱 |
| 電話 | `phone_number` | |
| 狀態 | `status` | `connected`=綠點, `disconnected`=灰點 |
| 訊息數 | `message_count` | |

### 2. 帳號詳情

**API**: `GET /api/agent/accounts/:id`

Response: 同上帳號物件，完整欄位。

### 3. 對話列表

**API**: `GET /api/agent/accounts/:id/chats?page=1&page_size=20`

Response:
```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 100,
        "account_id": 42,
        "jid": "886987654321@s.whatsapp.net",
        "name": "李先生",
        "avatar": "https://...",
        "last_message": "你好，我想了解貸款方案",
        "last_time": "2026-02-12T09:30:00Z",
        "unread_count": 3,
        "is_group": false,
        "archived": false
      }
    ],
    "total": 50,
    "page": 1,
    "page_size": 20,
    "total_pages": 3
  }
}
```

**列表顯示建議**:
| 欄位 | 來源 | 說明 |
|------|------|------|
| 頭像 | `avatar` | |
| 名稱 | `name` | 若空則顯示 `jid` 裡的電話號碼 |
| 最後訊息 | `last_message` | 截斷顯示 |
| 時間 | `last_time` | 相對時間（剛剛、5分鐘前、昨天） |
| 未讀 | `unread_count` | Badge |
| 群組 | `is_group` | 群組圖示 |

### 4. 對話訊息

**API**: `GET /api/agent/accounts/:id/chats/:jid/messages?page=1&page_size=50`

> `:jid` 範例: `886987654321@s.whatsapp.net`（需 URL encode `@` → `%40`）

Response:
```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 5000,
        "account_id": 42,
        "chat_id": 100,
        "message_id": "3EB0A1B2C3D4...",
        "from_jid": "886912345678@s.whatsapp.net",
        "to_jid": "886987654321@s.whatsapp.net",
        "content": "你好，我想了解貸款方案",
        "type": "text",
        "media_url": "",
        "timestamp": "2026-02-12T09:30:00Z",
        "is_from_me": false,
        "is_read": true,
        "send_status": "read",
        "is_revoked": false,
        "is_edited": false,
        "sent_by_admin_id": null,
        "sender_type": ""
      },
      {
        "id": 5001,
        "content": "",
        "type": "image",
        "media_url": "/media/image/abc123.jpg",
        "timestamp": "2026-02-12T09:31:00Z",
        "is_from_me": true,
        "sender_type": "agent"
      }
    ],
    "total": 200,
    "page": 1,
    "page_size": 50,
    "total_pages": 4
  }
}
```

**訊息類型 (`type`)**:
| type | 說明 | 顯示方式 |
|------|------|---------|
| `text` | 文字 | 直接顯示 `content` |
| `image` | 圖片 | 顯示 `media_url` 圖片 |
| `video` | 影片 | 影片播放器 |
| `audio` | 音訊 | 音訊播放器 |
| `document` | 文件 | 下載連結 |
| `sticker` | 貼圖 | 顯示圖片 |

**訊息方向**: `is_from_me === true` → 右側氣泡（己方發送），`false` → 左側氣泡

**發送者標記**:
- `sender_type === "admin"` → 顯示「管理員發送」標記
- `sender_type === "agent"` → 顯示「業務員發送」標記
- 空值 → 用戶自己發送或對方發送

### 5. 發送訊息

**API**: `POST /api/agent/accounts/:id/send`

#### 文字訊息
```json
{
  "to_jid": "886987654321@s.whatsapp.net",
  "content": "你好，這是回覆訊息"
}
```

#### 圖片訊息
```json
{
  "to_jid": "886987654321@s.whatsapp.net",
  "media_type": "image",
  "media_url": "/media/image/abc123.jpg",
  "caption": "這是圖片說明"
}
```

#### 影片/音訊/文件
```json
{
  "to_jid": "886987654321@s.whatsapp.net",
  "media_type": "document",
  "media_url": "/media/document/contract.pdf",
  "file_name": "合約書.pdf"
}
```

| 欄位 | 必填 | 說明 |
|------|------|------|
| `to_jid` | 是 | 接收者 JID（從對話的 `jid` 取得） |
| `content` | 文字訊息必填 | 訊息內容 |
| `media_type` | 媒體訊息必填 | `image`/`video`/`audio`/`document` |
| `media_url` | 媒體訊息必填 | 媒體檔案 URL |
| `caption` | 否 | 圖片/影片說明文字 |
| `file_name` | 否 | 文件名稱（document 類型建議填寫） |

> **媒體上傳**：目前媒體上傳 API 在 Admin 認證下（`/api/media/upload/*`）。如果 Agent 端也需要上傳媒體再發送，後續需另開 Agent 的上傳 API。目前可先用 URL 方式發送。

Response:
```json
{ "code": 0, "message": "success", "data": null }
```

---

### 6. 客戶資料

#### 客戶列表

**API**: `GET /api/agent/user-data?page=1&page_size=20`

Response:
```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 1,
        "phone": "886987654321",
        "basic_info": {
          "name": "李先生",
          "idNumber": "A123456789",
          "education": "bachelor",
          "maritalStatus": "married",
          "monthlyIncome": "50k-100k",
          "occupation": "上班族"
        },
        "house_info": {
          "hasHouse": "yes",
          "propertyType": "full",
          "area": "range70to90",
          "purchasePrice": "range2m3m",
          "currentValue": "range2m3m",
          "loanStatus": "hasLoan",
          "remainingLoan": "range500k1m"
        },
        "credit_card_info": { ... },
        "car_info": { ... },
        "bank_info": { ... },
        "shop_info": null,
        "created_at": "2026-01-15T00:00:00Z",
        "updated_at": "2026-02-10T00:00:00Z"
      }
    ],
    "total": 30,
    "page": 1,
    "page_size": 20,
    "total_pages": 2
  }
}
```

> 只回傳 Agent 可存取帳號對應電話號碼的客戶資料。

#### 單一客戶

**API**: `GET /api/agent/user-data/:phone`

Response: 同上單一物件。

**客戶資料子區塊**:

| 區塊 | 欄位 | 中文 |
|------|------|------|
| **basic_info** | `name` | 姓名 |
| | `idNumber` | 身份證號 |
| | `education` | 學歷 (`college`/`bachelor`/`master`/`doctor`) |
| | `maritalStatus` | 婚姻 (`single`/`married`/`divorced`) |
| | `monthlyIncome` | 月收入 (`below30k`/`30k-50k`/`50k-100k`/`above100k`) |
| | `occupation` | 職業 |
| **house_info** | `hasHouse` | 有無房產 (`yes`/`no`) |
| | `propertyType` | 產權類型 |
| | `area` | 面積 |
| | `purchasePrice` | 購入價格 |
| | `currentValue` | 現值 |
| | `loanStatus` | 貸款狀態 |
| | `remainingLoan` | 剩餘貸款 |
| **credit_card_info** | `hasCreditCard` | 有無信用卡 |
| | `creditLimit` | 額度 |
| | `cardUsageDuration` | 用卡時長 |
| | `repaymentRecord` | 還款記錄 |
| **car_info** | `hasVehicle` | 有無車輛 |
| | `brand` / `model` | 品牌/型號 |
| | `purchasePrice` / `currentValue` | 價格/現值 |
| **bank_info** | `bankName` | 銀行名稱 |
| | `accountNumber` | 帳號 |

---

### 7. 組員管理（僅 Leader）

以下 API 只有 `role === "leader"` 的帳號能呼叫。如果 Member 呼叫會回傳 403。

#### 組員列表

**API**: `GET /api/agent/members`

Response:
```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 5,
        "username": "member01",
        "workgroup_id": 1,
        "role": "member",
        "status": "active",
        "last_login_at": "2026-02-12T08:00:00Z",
        "last_login_ip": "1.2.3.4",
        "created_at": "2026-02-11T10:00:00Z"
      }
    ],
    "total": 5
  }
}
```

> 不分頁，直接回傳全部組員（一般不會太多）。

#### 建立組員

**API**: `POST /api/agent/members`

Request:
```json
{
  "username": "member02",
  "password": "abc123456"
}
```

| 欄位 | 必填 | 驗證 | 說明 |
|------|------|------|------|
| `username` | 是 | 同工作組內唯一 | 登入帳號 |
| `password` | 是 | 最少 6 字元 | 初始密碼 |

> 建立的帳號自動歸屬組長的工作組，角色固定為 `member`。

#### 編輯組員

**API**: `PUT /api/agent/members/:id`

Request:
```json
{
  "status": "inactive"
}
```

- 所有欄位選填，只傳要改的
- `status`: `active`（啟用）/ `inactive`（停用）
- 不可改 `username`

#### 刪除組員

**API**: `DELETE /api/agent/members/:id`

> 刪除後，該組員被分配的帳號會自動解除分配。

#### 重置組員密碼

**API**: `POST /api/agent/members/:id/reset-password`

Request:
```json
{
  "new_password": "newpwd123"
}
```

#### 組員帳號列表（查看某組員的帳號）

**API**: `GET /api/agent/members/:id/accounts`

Response:
```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 42,
        "phone_number": "886912345678",
        "push_name": "John",
        "status": "connected"
      }
    ],
    "total": 3
  }
}
```

#### 分配帳號給組員

**API**: `POST /api/agent/members/:id/accounts`

Request:
```json
{
  "account_ids": [42, 43, 44]
}
```

> 只能分配工作組內的帳號。已分配給其他組員的帳號會被重新分配。

#### 移除組員帳號

**API**: `DELETE /api/agent/members/:id/accounts`

Request:
```json
{
  "account_ids": [42]
}
```

---

## 組員帳號分配的 UI 建議

在組員管理頁面，點擊某個組員 → 進入「帳號分配」頁：

1. **已分配的帳號**（上方表格）
   - 來源：`GET /api/agent/members/:id/accounts`
   - 操作：移除（`DELETE /api/agent/members/:id/accounts`）

2. **工作組內可分配的帳號**（下方或 Modal）
   - 來源：`GET /api/agent/accounts`（組長看到全組帳號）
   - 前端過濾掉已分配給該組員的帳號
   - 勾選後分配：`POST /api/agent/members/:id/accounts`

---

## 前端路由與角色判斷

```
/login                              → 公開
/accounts                           → 需登入（所有角色）
/accounts/:id/chats                 → 需登入
/accounts/:id/chats/:jid            → 需登入
/user-data                          → 需登入
/profile                            → 需登入
/members                            → 需登入 + role === "leader"
/members/:id/accounts               → 需登入 + role === "leader"
```

建議在路由守衛中：
1. 沒 token → 跳 `/login`
2. 有 token 但 API 回 401 → 清 token，跳 `/login`
3. 有 token 且 `role !== "leader"` → 訪問 `/members*` 時跳轉回 `/accounts`
