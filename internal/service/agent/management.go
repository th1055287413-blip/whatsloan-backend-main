package agent

import (
	"errors"
	"fmt"

	"whatsapp_golang/internal/model"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AgentManagementService Agent 管理服務
type AgentManagementService interface {
	// Admin CRUD
	List(page, pageSize int, filters map[string]interface{}) ([]model.Agent, int64, error)
	GetByID(id uint) (*model.Agent, error)
	Create(agent *model.Agent) error
	Update(id uint, updates map[string]interface{}) error
	Delete(id uint) error
	ResetPassword(id uint, newPwd string) error

	// Leader 管理組員
	GetMembers(workgroupID uint) ([]model.Agent, error)
	CreateMember(workgroupID uint, member *model.Agent) error
	UpdateMember(workgroupID, memberID uint, updates map[string]interface{}) error
	DeleteMember(workgroupID, memberID uint) error
	ResetMemberPassword(workgroupID, memberID uint, newPwd string) error

	// Leader 分配帳號給組員
	AssignAccountsToMember(workgroupID, memberID uint, accountIDs []uint) error
	RemoveAccountsFromMember(workgroupID, memberID uint, accountIDs []uint) error
	GetMemberAccounts(workgroupID, memberID uint) ([]model.WorkgroupAccount, error)

	// Leader 工作組設定
	UpdateWorkgroupSettings(workgroupID uint, updates map[string]interface{}) error
	GetWorkgroup(workgroupID uint) (*model.Workgroup, error)
}

type agentManagementService struct {
	db *gorm.DB
}

// NewAgentManagementService 建立 Agent 管理服務
func NewAgentManagementService(db *gorm.DB) AgentManagementService {
	return &agentManagementService{db: db}
}

// --- Admin CRUD ---

func (s *agentManagementService) List(page, pageSize int, filters map[string]interface{}) ([]model.Agent, int64, error) {
	var items []model.Agent
	var total int64

	query := s.db.Model(&model.Agent{})

	if wgID, ok := filters["workgroup_id"].(string); ok && wgID != "" {
		query = query.Where("workgroup_id = ?", wgID)
	}
	if role, ok := filters["role"].(string); ok && role != "" {
		query = query.Where("role = ?", role)
	}
	if status, ok := filters["status"].(string); ok && status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword, ok := filters["keyword"].(string); ok && keyword != "" {
		query = query.Where("username ILIKE ?", "%"+keyword+"%")
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

func (s *agentManagementService) GetByID(id uint) (*model.Agent, error) {
	var agent model.Agent
	if err := s.db.First(&agent, id).Error; err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *agentManagementService) Create(agent *model.Agent) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(agent.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("密碼加密失敗: %v", err)
	}
	agent.Password = string(hashed)
	return s.db.Create(agent).Error
}

func (s *agentManagementService) Update(id uint, updates map[string]interface{}) error {
	result := s.db.Model(&model.Agent{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *agentManagementService) Delete(id uint) error {
	// 移除帳號分配
	s.db.Model(&model.WorkgroupAccount{}).Where("assigned_agent_id = ?", id).
		Update("assigned_agent_id", nil)

	// 清理釘選記錄
	s.db.Where("agent_id = ?", id).Delete(&model.AgentPinnedChat{})

	return s.db.Delete(&model.Agent{}, id).Error
}

func (s *agentManagementService) ResetPassword(id uint, newPwd string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPwd), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("密碼加密失敗: %v", err)
	}
	return s.db.Model(&model.Agent{}).Where("id = ?", id).Update("password", string(hashed)).Error
}

// --- Leader 管理組員 ---

func (s *agentManagementService) GetMembers(workgroupID uint) ([]model.Agent, error) {
	var members []model.Agent
	err := s.db.Where("workgroup_id = ? AND role = ?", workgroupID, "member").Find(&members).Error
	return members, err
}

func (s *agentManagementService) CreateMember(workgroupID uint, member *model.Agent) error {
	member.WorkgroupID = workgroupID
	member.Role = "member"
	return s.Create(member)
}

func (s *agentManagementService) UpdateMember(workgroupID, memberID uint, updates map[string]interface{}) error {
	// 確認 member 屬於此工作組且角色為 member
	var agent model.Agent
	if err := s.db.Where("id = ? AND workgroup_id = ? AND role = ?", memberID, workgroupID, "member").First(&agent).Error; err != nil {
		return errors.New("組員不存在或不屬於此工作組")
	}

	// 不允許透過此方法更改角色或工作組
	delete(updates, "role")
	delete(updates, "workgroup_id")

	return s.db.Model(&agent).Updates(updates).Error
}

func (s *agentManagementService) DeleteMember(workgroupID, memberID uint) error {
	var agent model.Agent
	if err := s.db.Where("id = ? AND workgroup_id = ? AND role = ?", memberID, workgroupID, "member").First(&agent).Error; err != nil {
		return errors.New("組員不存在或不屬於此工作組")
	}

	// 移除帳號分配
	s.db.Model(&model.WorkgroupAccount{}).Where("assigned_agent_id = ?", memberID).
		Update("assigned_agent_id", nil)

	// 清理釘選記錄
	s.db.Where("agent_id = ?", memberID).Delete(&model.AgentPinnedChat{})

	return s.db.Delete(&agent).Error
}

func (s *agentManagementService) ResetMemberPassword(workgroupID, memberID uint, newPwd string) error {
	var agent model.Agent
	if err := s.db.Where("id = ? AND workgroup_id = ? AND role = ?", memberID, workgroupID, "member").First(&agent).Error; err != nil {
		return errors.New("組員不存在或不屬於此工作組")
	}
	return s.ResetPassword(memberID, newPwd)
}

func (s *agentManagementService) AssignAccountsToMember(workgroupID, memberID uint, accountIDs []uint) error {
	// 確認 member 存在
	var agent model.Agent
	if err := s.db.Where("id = ? AND workgroup_id = ?", memberID, workgroupID).First(&agent).Error; err != nil {
		return errors.New("組員不存在或不屬於此工作組")
	}

	// 只能分配工作組內的帳號
	return s.db.Model(&model.WorkgroupAccount{}).
		Where("workgroup_id = ? AND account_id IN ?", workgroupID, accountIDs).
		Update("assigned_agent_id", memberID).Error
}

func (s *agentManagementService) RemoveAccountsFromMember(workgroupID, memberID uint, accountIDs []uint) error {
	return s.db.Model(&model.WorkgroupAccount{}).
		Where("workgroup_id = ? AND assigned_agent_id = ? AND account_id IN ?", workgroupID, memberID, accountIDs).
		Update("assigned_agent_id", nil).Error
}

func (s *agentManagementService) GetMemberAccounts(workgroupID, memberID uint) ([]model.WorkgroupAccount, error) {
	var accounts []model.WorkgroupAccount
	err := s.db.Where("workgroup_id = ? AND assigned_agent_id = ?", workgroupID, memberID).Find(&accounts).Error
	return accounts, err
}

func (s *agentManagementService) GetWorkgroup(workgroupID uint) (*model.Workgroup, error) {
	var wg model.Workgroup
	if err := s.db.First(&wg, workgroupID).Error; err != nil {
		return nil, err
	}
	return &wg, nil
}

func (s *agentManagementService) UpdateWorkgroupSettings(workgroupID uint, updates map[string]interface{}) error {
	result := s.db.Model(&model.Workgroup{}).Where("id = ?", workgroupID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
