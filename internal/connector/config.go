package connector

import (
	"fmt"
	"time"
)

// Config Connector 配置（由 Pool 建立並注入）
type Config struct {
	ConnectorID       string
	MediaDir          string
	HeartbeatInterval time.Duration
	Version           string
}

// Validate 驗證配置
func (c *Config) Validate() error {
	if c.ConnectorID == "" {
		return fmt.Errorf("ConnectorID 不能為空")
	}
	if c.MediaDir == "" {
		return fmt.Errorf("MediaDir 不能為空")
	}
	if c.HeartbeatInterval < 5*time.Second {
		return fmt.Errorf("HeartbeatInterval 不能小於 5 秒")
	}
	return nil
}
