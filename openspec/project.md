# Project Context

## Purpose

WhatsApp 管理系統 - 一個多租戶 SaaS 平台，用於管理 WhatsApp Business 帳戶、聯繫人、群組和消息。提供用戶管理、批量發送、消息分析和審計日誌功能。

## Tech Stack

### Backend

- **Language**: Go 1.24.0
- **Web Framework**: Gin 1.9.1
- **ORM**: GORM 1.25.10
- **Database**: PostgreSQL
- **Cache/Session**: Redis 8.11.5
- **Authentication**: JWT (dgrijalva/jwt-go)
- **Real-time**: Gorilla WebSocket 1.5.3
- **Logging**: Uber Zap 1.26.0
- **WhatsApp SDK**: go.mau.fi/whatsmeow
- **Cron Jobs**: robfig/cron 3.0.1

### Frontend

- **Framework**: Vue 3.4.0 (Composition API)
- **Language**: TypeScript 5.3.3
- **UI Library**: Element Plus 2.4.4
- **Build Tool**: Vite 5.0.10
- **State Management**: Pinia 2.1.7
- **Routing**: Vue Router 4.2.5
- **HTTP Client**: Axios 1.6.2
- **Charts**: ECharts 5.4.3
- **CSS**: Sass/SCSS

### Infrastructure

- **Container**: Docker + Docker Compose
- **Reverse Proxy**: Nginx

## Project Conventions

### Code Style

**Go:**

- 使用 `gofmt` 格式化
- Handler + Service 分層設計
- GORM 模型使用 JSON 和 GORM 標籤
- 錯誤處理優先，盡早返回

**TypeScript/Vue:**

- Prettier: 100 字符寬度, 2 空格縮進, 單引號, 無分號
- ESLint: Vue 3 essential + TypeScript strict
- Composition API + `<script setup>` 語法
- 類型定義放在 `/src/types/` 目錄

### Architecture Patterns

**Backend:**

```
backend/
├── cmd/api/main.go          # 入口點
├── internal/
│   ├── app/                  # 應用初始化
│   ├── config/               # 配置管理
│   ├── database/             # 資料庫連接
│   ├── handler/              # HTTP 處理器 (按功能模塊分組)
│   ├── middleware/           # HTTP 中間件
│   ├── model/                # GORM 實體
│   ├── service/              # 業務邏輯層
│   ├── logger/               # 日誌配置
│   └── queue/                # 消息隊列
└── config/config.yaml        # 應用配置
```

**Frontend:**

```
frontend/src/
├── api/                      # API 客戶端模塊
├── views/                    # 頁面組件
├── components/               # 可複用組件
├── stores/                   # Pinia 狀態管理
├── router/                   # 路由配置
├── types/                    # TypeScript 類型
├── utils/                    # 工具函數
├── composables/              # Vue 可組合函數
└── styles/                   # 全局樣式
```

### Testing Strategy

- **Go**: `go test -v -race -coverprofile` 執行測試
- **Frontend**: 尚未配置完整測試框架
- **E2E**: 未配置

### Git Workflow

- 主分支: `main`
- 功能分支命名: `feature/xxx`, `fix/xxx`, `epic/xxx`
- Commit 訊息使用中文或英文皆可

## Domain Context

### 核心概念

- **WhatsApp Account**: 連接到系統的 WhatsApp Business 帳戶
- **Contact**: 與 WhatsApp 帳戶關聯的聯繫人
- **Chat**: 對話會話，包含消息歷史
- **Tag**: 用於分類帳戶和聯繫人的標籤
- **Batch Send**: 批量發送消息功能
- **RBAC**: 角色基礎的訪問控制系統

### 多租戶模型

系統支持多個獨立的用戶/組織，每個用戶管理自己的 WhatsApp 帳戶和數據。

## Important Constraints

### 安全性

- JWT Token 24 小時過期
- 密碼使用 bcrypt 哈希
- CORS 保護啟用
- SQL 注入防護 (GORM 參數化查詢)

### 性能

- Redis 用於會話和緩存
- Docker 容器化部署

### 合規性

- 操作審計日誌
- 敏感詞過濾功能

## External Dependencies

### 必要服務

- **PostgreSQL**: 主資料庫
- **Redis**: 會話管理和緩存

### 可選服務

- **OpenRouter API**: AI 翻譯服務

### WhatsApp Integration

- 使用 `whatsmeow` 庫連接 WhatsApp
- 需要有效的 WhatsApp Business 帳戶
