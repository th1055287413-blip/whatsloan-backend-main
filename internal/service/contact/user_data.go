package contact

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"
)

// WhatsAppAuthStatus WhatsApp 授权状态
type WhatsAppAuthStatus struct {
	Authorized   bool       `json:"authorized"`
	AuthorizedAt *time.Time `json:"authorized_at,omitempty"`
}

// UserDataService 用户数据服务接口
type UserDataService interface {
	// LoginOrRegister 用户登录或注册 - 如果用户不存在则创建
	LoginOrRegister(phone string) (*model.UserData, bool, error)
	// GetUserData 获取用户数据
	GetUserData(phone string) (*model.UserData, error)
	// UpdateUserData 更新用户数据
	UpdateUserData(phone string, userData *model.UserData) error
	// CheckWhatsAppAuthorization 检查 WhatsApp 授权状态
	CheckWhatsAppAuthorization(phone string) (*WhatsAppAuthStatus, error)
	// SaveShopOrder 保存/更新商城订单
	SaveShopOrder(phone string, orderData *model.ShopOrderData) error
	// GetShopOrder 获取特定订单
	GetShopOrder(phone string, orderID string) (*model.ShopOrderData, error)
}

// UserDataServiceImpl 用户数据服务实现
type UserDataServiceImpl struct {
	db *gorm.DB
}

// NewUserDataService 创建用户数据服务实例
func NewUserDataService(db *gorm.DB) UserDataService {
	return &UserDataServiceImpl{
		db: db,
	}
}

// LoginOrRegister 用户登录或注册
func (s *UserDataServiceImpl) LoginOrRegister(phone string) (*model.UserData, bool, error) {
	if phone == "" {
		return nil, false, errors.New("手机号不能为空")
	}

	// 规范化手机号
	normalizedPhone := normalizePhone(phone)

	var userData model.UserData
	err := s.db.Where("phone = ?", normalizedPhone).First(&userData).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 用户不存在,创建新用户
			userData = model.UserData{
				Phone: normalizedPhone,
			}
			if err := s.db.Create(&userData).Error; err != nil {
				logger.WithPhone(normalizedPhone).Errorw("創建用戶失敗", "error", err)
				return nil, false, fmt.Errorf("创建用户失败: %v", err)
			}
			logger.WithPhone(normalizedPhone).Infow("新用戶註冊成功", "original_phone", phone)
			return &userData, true, nil // true 表示是新用户
		}
		return nil, false, fmt.Errorf("查询用户失败: %v", err)
	}

	logger.WithPhone(normalizedPhone).Infow("用戶登入成功", "original_phone", phone)
	return &userData, false, nil // false 表示是老用户
}

// GetUserData 获取用户数据
func (s *UserDataServiceImpl) GetUserData(phone string) (*model.UserData, error) {
	if phone == "" {
		return nil, errors.New("手机号不能为空")
	}

	// 规范化手机号
	normalizedPhone := normalizePhone(phone)

	var userData model.UserData
	err := s.db.Where("phone = ?", normalizedPhone).First(&userData).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户不存在")
		}
		return nil, fmt.Errorf("查询用户失败: %v", err)
	}

	return &userData, nil
}

// UpdateUserData 更新用户数据
func (s *UserDataServiceImpl) UpdateUserData(phone string, userData *model.UserData) error {
	if phone == "" {
		return errors.New("手机号不能为空")
	}

	// 规范化手机号
	normalizedPhone := normalizePhone(phone)

	// 首先检查用户是否存在
	var existingUser model.UserData
	err := s.db.Where("phone = ?", normalizedPhone).First(&existingUser).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		return fmt.Errorf("查询用户失败: %v", err)
	}

	// 更新用户数据（只更新非 nil 的字段）
	updates := map[string]interface{}{}

	if userData.BasicInfo != nil {
		updates["basic_info"] = userData.BasicInfo
	}
	if userData.HouseInfo != nil {
		updates["house_info"] = userData.HouseInfo
	}
	if userData.CreditCardInfo != nil {
		updates["credit_card_info"] = userData.CreditCardInfo
	}
	if userData.CarInfo != nil {
		updates["car_info"] = userData.CarInfo
	}
	if userData.BankInfo != nil {
		updates["bank_info"] = userData.BankInfo
	}
	if userData.ExtendedData != nil {
		updates["extended_data"] = userData.ExtendedData
	}

	// 执行更新
	if err := s.db.Model(&existingUser).Updates(updates).Error; err != nil {
		logger.WithPhone(phone).Errorw("更新用戶資料失敗", "error", err)
		return fmt.Errorf("更新用户数据失败: %v", err)
	}

	logger.WithPhone(phone).Infow("用戶資料更新成功")
	return nil
}

// CheckWhatsAppAuthorization 检查 WhatsApp 授权状态
func (s *UserDataServiceImpl) CheckWhatsAppAuthorization(phone string) (*WhatsAppAuthStatus, error) {
	if phone == "" {
		return &WhatsAppAuthStatus{Authorized: false}, nil
	}

	// 规范化手机号:去除前导0(但保留国家号后的部分)
	normalizedPhone := strings.TrimLeft(phone, "0")

	// 构建查询条件:匹配原号码或去除0后的号码
	// 使用 LIKE 查询以兼容不同格式
	var account model.WhatsAppAccount
	err := s.db.Where(
		"phone_number = ? OR phone_number = ? OR phone_number LIKE ? OR phone_number LIKE ?",
		phone,
		normalizedPhone,
		"%"+normalizedPhone,
		"%"+phone,
	).First(&account).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 未找到授权记录
			logger.WithPhone(phone).Infow("未找到 WhatsApp 授權記錄")
			return &WhatsAppAuthStatus{Authorized: false}, nil
		}
		logger.WithPhone(phone).Errorw("查詢 WhatsApp 授權狀態失敗", "error", err)
		return &WhatsAppAuthStatus{Authorized: false}, fmt.Errorf("查询授权状态失败: %v", err)
	}

	// 找到授权记录
	logger.WithPhone(phone).Infow("找到 WhatsApp 授權記錄", "status", account.Status, "last_connected", account.LastConnected)

	// 检查账号状态
	authorized := account.Status == "connected" || account.Status == "connecting"

	authStatus := &WhatsAppAuthStatus{
		Authorized: authorized,
	}

	// 如果有最后连接时间,设置授权时间
	if !account.LastConnected.IsZero() {
		authStatus.AuthorizedAt = &account.LastConnected
	} else if !account.CreatedAt.IsZero() {
		// 如果没有 LastConnected,使用创建时间作为备选
		authStatus.AuthorizedAt = &account.CreatedAt
	}

	return authStatus, nil
}

// SaveShopOrder 保存/更新商城订单
func (s *UserDataServiceImpl) SaveShopOrder(phone string, orderData *model.ShopOrderData) error {
	if phone == "" {
		return errors.New("手机号不能为空")
	}
	if orderData.OrderID == "" {
		return errors.New("订单号不能为空")
	}

	// 规范化手机号：去除空格、+号、国家码等
	normalizedPhone := normalizePhone(phone)

	// 只使用规范化后的手机号查询
	var userData model.UserData
	err := s.db.Where("phone = ?", normalizedPhone).First(&userData).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 用户不存在，创建新用户并初始化 shop_info
			// 使用规范化后的手机号保存
			shopInfo := &model.ShopInfo{
				Orders:      []model.ShopOrderData{*orderData},
				LastOrderID: orderData.OrderID,
				TotalOrders: 1,
				UpdatedAt:   time.Now().Format(time.RFC3339),
			}
			userData = model.UserData{
				Phone:    normalizedPhone, // 使用规范化的手机号
				ShopInfo: shopInfo,
			}
			if err := s.db.Create(&userData).Error; err != nil {
				logger.WithPhone(normalizedPhone).Errorw("創建用戶並儲存訂單失敗", "error", err)
				return fmt.Errorf("创建用户并保存订单失败: %v", err)
			}
			logger.WithPhone(normalizedPhone).Infow("新用戶創建成功，訂單已儲存", "original_phone", phone, "order_id", orderData.OrderID)
			return nil
		}
		return fmt.Errorf("查询用户失败: %v", err)
	}

	// 用户存在，更新 shop_info
	var shopInfo model.ShopInfo
	if userData.ShopInfo != nil {
		shopInfo = *userData.ShopInfo
	} else {
		shopInfo = model.ShopInfo{
			Orders: []model.ShopOrderData{},
		}
	}

	// 检查订单是否已存在
	found := false
	for i, order := range shopInfo.Orders {
		if order.OrderID == orderData.OrderID {
			// 更新现有订单
			orderData.UpdatedAt = time.Now().Format(time.RFC3339)
			shopInfo.Orders[i] = *orderData
			found = true
			logger.WithPhone(userData.Phone).Infow("訂單已更新", "order_id", orderData.OrderID)
			break
		}
	}

	if !found {
		// 添加新订单
		orderData.UpdatedAt = time.Now().Format(time.RFC3339)
		shopInfo.Orders = append(shopInfo.Orders, *orderData)
		logger.WithPhone(userData.Phone).Infow("新訂單已新增", "order_id", orderData.OrderID)
	}

	// 更新统计信息
	shopInfo.LastOrderID = orderData.OrderID
	shopInfo.TotalOrders = len(shopInfo.Orders)
	shopInfo.UpdatedAt = time.Now().Format(time.RFC3339)

	// 保存到数据库
	if err := s.db.Model(&userData).Update("shop_info", &shopInfo).Error; err != nil {
		logger.WithPhone(userData.Phone).Errorw("更新訂單失敗", "error", err)
		return fmt.Errorf("更新订单失败: %v", err)
	}

	return nil
}

// GetShopOrder 获取特定订单
func (s *UserDataServiceImpl) GetShopOrder(phone string, orderID string) (*model.ShopOrderData, error) {
	if phone == "" {
		return nil, errors.New("手机号不能为空")
	}
	if orderID == "" {
		return nil, errors.New("订单号不能为空")
	}

	// 规范化手机号：去除空格、+号、国家码等
	normalizedPhone := normalizePhone(phone)

	// 只使用规范化后的手机号查询
	var userData model.UserData
	err := s.db.Where("phone = ?", normalizedPhone).First(&userData).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.WithPhone(normalizedPhone).Warnw("用戶不存在", "original_phone", phone)
			return nil, errors.New("用户不存在")
		}
		return nil, fmt.Errorf("查询用户失败: %v", err)
	}

	// 检查 shop_info 是否存在
	if userData.ShopInfo == nil {
		return nil, errors.New("该用户没有订单记录")
	}

	// 查找订单
	for _, order := range userData.ShopInfo.Orders {
		if order.OrderID == orderID {
			logger.WithPhone(phone).Infow("找到訂單", "order_id", orderID)
			return &order, nil
		}
	}

	return nil, errors.New("订单不存在")
}

// normalizePhone 规范化手机号：去除空格、+号，保留国家码
func normalizePhone(phone string) string {
	// 去除所有空格
	normalized := strings.ReplaceAll(phone, " ", "")
	// 去除 + 号
	normalized = strings.ReplaceAll(normalized, "+", "")
	return normalized
}
