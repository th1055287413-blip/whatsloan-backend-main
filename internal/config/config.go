package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
)

// Config 應用配置結構
type Config struct {
	Server     ServerConfig
	Database   DatabaseConfig
	Redis      RedisConfig
	Version    string
	JWT        JWTConfig
	Log        LogConfig
	GoAdmin    GoAdminConfig
	WhatsApp WhatsAppConfig
	Platform PlatformConfig
	Connector  ConnectorConfig
	ServiceAPI ServiceAPIConfig
}

// ServiceAPIConfig 內部微服務 API 認證配置
type ServiceAPIConfig struct {
	Key string
}

// ConnectorConfig Connector 配置（保留結構供未來擴充）
type ConnectorConfig struct{}

// ServerConfig 伺服器配置
type ServerConfig struct {
	Port  int
	Host  string
	Debug bool
}

// DatabaseConfig 資料庫配置
type DatabaseConfig struct {
	Type       string
	PostgreSQL PostgreSQLConfig
}

// PostgreSQLConfig PostgreSQL配置
type PostgreSQLConfig struct {
	Host         string
	Port         int
	Username     string
	Password     string
	Database     string
	SSLMode      string
	Charset      string
	MaxOpenConns int
	MaxIdleConns int
}

// BuildDSN 建立資料庫連接字串
func (p *PostgreSQLConfig) BuildDSN() string {
	sslMode := p.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s options='-c search_path=public'",
		p.Host,
		p.Port,
		p.Username,
		p.Password,
		p.Database,
		sslMode,
	)
}

// RedisConfig Redis配置
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// JWTConfig JWT配置
type JWTConfig struct {
	SecretKey string
	ExpiresIn int // 小時
}

// LogConfig 日誌配置
type LogConfig struct {
	Level      string
	File       string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

// GoAdminConfig GoAdmin配置
type GoAdminConfig struct {
	Prefix           string
	Theme            string
	Title            string
	Logo             string
	MiniLogo         string
	IndexURL         string
	LoginURL         string
	Debug            bool
	Language         string
	SessionLifeTime  int
	FileUploadEngine map[string]interface{}
}

// WhatsAppConfig WhatsApp配置
type WhatsAppConfig struct {
	MediaDir string
}

// PlatformConfig 平台標識配置
type PlatformConfig struct {
	ID   string
	Name string
}

// Load 載入配置 (純環境變數)
func Load() (*Config, error) {
	// 載入 .env 檔案 (如果存在)
	// 忽略錯誤，因為 .env 檔案可能不存在（例如在 Docker 中使用環境變數）
	_ = godotenv.Load()

	config := &Config{}

	// 從環境變數載入
	loadFromEnv(config)

	// 設定預設值
	setDefaults(config)

	// 建立必要的目錄
	if err := createDirectories(config); err != nil {
		return nil, fmt.Errorf("建立目錄失敗: %v", err)
	}

	return config, nil
}

// loadFromEnv 從環境變數載入配置
func loadFromEnv(cfg *Config) {
	// Server
	cfg.Server.Port = getEnvInt("SERVER_PORT", 0)
	cfg.Server.Host = getEnv("SERVER_HOST", "")
	cfg.Server.Debug = getEnv("GIN_MODE", "") == "debug"

	// Database
	cfg.Database.Type = getEnv("DB_TYPE", "postgresql")
	cfg.Database.PostgreSQL.Host = getEnv("DB_HOST", "")
	cfg.Database.PostgreSQL.Port = getEnvInt("DB_PORT", 0)
	cfg.Database.PostgreSQL.Username = getEnv("DB_USER", "")
	cfg.Database.PostgreSQL.Password = getEnv("DB_PASSWORD", "")
	cfg.Database.PostgreSQL.Database = getEnv("DB_NAME", "")
	cfg.Database.PostgreSQL.SSLMode = getEnv("DB_SSLMODE", "")
	cfg.Database.PostgreSQL.MaxOpenConns = getEnvInt("DB_MAX_OPEN_CONNS", 0)
	cfg.Database.PostgreSQL.MaxIdleConns = getEnvInt("DB_MAX_IDLE_CONNS", 0)

	// Redis
	redisHost := getEnv("REDIS_HOST", "")
	redisPort := getEnv("REDIS_PORT", "6379")
	if redisHost != "" {
		cfg.Redis.Addr = redisHost + ":" + redisPort
	}
	cfg.Redis.Password = getEnv("REDIS_PASSWORD", "")
	cfg.Redis.DB = getEnvInt("REDIS_DB", 0)

	// JWT
	cfg.JWT.SecretKey = getEnv("JWT_SECRET", "")
	cfg.JWT.ExpiresIn = getEnvInt("JWT_EXPIRES_IN", 0)

	// Log
	cfg.Log.Level = getEnv("LOG_LEVEL", "")
	cfg.Log.File = getEnv("LOG_FILE", "")
	cfg.Log.MaxSize = getEnvInt("LOG_MAX_SIZE", 0)
	cfg.Log.MaxBackups = getEnvInt("LOG_MAX_BACKUPS", 0)
	cfg.Log.MaxAge = getEnvInt("LOG_MAX_AGE", 0)
	cfg.Log.Compress = getEnv("LOG_COMPRESS", "") == "true"

	// WhatsApp
	cfg.WhatsApp.MediaDir = getEnv("WHATSAPP_MEDIA_DIR", "")

	// ServiceAPI
	cfg.ServiceAPI.Key = getEnv("SERVICE_API_KEY", "")
}

// setDefaults 設定預設值
func setDefaults(cfg *Config) {
	// Server defaults
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}

	// Database defaults
	if cfg.Database.Type == "" {
		cfg.Database.Type = "postgresql"
	}
	if cfg.Database.PostgreSQL.Host == "" {
		cfg.Database.PostgreSQL.Host = "localhost"
	}
	if cfg.Database.PostgreSQL.Port == 0 {
		cfg.Database.PostgreSQL.Port = 5432
	}
	if cfg.Database.PostgreSQL.SSLMode == "" {
		cfg.Database.PostgreSQL.SSLMode = "disable"
	}
	if cfg.Database.PostgreSQL.MaxOpenConns == 0 {
		cfg.Database.PostgreSQL.MaxOpenConns = 50
	}
	if cfg.Database.PostgreSQL.MaxIdleConns == 0 {
		cfg.Database.PostgreSQL.MaxIdleConns = 20
	}

	// Redis defaults
	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = "localhost:6379"
	}

	// JWT defaults
	if cfg.JWT.ExpiresIn == 0 {
		cfg.JWT.ExpiresIn = 24
	}

	// Log defaults
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.File == "" {
		cfg.Log.File = "logs/app.log"
	}
	if cfg.Log.MaxSize == 0 {
		cfg.Log.MaxSize = 100
	}
	if cfg.Log.MaxBackups == 0 {
		cfg.Log.MaxBackups = 10
	}
	if cfg.Log.MaxAge == 0 {
		cfg.Log.MaxAge = 30
	}

	// WhatsApp defaults
	if cfg.WhatsApp.MediaDir == "" {
		cfg.WhatsApp.MediaDir = "uploads"
	}
	// Platform defaults
	if cfg.Platform.ID == "" {
		cfg.Platform.ID = "default"
	}
	if cfg.Platform.Name == "" {
		cfg.Platform.Name = "WhatsApp Manager"
	}

}

// getEnv 取得環境變數，若無則返回預設值
func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// getEnvInt 取得整數環境變數
func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

// createDirectories 建立必要的目錄
func createDirectories(config *Config) error {
	dirs := []string{
		filepath.Dir(config.Log.File),
		config.WhatsApp.MediaDir,
		"uploads",
	}

	for _, dir := range dirs {
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetDSN 取得資料庫連接字串
func (c *Config) GetDSN() string {
	sslMode := c.Database.PostgreSQL.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s options='-c search_path=public'",
		c.Database.PostgreSQL.Host,
		c.Database.PostgreSQL.Port,
		c.Database.PostgreSQL.Username,
		c.Database.PostgreSQL.Password,
		c.Database.PostgreSQL.Database,
		sslMode,
	)
}
