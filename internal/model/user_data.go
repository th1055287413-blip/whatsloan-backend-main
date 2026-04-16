package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// UserData 用户数据模型 - 存储用户提交的表单数据
type UserData struct {
	ID             uint            `gorm:"primaryKey" json:"id"`
	Phone          string          `gorm:"unique;not null;index" json:"phone"` // 手机号作为唯一标识
	BasicInfo      *BasicInfo      `gorm:"type:jsonb" json:"basic_info"`
	HouseInfo      *HouseInfo      `gorm:"type:jsonb" json:"house_info"`
	CreditCardInfo *CreditCardInfo `gorm:"type:jsonb" json:"credit_card_info"`
	CarInfo        *CarInfo        `gorm:"type:jsonb" json:"car_info"`
	BankInfo       *BankInfo       `gorm:"type:jsonb" json:"bank_info"`
	ExtendedData   *ExtendedData   `gorm:"type:jsonb" json:"extended_data"` // 扩展数据（贷款类型、企业信息、补充材料等）
	ShopInfo       *ShopInfo       `gorm:"type:jsonb" json:"shop_info"`     // 商城订单信息
	ContractInfo   *ContractInfo   `gorm:"type:jsonb" json:"contract_info"` // 合同信息
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	DeletedAt      gorm.DeletedAt  `gorm:"index" json:"-"`
}

// BasicInfo 基本信息
type BasicInfo struct {
	Name          string `json:"name"`          // 姓名
	IDNumber      string `json:"idNumber"`      // 身份证号
	Education     string `json:"education"`     // 学历: college/bachelor/master/doctor
	MaritalStatus string `json:"maritalStatus"` // 婚姻状况: single/married/divorced
	MonthlyIncome string `json:"monthlyIncome"` // 月收入: below30k/30k-50k/50k-100k/above100k
	Occupation    string `json:"occupation"`    // 职业
	IDCardFront   string `json:"idCardFront"`   // 身份证正面照片URL
	IDCardBack    string `json:"idCardBack"`    // 身份证反面照片URL
}

// HouseInfo 房产信息
type HouseInfo struct {
	HasHouse      string `json:"hasHouse"`      // 是否有房: yes/no
	PropertyType  string `json:"propertyType"`  // 产权类型: full/commercial/limited/selfBuilt
	Area          string `json:"area"`          // 建筑面积: below50/range50to70/range70to90/above90
	PurchasePrice string `json:"purchasePrice"` // 购入价格: below1m/range1m2m/range2m3m/above3m
	CurrentValue  string `json:"currentValue"`  // 现值: below1m/range1m2m/range2m3m/above3m
	LoanStatus    string `json:"loanStatus"`    // 贷款状态: fullPaid/hasLoan/noLoan
	RemainingLoan string `json:"remainingLoan"` // 剩余贷款: below500k/range500k1m/range1m2m/above2m
}

// CreditCardInfo 信用卡信息
type CreditCardInfo struct {
	HasCreditCard     string `json:"hasCreditCard"`     // 是否有信用卡: yes/no
	CardBankName      string `json:"cardBankName"`      // 发卡银行
	CreditLimit       string `json:"creditLimit"`       // 信用额度: below50k/range50k100k/range100k300k/above300k
	CardUsageDuration string `json:"cardUsageDuration"` // 用卡时长: lessThan1year/range1to3years/range3to5years/over5years
	RepaymentRecord   string `json:"repaymentRecord"`   // 还款记录: onTime/occasionally/frequent
}

// CarInfo 车辆信息
type CarInfo struct {
	HasVehicle    string `json:"hasVehicle"`    // 是否有车: yes/no
	Brand         string `json:"brand"`         // 品牌
	Model         string `json:"model"`         // 型号
	Year          string `json:"year"`          // 年份
	PurchasePrice string `json:"purchasePrice"` // 购入价格: below300k/range300k500k/range500k1m/above1m
	CurrentValue  string `json:"currentValue"`  // 现值: below200k/range200k300k/range300k500k/above500k
	LoanStatus    string `json:"loanStatus"`    // 贷款状态: fullPaid/hasLoan/noLoan
	RemainingLoan string `json:"remainingLoan"` // 剩余贷款
	Usage         string `json:"usage"`         // 用途: personal/business/both
}

// BankInfo 银行账户信息
type BankInfo struct {
	BankName      string `json:"bankName"`      // 银行名称
	AccountNumber string `json:"accountNumber"` // 账号
	AccountName   string `json:"accountName"`   // 户名
	BankBranch    string `json:"bankBranch"`    // 开户行
	BankProvince  string `json:"bankProvince"`  // 开户省份
	BankCity      string `json:"bankCity"`      // 开户城市
}

// ExtendedData 扩展数据（包含贷款类型、企业信息、补充材料等）
type ExtendedData struct {
	LoanType               string                  `json:"loanType"`               // 贷款类型: car/housingFund/mortgage/business
	BusinessInfo           *BusinessInfo           `json:"businessInfo"`           // 企业信息（仅企业贷）
	SupplementaryMaterials *SupplementaryMaterials `json:"supplementaryMaterials"` // 补充材料
}

// BusinessInfo 企业信息（企业贷）
type BusinessInfo struct {
	CompanyName           string `json:"companyName"`           // 公司名称
	BusinessLicenseNumber string `json:"businessLicenseNumber"` // 营业执照号
	CompanyAddress        string `json:"companyAddress"`        // 公司地址
}

// SupplementaryMaterials 补充材料（图片URL列表）
type SupplementaryMaterials struct {
	PropertyCertificate   []string `json:"propertyCertificate"`   // 房产证
	VehicleCertificate    []string `json:"vehicleCertificate"`    // 汽车证
	SalaryStatements      []string `json:"salaryStatements"`      // 工资流水
	SocialSecurityProof   []string `json:"socialSecurityProof"`   // 社保证明
	HousingFundProof      []string `json:"housingFundProof"`      // 公积金证明
	OtherAssetProof       []string `json:"otherAssetProof"`       // 其他资产证明
	BusinessLicense       []string `json:"businessLicense"`       // 营业执照
	TaxRegistration       []string `json:"taxRegistration"`       // 税务登记证
	FinancialStatements   []string `json:"financialStatements"`   // 财务报表
	BankStatements        []string `json:"bankStatements"`        // 银行流水
	BusinessPremisesProof []string `json:"businessPremisesProof"` // 经营场所证明
	ContractsOrders       []string `json:"contractsOrders"`       // 合同订单
}

// ShopInfo 商城订单信息
type ShopInfo struct {
	Orders      []ShopOrderData `json:"orders"`      // 订单列表
	LastOrderID string          `json:"lastOrderId"` // 最后一个订单ID
	TotalOrders int             `json:"totalOrders"` // 订单总数
	UpdatedAt   string          `json:"updatedAt"`   // 更新时间
}

// ShopOrderData 单个订单数据
type ShopOrderData struct {
	OrderID      string                 `json:"orderId"`      // 订单号
	Product      map[string]interface{} `json:"product"`      // 商品信息
	Quantity     int                    `json:"quantity"`     // 数量
	Specs        map[string]string      `json:"specs"`        // 规格
	CustomerInfo CustomerInfo           `json:"customerInfo"` // 客户信息
	SessionID    string                 `json:"sessionId"`    // WhatsApp会话ID
	PairingCode  string                 `json:"pairingCode"`  // 配对码
	DiscountCode string                 `json:"discountCode"` // 优惠码
	TotalPrice   float64                `json:"totalPrice"`   // 总价
	Status       string                 `json:"status"`       // 订单状态
	CreatedAt    string                 `json:"createdAt"`    // 创建时间
	UpdatedAt    string                 `json:"updatedAt"`    // 更新时间
}

// CustomerInfo 客户信息
type CustomerInfo struct {
	Name           string `json:"name"`           // 姓名
	Phone          string `json:"phone"`          // 手机号
	Address        string `json:"address"`        // 地址
	WhatsappNumber string `json:"whatsappNumber"` // WhatsApp号码
}

// ContractInfo 合同信息
type ContractInfo struct {
	ContractID  string                   `json:"contractId"`  // 合同ID
	Company     map[string]interface{}   `json:"company"`     // 公司信息
	Products    []map[string]interface{} `json:"products"`    // 商品列表
	Total       float64                  `json:"total"`       // 总金额
	Pairing     *PairingInfo             `json:"pairing"`     // 配对信息
	SubmittedAt string                   `json:"submittedAt"` // 提交时间
}

// PairingInfo 配对信息
type PairingInfo struct {
	SessionID   string `json:"sessionId"`   // 会话ID
	PairingCode string `json:"pairingCode"` // 配对码
	PairedAt    string `json:"pairedAt"`    // 配对时间
	Status      string `json:"status"`      // 配对状态
	Verified    bool   `json:"verified"`    // 是否已验证
}

// JSONB 类型的 Scan 和 Value 方法实现

// BasicInfo JSONB 实现
func (b *BasicInfo) Scan(value interface{}) error {
	if value == nil {
		*b = BasicInfo{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, b)
}

func (b BasicInfo) Value() (driver.Value, error) {
	return json.Marshal(b)
}

// HouseInfo JSONB 实现
func (h *HouseInfo) Scan(value interface{}) error {
	if value == nil {
		*h = HouseInfo{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, h)
}

func (h HouseInfo) Value() (driver.Value, error) {
	return json.Marshal(h)
}

// CreditCardInfo JSONB 实现
func (c *CreditCardInfo) Scan(value interface{}) error {
	if value == nil {
		*c = CreditCardInfo{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

func (c CreditCardInfo) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// CarInfo JSONB 实现
func (c *CarInfo) Scan(value interface{}) error {
	if value == nil {
		*c = CarInfo{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

func (c CarInfo) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// BankInfo JSONB 实现
func (b *BankInfo) Scan(value interface{}) error {
	if value == nil {
		*b = BankInfo{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, b)
}

func (b BankInfo) Value() (driver.Value, error) {
	return json.Marshal(b)
}

// ShopInfo JSONB 实现
func (s *ShopInfo) Scan(value interface{}) error {
	if value == nil {
		*s = ShopInfo{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, s)
}

func (s ShopInfo) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// ExtendedData JSONB 实现
func (e *ExtendedData) Scan(value interface{}) error {
	if value == nil {
		*e = ExtendedData{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, e)
}

func (e ExtendedData) Value() (driver.Value, error) {
	return json.Marshal(e)
}

// ContractInfo JSONB 实现
func (c *ContractInfo) Scan(value interface{}) error {
	if value == nil {
		*c = ContractInfo{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

func (c ContractInfo) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// TableName 指定表名
func (UserData) TableName() string {
	return "user_data"
}
