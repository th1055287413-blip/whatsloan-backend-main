# 工作組功能 — Admin 前端頁面需求

## 業務背景

目前系統裡有大量 WhatsApp 帳號，每個帳號對應一個真實的 WhatsApp 號碼，帳號下會有很多聊天對話和客戶資料（user_data）。

現在需要讓**外部業務團隊**也能操作這些帳號（發訊息、看對話、看客戶資料），但他們不是後台管理員，不能用 Admin 帳號登入。

**解決方案：工作組機制**

```
Admin（後台管理員）
  │
  ├── 1. 建立「工作組」（例如：台北團隊、高雄團隊）
  │
  ├── 2. 從 WhatsApp 帳號池中，依客戶資料篩選帳號
  │      （例如：職業=上班族、月收入>30000 的帳號）
  │      → 批量分配到工作組
  │
  └── 3. 建立「組長」帳號，分配到工作組
         │
         └── 組長（Leader）登入獨立前端
               │
               ├── 建立「組員」帳號
               ├── 將工作組內的帳號分配給具體組員
               │
               └── 組員（Member）登入同一個前端
                     │
                     └── 操作分配給自己的帳號
                           ├── 看對話列表
                           ├── 看訊息內容
                           ├── 發送訊息
                           └── 看客戶資料
```

### 關鍵規則

- **1 帳號 → 1 工作組**：一個 WhatsApp 帳號只能分配給一個工作組，不能跨組共享
- **組長看全組、組員看自己的**：組長能看到工作組內所有帳號，組員只能看到分配給自己的
- **Admin 只管工作組和組長**：組員由組長自行管理，Admin 不需要介入日常操作
- **業務員系統完全獨立**：業務員有獨立的登入系統（`/api/agent/auth/login`），與 Admin 帳號互不影響

### Admin 需要做的事（= 這份文件涵蓋的範圍）

| 步驟 | 操作 | 對應頁面 |
|------|------|---------|
| 1 | 建立工作組 | 工作組列表 |
| 2 | 篩選帳號並分配到工作組 | 工作組詳情 |
| 3 | 建立組長帳號 | 業務員管理 |

> 組長/組員的前端是另一個獨立項目，不在此文件範圍內。

---

## 側邊欄

在 Admin 後台側邊欄新增 **「工作組管理」** 選單分組：

```
工作組管理
├── 工作組列表    /workgroups
└── 業務員管理    /agents
```

---

## 頁面 1：工作組列表

**路徑**: `/workgroups`
**權限**: `workgroup:read`（頁面可見）、`workgroup:write`（新增/編輯）、`workgroup:delete`（刪除）

### 列表

**API**: `GET /api/admin/workgroups?page=1&page_size=20&keyword=&status=`

**篩選欄位**:
| 參數 | 類型 | 說明 |
|------|------|------|
| `keyword` | string | 搜尋名稱/描述 |
| `status` | string | `active` / `disabled` |

**回應**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "list": [
      {
        "id": 1,
        "code": "taipei",
        "name": "台北團隊",
        "description": "負責台北區域客戶",
        "status": "active",
        "created_by": 1,
        "created_at": "2026-02-12T10:00:00Z",
        "updated_at": "2026-02-12T10:00:00Z"
      }
    ],
    "total": 50,
    "page": 1,
    "page_size": 20,
    "total_pages": 3
  }
}
```

**表格欄位建議**:
| 欄位 | 來源 | 說明 |
|------|------|------|
| 代碼 | `code` | Agent 登入時使用 |
| 名稱 | `name` | |
| 描述 | `description` | 可截斷顯示 |
| 狀態 | `status` | Tag: active=綠, disabled=灰 |
| 建立時間 | `created_at` | |
| 操作 | — | 詳情、編輯、刪除 |

### 新增/編輯 工作組（Modal 或 Drawer）

**新增 API**: `POST /api/admin/workgroups`
**權限**: `workgroup:write`

```json
{
  "code": "taipei",
  "name": "台北團隊",
  "description": "負責台北區域客戶"
}
```
- `code`: 必填，全域唯一，業務員登入時需要輸入此代碼
- `name`: 必填

**編輯 API**: `PUT /api/admin/workgroups/:id`
**權限**: `workgroup:write`

```json
{
  "code": "taipei-new",
  "name": "台北團隊（已更名）",
  "description": "更新描述",
  "status": "disabled"
}
```
- 所有欄位選填，只傳要改的
- 修改 `code` 後，業務員需使用新代碼登入

### 刪除

**API**: `DELETE /api/admin/workgroups/:id`
**權限**: `workgroup:delete`

> 如果工作組下還有帳號或業務員，API 會回傳錯誤，需先移除。前端可顯示對應提示。

---

## 頁面 2：工作組詳情（帳號分配）

**路徑**: `/workgroups/:id`
**權限**: `workgroup:read` + `workgroup_account:read`（帳號列表）、`workgroup_account:write`（分配/移除）

從工作組列表點「詳情」進入。

### 上方：工作組基本資訊

**API**: `GET /api/admin/workgroups/:id`

```json
{
  "code": 0,
  "data": {
    "id": 1,
    "code": "taipei",
    "name": "台北團隊",
    "description": "負責台北區域客戶",
    "status": "active",
    "created_by": 1,
    "created_at": "2026-02-12T10:00:00Z"
  }
}
```

### 已分配帳號列表

**API**: `GET /api/admin/workgroups/:id/accounts?page=1&page_size=20`

```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 10,
        "workgroup_id": 1,
        "account_id": 42,
        "assigned_agent_id": 5,
        "assigned_by": 1,
        "assigned_at": "2026-02-12T10:00:00Z",
        "phone_number": "886912345678",
        "push_name": "John",
        "account_status": "connected",
        "agent_name": "王小明"
      }
    ],
    "total": 30,
    "page": 1,
    "page_size": 20,
    "total_pages": 2
  }
}
```

**表格欄位**:
| 欄位 | 來源 | 說明 |
|------|------|------|
| 帳號 ID | `account_id` | |
| 電話號碼 | `phone_number` | |
| 暱稱 | `push_name` | |
| 帳號狀態 | `account_status` | connected/disconnected 等 |
| 分配給 | `agent_name` | 顯示業務員姓名，空值表示未分配給具體業務員 |
| 分配時間 | `assigned_at` | |
| 操作 | — | 移除 |

### 分配帳號（Modal）

點「分配帳號」按鈕 → 開啟 Modal，內含：

#### 1. 篩選條件

**API**: `GET /api/admin/accounts/assignable?page=1&page_size=20&keyword=&occupation=&education=&monthlyIncome=&maritalStatus=`

| 參數 | 類型 | 說明 | 範例值 |
|------|------|------|--------|
| `keyword` | string | 搜尋電話/暱稱 | |
| `occupation` | string | 職業（user_data.basic_info 欄位） | 上班族、自僱人士... |
| `education` | string | 學歷 | 大學、碩士... |
| `monthlyIncome` | string | 月收入 | 30000以下、30000-50000... |
| `maritalStatus` | string | 婚姻狀態 | 未婚、已婚... |

> 篩選值的選項取決於 user_data 裡實際的資料，可先 hardcode 常見值或後續加 API 取得可用選項。

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
    "total": 100,
    "page": 1,
    "page_size": 20,
    "total_pages": 5
  }
}
```

> 此 API 只回傳**尚未分配給任何工作組**的帳號。

#### 2. 表格（勾選多筆）

使用者勾選要分配的帳號，點確認後：

**API**: `POST /api/admin/workgroups/:id/accounts`

```json
{
  "account_ids": [42, 43, 44, 45]
}
```

> 一個帳號只能屬於一個工作組。如果帳號已被其他工作組分配，API 會回傳錯誤。

### 移除帳號

**API**: `DELETE /api/admin/workgroups/:id/accounts`

```json
{
  "account_ids": [42, 43]
}
```

> 支援批量移除（表格勾選多筆），也可單筆操作。

---

## 頁面 3：業務員管理

**路徑**: `/agents`
**權限**: `agent:read`（頁面可見）、`agent:write`（新增/編輯/重置密碼）、`agent:delete`（刪除）

### 列表

**API**: `GET /api/admin/agents?page=1&page_size=20&workgroup_id=&role=&status=&keyword=`

**篩選欄位**:
| 參數 | 類型 | 說明 |
|------|------|------|
| `keyword` | string | 搜尋帳號 |
| `workgroup_id` | uint | 所屬工作組（下拉選單，用工作組列表 API 取得） |
| `role` | string | `leader` / `member` |
| `status` | string | `active` / `inactive` |

**回應**:
```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 1,
        "username": "leader01",
        "workgroup_id": 1,
        "role": "leader",
        "status": "active",
        "last_login_at": "2026-02-12T08:00:00Z",
        "last_login_ip": "1.2.3.4",
        "created_at": "2026-02-10T10:00:00Z"
      }
    ],
    "total": 20,
    "page": 1,
    "page_size": 20,
    "total_pages": 1
  }
}
```

> 注意：`password` 欄位不會出現在回應中（後端 `json:"-"`）。

**表格欄位**:
| 欄位 | 來源 | 說明 |
|------|------|------|
| 帳號 | `username` | |
| 所屬工作組 | `workgroup_id` | 前端需對應顯示工作組名稱 |
| 角色 | `role` | Tag: leader=藍, member=綠 |
| 狀態 | `status` | Tag: active=綠, inactive=灰 |
| 最後登入 | `last_login_at` | 可顯示 IP: `last_login_ip` |
| 操作 | — | 編輯、重置密碼、刪除 |

### 新增業務員（Modal）

**API**: `POST /api/admin/agents`
**權限**: `agent:write`

```json
{
  "username": "leader01",
  "password": "abc123456",
  "workgroup_id": 1,
  "role": "leader"
}
```

| 欄位 | 必填 | 驗證規則 | 說明 |
|------|------|---------|------|
| `username` | 是 | 同工作組內唯一 | 登入帳號（不同工作組可以相同） |
| `password` | 是 | 最少 6 字元 | 初始密碼 |
| `workgroup_id` | 是 | | 下拉選單（取工作組列表） |
| `role` | 是 | `leader` 或 `member` | 角色 |

> Admin 通常只建 leader，leader 之後在自己的前端管理組員。但 Admin 也能直接建 member。

### 編輯業務員（Modal）

**API**: `PUT /api/admin/agents/:id`
**權限**: `agent:write`

```json
{
  "status": "inactive",
  "role": "member"
}
```

> 所有欄位選填。**不可改** `username` 和 `workgroup_id`（如需移到其他工作組，需刪除重建）。

### 重置密碼（確認彈窗）

**API**: `POST /api/admin/agents/:id/reset-password`
**權限**: `agent:write`

```json
{
  "new_password": "newpwd123"
}
```

> 彈出輸入框讓 Admin 輸入新密碼，最少 6 字元。

### 刪除

**API**: `DELETE /api/admin/agents/:id`
**權限**: `agent:delete`

> 刪除業務員會同時清除該業務員被分配的帳號關聯（workgroup_accounts 的 assigned_agent_id 清空）。

---

## 權限對應表

以下權限已加入後端 seed，需在角色管理頁面分配給對應角色：

| 權限代碼 | 資源 | 動作 | 用途 |
|---------|------|------|------|
| `workgroup:read` | workgroup | read | 查看工作組列表/詳情 |
| `workgroup:write` | workgroup | write | 新增/編輯工作組 |
| `workgroup:delete` | workgroup | delete | 刪除工作組 |
| `workgroup_account:read` | workgroup_account | read | 查看工作組帳號列表 + 可分配帳號 |
| `workgroup_account:write` | workgroup_account | write | 分配/移除帳號 |
| `agent:read` | agent | read | 查看業務員列表/詳情 |
| `agent:write` | agent | write | 新增/編輯/重置密碼 |
| `agent:delete` | agent | delete | 刪除業務員 |

> `super_admin` 角色自動擁有所有權限（`*`），不需額外配置。

---

## 統一回應格式

所有 API 回應格式一致：

**成功**:
```json
{ "code": 0, "message": "success", "data": { ... } }
```

**分頁**:
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

**建立成功** (HTTP 201):
```json
{ "code": 0, "message": "success", "data": { "id": 1, ... } }
```

**刪除成功** (HTTP 204): 無回應 body

**錯誤**:
```json
{ "code": 40001, "message": "工作組下有 5 個帳號，請先移除" }
```

---

## 認證

所有 Admin API 使用現有的 Admin JWT 認證（`Authorization: Bearer <token>`），與現有後台一致，不需額外處理。
