package workgroup

import (
	"errors"
	"fmt"
	"time"

	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// WorkgroupAccountDetail 帳號分配詳情（含帳號資訊）
type WorkgroupAccountDetail struct {
	model.WorkgroupAccount
	PhoneNumber string `json:"phone_number"`
	PushName    string `json:"push_name"`
	AccountStatus string `json:"account_status"`
	AgentName   string `json:"agent_name,omitempty"`
}

// AssignableAccount 可分配帳號
type AssignableAccount struct {
	ID          uint   `json:"id"`
	PhoneNumber string `json:"phone_number"`
	PushName    string `json:"push_name"`
	Status      string `json:"status"`
}

// AssignableAccountsFilter 可分配帳號篩選條件
type AssignableAccountsFilter struct {
	Page          int
	PageSize      int
	Keyword       string
	Status        string
	WorkgroupType string // 排除同 type 已分配的帳號
	// UserData JSONB 篩選（key=欄位路徑, value=值）
	UserDataFilters map[string]string
}

// AssignableConditionFilter 按條件篩選可分配帳號
type AssignableConditionFilter struct {
	TagIDs              []uint
	AuthorizedMinutesGT *int
}

// WorkgroupService 工作組服務
type WorkgroupService interface {
	List(page, pageSize int, filters map[string]interface{}) ([]model.Workgroup, int64, error)
	GetByID(id uint) (*model.Workgroup, error)
	Create(wg *model.Workgroup) error
	Update(id uint, updates map[string]interface{}) error
	Archive(id uint) error

	AssignAccounts(workgroupID uint, accountIDs []uint, adminID uint) error
	RemoveAccounts(workgroupID uint, accountIDs []uint) error
	GetAccounts(workgroupID uint, page, pageSize int) ([]WorkgroupAccountDetail, int64, error)
	GetAssignableAccounts(filters AssignableAccountsFilter) ([]AssignableAccount, int64, error)
	CountAssignableByCondition(workgroupType string, filter AssignableConditionFilter) (int64, error)
	AssignAccountsByCondition(workgroupID uint, filter AssignableConditionFilter, count int, adminID uint) (int64, error)
	AutoAssignAccount(accountID uint, channelID *uint, sourceAgentID *uint) error
}

type workgroupService struct {
	db *gorm.DB
}

// NewWorkgroupService 建立工作組服務
func NewWorkgroupService(db *gorm.DB) WorkgroupService {
	return &workgroupService{db: db}
}

func (s *workgroupService) List(page, pageSize int, filters map[string]interface{}) ([]model.Workgroup, int64, error) {
	var items []model.Workgroup
	var total int64

	query := s.db.Model(&model.Workgroup{})

	if status, ok := filters["status"].(string); ok && status != "" {
		query = query.Where("status = ?", status)
	} else {
		query = query.Where("status != ?", model.WorkgroupStatusArchived)
	}
	if keyword, ok := filters["keyword"].(string); ok && keyword != "" {
		query = query.Where("name ILIKE ? OR description ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if wgType, ok := filters["type"].(string); ok && wgType != "" {
		query = query.Where("type = ?", wgType)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("id DESC").Find(&items).Error; err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func (s *workgroupService) GetByID(id uint) (*model.Workgroup, error) {
	var wg model.Workgroup
	if err := s.db.First(&wg, id).Error; err != nil {
		return nil, err
	}
	return &wg, nil
}

func (s *workgroupService) Create(wg *model.Workgroup) error {
	if wg.Code == model.WorkgroupCodeAdmin || wg.Name == model.WorkgroupNameAdmin {
		return errors.New("此代碼或名稱為系統保留，無法使用")
	}
	return s.db.Create(wg).Error
}

func (s *workgroupService) Update(id uint, updates map[string]interface{}) error {
	var wg model.Workgroup
	if err := s.db.First(&wg, id).Error; err != nil {
		return err
	}
	if wg.Code == model.WorkgroupCodeAdmin {
		return errors.New("預設管理員工作組無法修改")
	}
	if code, ok := updates["code"].(string); ok && code == model.WorkgroupCodeAdmin {
		return errors.New("此代碼為系統保留，無法使用")
	}
	if name, ok := updates["name"].(string); ok && name == model.WorkgroupNameAdmin {
		return errors.New("此名稱為系統保留，無法使用")
	}
	result := s.db.Model(&model.Workgroup{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *workgroupService) Archive(id uint) error {
	var wg model.Workgroup
	if err := s.db.First(&wg, id).Error; err != nil {
		return err
	}
	if wg.Code == model.WorkgroupCodeAdmin {
		return errors.New("預設管理員工作組無法封存")
	}
	return s.db.Model(&model.Workgroup{}).Where("id = ?", id).Update("status", model.WorkgroupStatusArchived).Error
}

func (s *workgroupService) AssignAccounts(workgroupID uint, accountIDs []uint, adminID uint) error {
	// 驗證工作組存在
	var wg model.Workgroup
	if err := s.db.First(&wg, workgroupID).Error; err != nil {
		return errors.New("工作組不存在")
	}

	now := time.Now()
	for _, accountID := range accountIDs {
		wa := model.WorkgroupAccount{
			WorkgroupID:   workgroupID,
			AccountID:     accountID,
			WorkgroupType: wg.Type,
			AssignedBy:    adminID,
			AssignedAt:    now,
		}
		if err := s.db.Create(&wa).Error; err != nil {
			return fmt.Errorf("分配帳號 %d 失敗: %v", accountID, err)
		}
	}
	return nil
}

func (s *workgroupService) RemoveAccounts(workgroupID uint, accountIDs []uint) error {
	return s.db.Where("workgroup_id = ? AND account_id IN ?", workgroupID, accountIDs).
		Delete(&model.WorkgroupAccount{}).Error
}

func (s *workgroupService) GetAccounts(workgroupID uint, page, pageSize int) ([]WorkgroupAccountDetail, int64, error) {
	var wg model.Workgroup
	if err := s.db.First(&wg, workgroupID).Error; err != nil {
		return nil, 0, err
	}

	var total int64
	var results []WorkgroupAccountDetail
	offset := (page - 1) * pageSize

	if wg.Type == model.WorkgroupTypeAdmin {
		s.db.Table("whatsapp_accounts").Count(&total)
		err := s.db.Table("whatsapp_accounts a").
			Select("a.id AS account_id, a.phone_number, a.push_name, a.status AS account_status").
			Offset(offset).Limit(pageSize).
			Order("a.id DESC").
			Scan(&results).Error
		return results, total, err
	}

	s.db.Model(&model.WorkgroupAccount{}).Where("workgroup_id = ?", workgroupID).Count(&total)
	err := s.db.Table("workgroup_accounts wa").
		Select("wa.*, a.phone_number, a.push_name, a.status AS account_status, ag.username AS agent_name").
		Joins("LEFT JOIN whatsapp_accounts a ON a.id = wa.account_id").
		Joins("LEFT JOIN agents ag ON ag.id = wa.assigned_agent_id AND ag.deleted_at IS NULL").
		Where("wa.workgroup_id = ?", workgroupID).
		Offset(offset).Limit(pageSize).
		Order("wa.id DESC").
		Scan(&results).Error
	return results, total, err
}

func (s *workgroupService) GetAssignableAccounts(filters AssignableAccountsFilter) ([]AssignableAccount, int64, error) {
	var results []AssignableAccount
	var total int64

	query := s.db.Table("whatsapp_accounts a").
		Select("a.id, a.phone_number, a.push_name, a.status")

	if filters.WorkgroupType != model.WorkgroupTypeAdmin {
		query = query.Where("a.id NOT IN (SELECT account_id FROM workgroup_accounts WHERE workgroup_type = ?)", filters.WorkgroupType)
	}

	if filters.Status != "" {
		query = query.Where("a.status = ?", filters.Status)
	}

	if filters.Keyword != "" {
		query = query.Where("a.phone_number ILIKE ? OR a.push_name ILIKE ?",
			"%"+filters.Keyword+"%", "%"+filters.Keyword+"%")
	}

	// user_data JSONB 篩選
	for key, val := range filters.UserDataFilters {
		query = query.Where("a.id IN (SELECT wa.id FROM whatsapp_accounts wa JOIN user_data ud ON ud.phone = wa.phone_number WHERE ud.basic_info->>? = ?)", key, val)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (filters.Page - 1) * filters.PageSize
	if err := query.Offset(offset).Limit(filters.PageSize).Order("a.id DESC").Scan(&results).Error; err != nil {
		return nil, 0, err
	}

	return results, total, nil
}

// buildAssignableConditionQuery 建立按條件篩選可分配帳號的共用查詢
func (s *workgroupService) buildAssignableConditionQuery(workgroupType string, filter AssignableConditionFilter) *gorm.DB {
	query := s.db.Table("whatsapp_accounts a").
		Where("a.status = ?", "connected")

	if workgroupType != model.WorkgroupTypeAdmin {
		query = query.Where("a.id NOT IN (SELECT account_id FROM workgroup_accounts WHERE workgroup_type = ?)", workgroupType)
	}

	if len(filter.TagIDs) > 0 {
		query = query.Where("a.id IN (SELECT account_id FROM whatsapp_account_tags WHERE tag_id IN ?)", filter.TagIDs)
	}

	if filter.AuthorizedMinutesGT != nil {
		query = query.Where("a.created_at < NOW() - MAKE_INTERVAL(mins => ?)", *filter.AuthorizedMinutesGT)
	}

	return query
}

func (s *workgroupService) CountAssignableByCondition(workgroupType string, filter AssignableConditionFilter) (int64, error) {
	var count int64
	if err := s.buildAssignableConditionQuery(workgroupType, filter).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (s *workgroupService) AssignAccountsByCondition(workgroupID uint, filter AssignableConditionFilter, count int, adminID uint) (int64, error) {
	var wg model.Workgroup
	if err := s.db.First(&wg, workgroupID).Error; err != nil {
		return 0, errors.New("工作組不存在")
	}

	// 查詢符合條件的帳號 ID，依 authorized_at 升序取前 count 筆
	var accountIDs []uint
	err := s.buildAssignableConditionQuery(wg.Type, filter).
		Select("a.id").
		Order("COALESCE(a.last_connected, a.created_at) ASC").
		Limit(count).
		Pluck("a.id", &accountIDs).Error
	if err != nil {
		return 0, err
	}

	if len(accountIDs) == 0 {
		return 0, nil
	}

	now := time.Now()
	records := make([]model.WorkgroupAccount, len(accountIDs))
	for i, accountID := range accountIDs {
		records[i] = model.WorkgroupAccount{
			WorkgroupID:   workgroupID,
			AccountID:     accountID,
			WorkgroupType: wg.Type,
			AssignedBy:    adminID,
			AssignedAt:    now,
		}
	}

	if err := s.db.Create(&records).Error; err != nil {
		return 0, fmt.Errorf("批量分配帳號失敗: %v", err)
	}

	return int64(len(records)), nil
}
