package whatsapp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// ReferralCodeGenerator 推荐码生成器
type ReferralCodeGenerator struct{}

// NewReferralCodeGenerator 创建推荐码生成器实例
func NewReferralCodeGenerator() *ReferralCodeGenerator {
	return &ReferralCodeGenerator{}
}

// GenerateCode 生成固定唯一的推荐码
// 使用 SHA256(account_id + timestamp + random_salt) 生成
func (g *ReferralCodeGenerator) GenerateCode(accountID uint) (string, error) {
	// 生成随机盐
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// 组合数据
	data := fmt.Sprintf("%d-%d-%s", accountID, time.Now().UnixNano(), base64.StdEncoding.EncodeToString(randomBytes))

	// SHA256 哈希
	hash := sha256.Sum256([]byte(data))
	encoded := base64.URLEncoding.EncodeToString(hash[:])

	// 清理特殊字符，只保留字母和数字
	cleaned := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, encoded)

	// 取前10位并转大写
	if len(cleaned) < 10 {
		return "", fmt.Errorf("generated code too short")
	}

	code := strings.ToUpper(cleaned[:10])
	return code, nil
}
