package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// ReferralSessionInfo 推荐码会话信息
type ReferralSessionInfo struct {
	ReferralCode      string `json:"referral_code"`
	SourceAccountID   uint   `json:"source_account_id"`
	OperatorAdminID   *uint  `json:"operator_admin_id,omitempty"`
	PromotionDomainID *uint  `json:"promotion_domain_id,omitempty"`
	SourceKey         string `json:"source_key,omitempty"`
	SourceAgentID     *uint  `json:"source_agent_id,omitempty"`
}

// ReferralSessionService 推荐码会话服务
type ReferralSessionService interface {
	// StoreReferralSession 存储推荐码会话信息
	StoreReferralSession(ctx context.Context, sessionID string, info *ReferralSessionInfo) error
	// GetReferralSession 获取推荐码会话信息
	GetReferralSession(ctx context.Context, sessionID string) (*ReferralSessionInfo, error)
	// DeleteReferralSession 删除推荐码会话信息
	DeleteReferralSession(ctx context.Context, sessionID string) error
}

type referralSessionService struct {
	redis *redis.Client
}

// NewReferralSessionService 创建推荐码会话服务
func NewReferralSessionService(redisClient *redis.Client) ReferralSessionService {
	return &referralSessionService{
		redis: redisClient,
	}
}

// StoreReferralSession 存储推荐码会话信息
func (s *referralSessionService) StoreReferralSession(ctx context.Context, sessionID string, info *ReferralSessionInfo) error {
	key := fmt.Sprintf("pairing:%s:referral", sessionID)

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal referral session info failed: %w", err)
	}

	// 设置 1 小时过期
	err = s.redis.Set(ctx, key, data, time.Hour).Err()
	if err != nil {
		return fmt.Errorf("store referral session failed: %w", err)
	}

	return nil
}

// GetReferralSession 获取推荐码会话信息
func (s *referralSessionService) GetReferralSession(ctx context.Context, sessionID string) (*ReferralSessionInfo, error) {
	key := fmt.Sprintf("pairing:%s:referral", sessionID)

	data, err := s.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // 会话不存在
	}
	if err != nil {
		return nil, fmt.Errorf("get referral session failed: %w", err)
	}

	var info ReferralSessionInfo
	err = json.Unmarshal([]byte(data), &info)
	if err != nil {
		return nil, fmt.Errorf("unmarshal referral session info failed: %w", err)
	}

	return &info, nil
}

// DeleteReferralSession 删除推荐码会话信息
func (s *referralSessionService) DeleteReferralSession(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf("pairing:%s:referral", sessionID)
	return s.redis.Del(ctx, key).Err()
}
