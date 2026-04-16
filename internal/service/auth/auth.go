package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"whatsapp_golang/internal/seeds"
	"whatsapp_golang/internal/config"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-redis/redis/v8"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// SeedData 種子資料結構
type SeedData struct {
	Roles             []SeedRole             `json:"roles"`
	Permissions       []SeedPermission       `json:"permissions"`
	RolePermissions   map[string][]string    `json:"role_permissions"`
	AdminUser         SeedAdminUser          `json:"admin_user"`
	SensitiveWords    []SeedSensitiveWord    `json:"sensitive_words"`
	AiTagDefinitions  []SeedAiTagDefinition  `json:"ai_tag_definitions"`
	SystemConfigs     []SeedSystemConfig     `json:"system_configs"`
	AutoReplyKeywords []SeedAutoReplyKeyword `json:"auto_reply_keywords"`
}

type SeedAiTagDefinition struct {
	Category    string `json:"category"`
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

type SeedSystemConfig struct {
	ConfigKey   string `json:"config_key"`
	ConfigValue string `json:"config_value"`
	Description string `json:"description"`
	IsSecret    bool   `json:"is_secret"`
}

type SeedRole struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	IsSystem    bool   `json:"is_system"`
	SortOrder   int    `json:"sort_order"`
}

type SeedPermission struct {
	Name        string `json:"name"`
	Code        string `json:"code"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Module      string `json:"module"`
	Description string `json:"description"`
}

type SeedAdminUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
	RealName string `json:"real_name"`
	Role     string `json:"role"`
}

type SeedSensitiveWord struct {
	Word        string  `json:"word"`
	MatchType   string  `json:"match_type"`
	Category    string  `json:"category"`
	Enabled     bool    `json:"enabled"`
	Priority    int     `json:"priority"`
	Description string  `json:"description"`
	ReplaceText *string `json:"replace_text"`
}

type SeedAutoReplyKeyword struct {
	Keywords    []string `json:"keywords"`
	Reply       string   `json:"reply"`
	Priority    int      `json:"priority"`
	MatchType   string   `json:"match_type"`
	Status      string   `json:"status"`
	Language    string   `json:"language"`
	KeywordType string   `json:"keyword_type"`
}

// AuthService 认证服务接口
type AuthService interface {
	Login(username, password, ip, userAgent string) (*model.AdminUser, string, error)
	GetUserByToken(token string) (*model.AdminUser, error)
	RefreshToken(token string) (string, error)
	Logout(token string) error
	GetUserByID(id uint) (*model.AdminUser, error)
	CreateUser(user *model.AdminUser) error
	UpdateUser(id uint, updates map[string]interface{}) error
	ChangePassword(userID uint, oldPassword, newPassword string) error

	// 后管用户管理
	GetAdminUserList(page, limit int, filters map[string]interface{}) ([]model.AdminUser, int64, error)
	GetAdminUserByID(id uint) (*model.AdminUser, error)
	CreateAdminUser(user *model.AdminUser) error
	UpdateAdminUser(id uint, updates map[string]interface{}) error
	DeleteAdminUser(id uint) error

	// 后管用户今日统计
	GetAdminTodayStats(adminID uint) (*model.AdminTodayStats, error)
}

// authService 认证服务实现
type authService struct {
	db     *gorm.DB
	redis  *redis.Client
	config *config.Config
}

// JWTClaims JWT声明
type JWTClaims struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.StandardClaims
}

// NewAuthService 创建认证服务
func NewAuthService(db *gorm.DB, rdb *redis.Client, cfg *config.Config) (AuthService, error) {
	// 自动迁移管理员相关表
	err := db.AutoMigrate(
		&model.AdminUser{},
		&model.Permission{},
		&model.Role{},
		&model.RolePermission{},
		&model.AdminRole{},
	)
	if err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %v", err)
	}

	// 載入種子資料
	seed, err := loadSeedData()
	if err != nil {
		return nil, fmt.Errorf("載入種子資料失敗: %v", err)
	}

	// 初始化默认角色和权限
	if err := initDefaultRolesAndPermissions(db, seed); err != nil {
		return nil, fmt.Errorf("初始化默认角色和权限失败: %v", err)
	}

	// 创建默认管理员用户（首次初始化时同时分配角色）
	if err := createDefaultAdmin(db, seed); err != nil {
		return nil, fmt.Errorf("创建默认管理员失败: %v", err)
	}

	// 初始化默認敏感詞
	if err := initDefaultSensitiveWords(db, seed); err != nil {
		return nil, fmt.Errorf("初始化默認敏感詞失敗: %v", err)
	}

	// 初始化 AI 標籤定義
	if err := initDefaultAiTagDefinitions(db, seed); err != nil {
		return nil, fmt.Errorf("初始化 AI 標籤定義失敗: %v", err)
	}

	// 初始化系統配置
	if err := initDefaultSystemConfigs(db, seed); err != nil {
		return nil, fmt.Errorf("初始化系統配置失敗: %v", err)
	}

	// 初始化自動回復關鍵詞
	if err := initDefaultAutoReplyKeywords(db, seed); err != nil {
		return nil, fmt.Errorf("初始化自動回復關鍵詞失敗: %v", err)
	}

	// 初始化預設管理員工作組
	if err := initDefaultAdminWorkgroup(db); err != nil {
		return nil, fmt.Errorf("初始化預設管理員工作組失敗: %v", err)
	}

	return &authService{
		db:     db,
		redis:  rdb,
		config: cfg,
	}, nil
}

// loadSeedData 載入種子資料（優先使用嵌入數據，開發時可覆蓋）
func loadSeedData() (*SeedData, error) {
	var data []byte
	var source string

	// 先嘗試從文件系統讀取（方便開發時覆蓋）
	paths := []string{
		"db/seeds/init.json",
		"./db/seeds/init.json",
		"../db/seeds/init.json",
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		paths = append(paths, filepath.Join(exeDir, "db/seeds/init.json"))
	}

	for _, path := range paths {
		if fileData, err := os.ReadFile(path); err == nil {
			data = fileData
			source = path
			break
		}
	}

	// 若文件系統找不到，使用嵌入的數據
	if data == nil {
		data = seeds.InitJSON
		source = "embedded"
	}

	var seed SeedData
	if err := json.Unmarshal(data, &seed); err != nil {
		return nil, fmt.Errorf("解析種子資料失敗: %v", err)
	}

	logger.Infow("種子資料已載入", "source", source, "permission_count", len(seed.Permissions))

	return &seed, nil
}

// createDefaultAdmin 创建默认管理员用户（仅首次初始化时执行）
func createDefaultAdmin(db *gorm.DB, seed *SeedData) error {
	var count int64
	db.Model(&model.AdminUser{}).Count(&count)

	if count == 0 {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(seed.AdminUser.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		admin := &model.AdminUser{
			Username: seed.AdminUser.Username,
			Password: string(hashedPassword),
			Salt:     "default_salt",
			RealName: seed.AdminUser.RealName,
			Status:   "active",
		}

		if err := db.Create(admin).Error; err != nil {
			return err
		}

		// 查找指定角色並分配給 admin
		var role model.Role
		if err := db.Where("name = ?", seed.AdminUser.Role).First(&role).Error; err != nil {
			return fmt.Errorf("查找角色 %s 失敗: %v", seed.AdminUser.Role, err)
		}

		adminRole := &model.AdminRole{
			AdminID:   admin.ID,
			RoleID:    role.ID,
			IsPrimary: true,
		}
		if err := db.Create(adminRole).Error; err != nil {
			return fmt.Errorf("分配角色失敗: %v", err)
		}
	}

	return nil
}

// initDefaultRolesAndPermissions 初始化默认角色和权限（增量同步）
func initDefaultRolesAndPermissions(db *gorm.DB, seed *SeedData) error {
	// 同步角色（有則跳過，無則創建）
	roleMap := make(map[string]uint)
	for _, r := range seed.Roles {
		var role model.Role
		err := db.Where("name = ?", r.Name).First(&role).Error
		if err == gorm.ErrRecordNotFound {
			role = model.Role{
				Name:        r.Name,
				DisplayName: r.DisplayName,
				Description: r.Description,
				IsSystem:    r.IsSystem,
				Status:      "active",
				SortOrder:   r.SortOrder,
			}
			if err := db.Create(&role).Error; err != nil {
				return fmt.Errorf("創建角色 %s 失敗: %v", r.Name, err)
			}
		} else if err != nil {
			return fmt.Errorf("查詢角色 %s 失敗: %v", r.Name, err)
		}
		roleMap[r.Name] = role.ID
	}

	// 同步權限（根據 code 判斷，有則跳過，無則創建）
	var createdCount int
	for _, p := range seed.Permissions {
		var existing model.Permission
		err := db.Where("code = ?", p.Code).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			perm := &model.Permission{
				Name:        p.Name,
				Code:        p.Code,
				Resource:    p.Resource,
				Action:      p.Action,
				Module:      p.Module,
				Description: p.Description,
			}
			if err := db.Create(perm).Error; err != nil {
				return fmt.Errorf("創建權限 %s 失敗: %v", p.Code, err)
			}
			logger.Infow("新增權限", "code", p.Code)
			createdCount++
		} else if err != nil {
			return fmt.Errorf("查詢權限 %s 失敗: %v", p.Code, err)
		}
	}
	logger.Infow("權限同步完成", "total", len(seed.Permissions), "created", createdCount)

	// 同步角色權限
	for roleName, permCodes := range seed.RolePermissions {
		roleID, ok := roleMap[roleName]
		if !ok {
			// 嘗試從 DB 查詢（可能是之前就存在的角色）
			var role model.Role
			if err := db.Where("name = ?", roleName).First(&role).Error; err != nil {
				return fmt.Errorf("角色 %s 不存在", roleName)
			}
			roleID = role.ID
		}

		// "*" 表示所有權限
		if len(permCodes) == 1 && permCodes[0] == "*" {
			var permissions []model.Permission
			if err := db.Find(&permissions).Error; err != nil {
				return fmt.Errorf("查詢權限失敗: %v", err)
			}
			for _, perm := range permissions {
				// 檢查是否已分配
				var count int64
				db.Model(&model.RolePermission{}).
					Where("role_id = ? AND permission_id = ?", roleID, perm.ID).
					Count(&count)
				if count == 0 {
					rp := &model.RolePermission{
						RoleID:       roleID,
						PermissionID: perm.ID,
					}
					if err := db.Create(rp).Error; err != nil {
						return fmt.Errorf("分配權限失敗: %v", err)
					}
				}
			}
		} else {
			for _, code := range permCodes {
				var perm model.Permission
				if err := db.Where("code = ?", code).First(&perm).Error; err != nil {
					return fmt.Errorf("權限 %s 不存在: %v", code, err)
				}
				// 檢查是否已分配
				var count int64
				db.Model(&model.RolePermission{}).
					Where("role_id = ? AND permission_id = ?", roleID, perm.ID).
					Count(&count)
				if count == 0 {
					rp := &model.RolePermission{
						RoleID:       roleID,
						PermissionID: perm.ID,
					}
					if err := db.Create(rp).Error; err != nil {
						return fmt.Errorf("分配權限失敗: %v", err)
					}
				}
			}
		}
	}

	return nil
}

// initDefaultSensitiveWords 初始化默認敏感詞
func initDefaultSensitiveWords(db *gorm.DB, seed *SeedData) error {
	if len(seed.SensitiveWords) == 0 {
		return nil
	}

	for _, sw := range seed.SensitiveWords {
		// 檢查是否已存在相同的敏感詞
		var count int64
		db.Model(&model.SensitiveWord{}).Where("word = ?", sw.Word).Count(&count)
		if count > 0 {
			continue
		}

		word := &model.SensitiveWord{
			Word:        sw.Word,
			MatchType:   sw.MatchType,
			Category:    sw.Category,
			Enabled:     sw.Enabled,
			Priority:    sw.Priority,
			Description: sw.Description,
			ReplaceText: sw.ReplaceText,
			CreatedBy:   "system",
		}
		if err := db.Create(word).Error; err != nil {
			return fmt.Errorf("創建敏感詞失敗: %v", err)
		}
	}

	return nil
}

// initDefaultAiTagDefinitions 初始化 AI 標籤定義
func initDefaultAiTagDefinitions(db *gorm.DB, seed *SeedData) error {
	if len(seed.AiTagDefinitions) == 0 {
		return nil
	}

	var count int64
	db.Model(&model.AiTagDefinition{}).Count(&count)
	if count > 0 {
		return nil
	}

	for _, d := range seed.AiTagDefinitions {
		def := &model.AiTagDefinition{
			Category:    d.Category,
			Key:         d.Key,
			Label:       d.Label,
			Description: d.Description,
			Enabled:     true,
			SortOrder:   d.SortOrder,
		}
		if err := db.Create(def).Error; err != nil {
			return fmt.Errorf("創建 AI 標籤定義失敗: %v", err)
		}
	}

	return nil
}

func initDefaultSystemConfigs(db *gorm.DB, seed *SeedData) error {
	for _, sc := range seed.SystemConfigs {
		var count int64
		db.Model(&model.SystemConfig{}).Where("config_key = ?", sc.ConfigKey).Count(&count)
		if count == 0 {
			cfg := &model.SystemConfig{
				ConfigKey:   sc.ConfigKey,
				ConfigValue: sc.ConfigValue,
				Description: sc.Description,
				IsSecret:    sc.IsSecret,
			}
			if err := db.Create(cfg).Error; err != nil {
				logger.Errorw("創建系統設定失敗", "config_key", sc.ConfigKey, "error", err)
			} else {
				logger.Infow("創建系統設定", "config_key", sc.ConfigKey)
			}
		} else if sc.IsSecret {
			db.Model(&model.SystemConfig{}).Where("config_key = ?", sc.ConfigKey).Update("is_secret", true)
		}
	}
	return nil
}

// initDefaultAutoReplyKeywords 初始化默認自動回復關鍵詞
func initDefaultAutoReplyKeywords(db *gorm.DB, seed *SeedData) error {
	if len(seed.AutoReplyKeywords) == 0 {
		return nil
	}

	var count int64
	db.Model(&model.AutoReplyKeyword{}).Count(&count)

	// 只在表為空時初始化
	if count > 0 {
		logger.Info("自動回覆關鍵詞已存在，跳過初始化")
		return nil
	}

	for _, kw := range seed.AutoReplyKeywords {
		keyword := &model.AutoReplyKeyword{
			Keywords:    model.AutoReplyKeywordList(kw.Keywords),
			Reply:       kw.Reply,
			Priority:    kw.Priority,
			MatchType:   model.AutoReplyMatchType(kw.MatchType),
			Status:      model.AutoReplyStatus(kw.Status),
			Language:    model.AutoReplyLanguage(kw.Language),
			KeywordType: model.AutoReplyKeywordType(kw.KeywordType),
		}
		if err := db.Create(keyword).Error; err != nil {
			logger.Errorw("創建自動回覆關鍵詞失敗", "keyword_type", kw.KeywordType, "language", kw.Language, "error", err)
		} else {
			logger.Infow("創建自動回覆關鍵詞", "keyword_type", kw.KeywordType, "language", kw.Language)
		}
	}

	logger.Infow("自動回覆關鍵詞初始化完成", "count", len(seed.AutoReplyKeywords))
	return nil
}

// initDefaultAdminWorkgroup 初始化預設管理員工作組
func initDefaultAdminWorkgroup(db *gorm.DB) error {
	var count int64
	db.Model(&model.Workgroup{}).Where("code = ?", model.WorkgroupCodeAdmin).Count(&count)
	if count > 0 {
		return nil
	}

	var admin model.AdminUser
	if err := db.First(&admin).Error; err != nil {
		return fmt.Errorf("找不到管理員用戶: %v", err)
	}

	wg := &model.Workgroup{
		Code:      model.WorkgroupCodeAdmin,
		Name:      model.WorkgroupNameAdmin,
		Type:      model.WorkgroupTypeAdmin,
		Status:    model.WorkgroupStatusActive,
		CreatedBy: admin.ID,
	}
	if err := db.Create(wg).Error; err != nil {
		return fmt.Errorf("建立預設管理員工作組失敗: %v", err)
	}

	logger.Infow("預設管理員工作組已建立", "id", wg.ID)
	return nil
}

// Login 用户登录
func (s *authService) Login(username, password, ip, userAgent string) (*model.AdminUser, string, error) {
	// 查找用户
	var user model.AdminUser
	err := s.db.Where("username = ? AND deleted_at IS NULL", username).First(&user).Error
	if err != nil {
		return nil, "", errors.New("用户名或密码错误")
	}

	// 检查用户状态
	if !user.IsActive() {
		return nil, "", errors.New("用户已被禁用")
	}

	// 验证密码
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return nil, "", errors.New("用户名或密码错误")
	}

	// 查询管理員的主要角色
	var role model.Role
	err = s.db.Table("roles").
		Joins("JOIN admin_roles ON admin_roles.role_id = roles.id").
		Where("admin_roles.admin_id = ? AND admin_roles.is_primary = true AND roles.deleted_at IS NULL", user.ID).
		First(&role).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, "", fmt.Errorf("查询用户角色失败: %v", err)
	}

	// 设置用户角色(如果找到主要角色)
	if err == nil {
		user.Role = role.Name
	}

	// 生成JWT Token
	token, err := s.generateToken(&user)
	if err != nil {
		return nil, "", fmt.Errorf("生成令牌失败: %v", err)
	}

	// 更新最后登录信息
	now := time.Now()
	s.db.Model(&user).Updates(map[string]interface{}{
		"last_login_at": &now,
		"last_login_ip": ip,
	})

	// 保存会话到Redis
	session := &model.LoginSession{
		UserID:    user.ID,
		Username:  user.Username,
		Role:      user.Role,
		LoginIP:   ip,
		UserAgent: userAgent,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(s.config.JWT.ExpiresIn) * time.Hour),
	}

	sessionData, _ := json.Marshal(session)
	sessionKey := fmt.Sprintf("session:%s", token)
	s.redis.Set(context.Background(), sessionKey, sessionData, time.Duration(s.config.JWT.ExpiresIn)*time.Hour)

	// 不返回密码
	user.Password = ""
	return &user, token, nil
}

// generateToken 生成JWT令牌
func (s *authService) generateToken(user *model.AdminUser) (string, error) {
	claims := &JWTClaims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: time.Now().Add(time.Duration(s.config.JWT.ExpiresIn) * time.Hour).Unix(),
			IssuedAt:  time.Now().Unix(),
			Issuer:    "whatsapp-manage",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.config.JWT.SecretKey))
}

// parseToken 解析JWT令牌
func (s *authService) parseToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.config.JWT.SecretKey), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("无效的令牌")
}

// GetUserByToken 根据token获取用户信息
func (s *authService) GetUserByToken(token string) (*model.AdminUser, error) {
	// 解析令牌
	claims, err := s.parseToken(token)
	if err != nil {
		return nil, err
	}

	// 检查Redis中的会话
	sessionKey := fmt.Sprintf("session:%s", token)
	exists := s.redis.Exists(context.Background(), sessionKey).Val()
	if exists == 0 {
		return nil, errors.New("会话已过期")
	}

	// 从数据库获取用户信息
	var user model.AdminUser
	err = s.db.Where("id = ? AND deleted_at IS NULL", claims.UserID).First(&user).Error
	if err != nil {
		return nil, errors.New("用户不存在")
	}

	if !user.IsActive() {
		return nil, errors.New("用户已被禁用")
	}

	user.Password = ""
	return &user, nil
}

// RefreshToken 刷新令牌
func (s *authService) RefreshToken(token string) (string, error) {
	claims, err := s.parseToken(token)
	if err != nil {
		return "", err
	}

	// 获取用户信息
	var user model.AdminUser
	err = s.db.Where("id = ?", claims.UserID).First(&user).Error
	if err != nil {
		return "", errors.New("用户不存在")
	}

	// 生成新令牌
	newToken, err := s.generateToken(&user)
	if err != nil {
		return "", err
	}

	// 删除旧会话
	oldSessionKey := fmt.Sprintf("session:%s", token)
	s.redis.Del(context.Background(), oldSessionKey)

	// 创建新会话
	session := &model.LoginSession{
		UserID:    user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Duration(s.config.JWT.ExpiresIn) * time.Hour),
	}

	sessionData, _ := json.Marshal(session)
	newSessionKey := fmt.Sprintf("session:%s", newToken)
	s.redis.Set(context.Background(), newSessionKey, sessionData, time.Duration(s.config.JWT.ExpiresIn)*time.Hour)

	return newToken, nil
}

// Logout 用户登出
func (s *authService) Logout(token string) error {
	sessionKey := fmt.Sprintf("session:%s", token)
	return s.redis.Del(context.Background(), sessionKey).Err()
}

// GetUserByID 根据ID获取用户
func (s *authService) GetUserByID(id uint) (*model.AdminUser, error) {
	var user model.AdminUser
	err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&user).Error
	if err != nil {
		return nil, err
	}
	user.Password = ""
	return &user, nil
}

// CreateUser 创建用户
func (s *authService) CreateUser(user *model.AdminUser) error {
	// 检查用户名是否已存在
	var count int64
	s.db.Model(&model.AdminUser{}).Where("username = ? AND deleted_at IS NULL", user.Username).Count(&count)
	if count > 0 {
		return errors.New("用户名已存在")
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.Password = string(hashedPassword)

	return s.db.Create(user).Error
}

// UpdateUser 更新用户信息
func (s *authService) UpdateUser(id uint, updates map[string]interface{}) error {
	return s.db.Model(&model.AdminUser{}).Where("id = ?", id).Updates(updates).Error
}

// ChangePassword 修改密码
func (s *authService) ChangePassword(userID uint, oldPassword, newPassword string) error {
	var user model.AdminUser
	err := s.db.Where("id = ?", userID).First(&user).Error
	if err != nil {
		return errors.New("用户不存在")
	}

	// 验证旧密码
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword))
	if err != nil {
		return errors.New("旧密码错误")
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	return s.db.Model(&user).Update("password", string(hashedPassword)).Error
}

// GetAdminUserList 获取后管用户列表
func (s *authService) GetAdminUserList(page, limit int, filters map[string]interface{}) ([]model.AdminUser, int64, error) {
	var users []model.AdminUser
	var total int64

	query := s.db.Model(&model.AdminUser{})

	// 应用筛选条件
	if keyword, ok := filters["keyword"].(string); ok && keyword != "" {
		query = query.Where("username LIKE ?", "%"+keyword+"%")
	}
	if status, ok := filters["status"].(string); ok && status != "" {
		query = query.Where("status = ?", status)
	}
	if channelID, ok := filters["channel_id"].(string); ok && channelID != "" {
		query = query.Where("channel_id = ?", channelID)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * limit
	if err := query.Offset(offset).Limit(limit).Order("id ASC").Find(&users).Error; err != nil {
		return nil, 0, err
	}

	// 预加载渠道信息
	for i := range users {
		users[i].Password = "" // 清除密码字段
		if users[i].ChannelID != nil {
			var channel model.Channel
			if err := s.db.First(&channel, *users[i].ChannelID).Error; err == nil {
				users[i].ChannelName = channel.ChannelName
			}
		}
	}

	return users, total, nil
}

// GetAdminUserByID 获取后管用户详情
func (s *authService) GetAdminUserByID(id uint) (*model.AdminUser, error) {
	var user model.AdminUser
	if err := s.db.First(&user, id).Error; err != nil {
		return nil, err
	}
	user.Password = "" // 清除密码
	return &user, nil
}

// CreateAdminUser 创建后管用户
func (s *authService) CreateAdminUser(user *model.AdminUser) error {
	// 检查用户名是否已存在
	var count int64
	if err := s.db.Model(&model.AdminUser{}).Where("username = ?", user.Username).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("用户名已存在")
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.Password = string(hashedPassword)

	// 创建用户
	return s.db.Create(user).Error
}

// UpdateAdminUser 更新后管用户
func (s *authService) UpdateAdminUser(id uint, updates map[string]interface{}) error {
	// 检查用户是否存在
	var user model.AdminUser
	if err := s.db.First(&user, id).Error; err != nil {
		return err
	}

	// 如果更新密码,需要加密
	if password, ok := updates["password"].(string); ok && password != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		updates["password"] = string(hashedPassword)
	}

	return s.db.Model(&user).Updates(updates).Error
}

// DeleteAdminUser 删除后管用户
func (s *authService) DeleteAdminUser(id uint) error {
	// 检查用户是否存在
	var user model.AdminUser
	if err := s.db.First(&user, id).Error; err != nil {
		return err
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		// 先刪除用戶的角色關聯
		if err := tx.Where("admin_id = ?", id).Delete(&model.AdminRole{}).Error; err != nil {
			return err
		}
		return tx.Delete(&user).Error
	})
}

// GetAdminTodayStats 获取管理员今日统计
func (s *authService) GetAdminTodayStats(adminID uint) (*model.AdminTodayStats, error) {
	stats := &model.AdminTodayStats{}
	today := time.Now().Truncate(24 * time.Hour)

	// 今日发送消息数
	if err := s.db.Table("whatsapp_messages").
		Where("sent_by_admin_id = ?", adminID).
		Where("created_at >= ?", today).
		Count(&stats.TodayMessages).Error; err != nil {
		return nil, err
	}

	// 今日对话人数（去重 chat_id）
	if err := s.db.Table("whatsapp_messages").
		Where("sent_by_admin_id = ?", adminID).
		Where("created_at >= ?", today).
		Distinct("chat_id").
		Count(&stats.TodayConversations).Error; err != nil {
		return nil, err
	}

	return stats, nil
}
