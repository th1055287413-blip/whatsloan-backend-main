package config

import (
	"os"
	"sync"

	"github.com/joho/godotenv"
)

// 版本資訊
// 發版時更新 Version，或使用 ldflags 注入：
// go build -ldflags "-X whatsapp_golang/internal/config.Version=1.2.3"
var (
	Version          = "0.38.0" // 版本號（發版時更新此值）
	ConnectorVersion = "0.3.3"  // Connector 獨立版本號
	GitCommit        = "unknown" // Git commit hash（編譯時注入）
	BuildTime        = "unknown" // 編譯時間（編譯時注入）
)

var envOnce sync.Once

// ensureEnvLoaded 確保 .env 已載入
func ensureEnvLoaded() {
	envOnce.Do(func() {
		_ = godotenv.Load()
	})
}

// GetVersion 取得版本號（環境變數可覆蓋）
func GetVersion() string {
	ensureEnvLoaded()
	if v := os.Getenv("APP_VERSION"); v != "" {
		return v
	}
	return Version
}

// GetConnectorVersion 取得 Connector 版本號（環境變數可覆蓋）
func GetConnectorVersion() string {
	ensureEnvLoaded()
	if v := os.Getenv("CONNECTOR_VERSION"); v != "" {
		return v
	}
	return ConnectorVersion
}

// GetBuildInfo 取得完整編譯資訊
func GetBuildInfo() map[string]string {
	return map[string]string{
		"version":           GetVersion(),
		"connector_version": GetConnectorVersion(),
		"git_commit":        GitCommit,
		"build_time":        BuildTime,
	}
}
