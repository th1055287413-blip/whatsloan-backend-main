package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"whatsapp_golang/internal/config"
	"whatsapp_golang/internal/model"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-redis/redis/v8"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AgentJWTClaims Agent JWT 聲明
type AgentJWTClaims struct {
	AgentID     uint   `json:"agent_id"`
	Username    string `json:"username"`
	WorkgroupID uint   `json:"workgroup_id"`
	Role        string `json:"role"`
	jwt.StandardClaims
}

// AgentLoginResult 登入結果
type AgentLoginResult struct {
	Agent     *model.Agent
	Workgroup *model.Workgroup
	Token     string
}

// AgentAuthService Agent 認證服務
type AgentAuthService interface {
	Login(workgroupCode, username, password, ip, userAgent string) (*AgentLoginResult, error)
	GetAgentByToken(token string) (*model.Agent, error)
	Logout(token string) error
	ChangePassword(agentID uint, oldPwd, newPwd string) error
}

type agentAuthService struct {
	db     *gorm.DB
	redis  *redis.Client
	config *config.Config
}

// NewAgentAuthService 建立 Agent 認證服務
func NewAgentAuthService(db *gorm.DB, rdb *redis.Client, cfg *config.Config) AgentAuthService {
	return &agentAuthService{
		db:     db,
		redis:  rdb,
		config: cfg,
	}
}

func (s *agentAuthService) Login(workgroupCode, username, password, ip, userAgent string) (*AgentLoginResult, error) {
	// 根據工作組代碼找到工作組
	var wg model.Workgroup
	if err := s.db.Where("code = ?", workgroupCode).First(&wg).Error; err != nil {
		return nil, errors.New("工作組代碼錯誤")
	}

	var agent model.Agent
	if err := s.db.Where("workgroup_id = ? AND username = ? AND deleted_at IS NULL", wg.ID, username).First(&agent).Error; err != nil {
		return nil, errors.New("帳號或密碼錯誤")
	}

	if !agent.IsActive() {
		return nil, errors.New("帳號已被停用")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(agent.Password), []byte(password)); err != nil {
		return nil, errors.New("帳號或密碼錯誤")
	}

	token, err := s.generateToken(&agent)
	if err != nil {
		return nil, fmt.Errorf("生成令牌失敗: %v", err)
	}

	// 更新登入資訊
	now := time.Now()
	s.db.Model(&agent).Updates(map[string]interface{}{
		"last_login_at": &now,
		"last_login_ip": ip,
	})

	// 存 Redis session
	session := &model.AgentSession{
		AgentID:     agent.ID,
		Username:    agent.Username,
		WorkgroupID: agent.WorkgroupID,
		Role:        agent.Role,
		LoginIP:     ip,
		UserAgent:   userAgent,
	}
	sessionData, _ := json.Marshal(session)
	sessionKey := fmt.Sprintf("agent_session:%s", token)
	s.redis.Set(context.Background(), sessionKey, sessionData, time.Duration(s.config.JWT.ExpiresIn)*time.Hour)

	return &AgentLoginResult{Agent: &agent, Workgroup: &wg, Token: token}, nil
}

func (s *agentAuthService) GetAgentByToken(token string) (*model.Agent, error) {
	claims, err := s.parseToken(token)
	if err != nil {
		return nil, errors.New("無效的令牌")
	}

	// 檢查 Redis session
	sessionKey := fmt.Sprintf("agent_session:%s", token)
	exists := s.redis.Exists(context.Background(), sessionKey).Val()
	if exists == 0 {
		return nil, errors.New("會話已過期")
	}

	var agent model.Agent
	if err := s.db.First(&agent, claims.AgentID).Error; err != nil {
		return nil, errors.New("Agent 不存在")
	}

	if !agent.IsActive() {
		return nil, errors.New("帳號已被停用")
	}

	return &agent, nil
}

func (s *agentAuthService) Logout(token string) error {
	sessionKey := fmt.Sprintf("agent_session:%s", token)
	return s.redis.Del(context.Background(), sessionKey).Err()
}

func (s *agentAuthService) ChangePassword(agentID uint, oldPwd, newPwd string) error {
	var agent model.Agent
	if err := s.db.First(&agent, agentID).Error; err != nil {
		return errors.New("Agent 不存在")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(agent.Password), []byte(oldPwd)); err != nil {
		return errors.New("原密碼錯誤")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPwd), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("密碼加密失敗: %v", err)
	}

	return s.db.Model(&agent).Update("password", string(hashed)).Error
}

func (s *agentAuthService) generateToken(agent *model.Agent) (string, error) {
	claims := &AgentJWTClaims{
		AgentID:     agent.ID,
		Username:    agent.Username,
		WorkgroupID: agent.WorkgroupID,
		Role:        agent.Role,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: time.Now().Add(time.Duration(s.config.JWT.ExpiresIn) * time.Hour).Unix(),
			IssuedAt:  time.Now().Unix(),
			Issuer:    "whatsapp-agent",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.config.JWT.SecretKey))
}

func (s *agentAuthService) parseToken(tokenString string) (*AgentJWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AgentJWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.config.JWT.SecretKey), nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*AgentJWTClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("無效的令牌")
}
