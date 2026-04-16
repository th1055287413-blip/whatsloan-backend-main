package auth

import (
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"whatsapp_golang/internal/model"
)

// RBACService 权限管理服务接口
type RBACService interface {
	// 角色管理
	CreateRole(role *model.Role) error
	UpdateRole(id uint, updates map[string]interface{}) error
	DeleteRole(id uint) error
	GetRole(id uint) (*model.Role, error)
	GetRoleList(page, limit int, filters map[string]interface{}) ([]*model.Role, int64, error)
	GetRoleWithPermissions(id uint) (*model.RoleWithPermissions, error)

	// 权限管理
	GetPermissionList(page, limit int, filters map[string]interface{}) ([]*model.Permission, int64, error)
	GetAllPermissions() ([]*model.Permission, error)
	GetPermissionsByModule(module string) ([]*model.Permission, error)

	// 角色权限关联
	AssignPermissionsToRole(roleID uint, permissionIDs []uint) error
	RemovePermissionsFromRole(roleID uint, permissionIDs []uint) error
	GetRolePermissions(roleID uint) ([]*model.Permission, error)

	// 管理員角色关联
	AssignRolesToAdmin(adminID uint, roleIDs []uint, isPrimary bool) error
	RemoveRolesFromAdmin(adminID uint, roleIDs []uint) error
	GetAdminRoles(adminID uint) ([]*model.Role, error)
	GetAdminPermissions(adminID uint) ([]*model.Permission, error)
	CheckAdminPermission(adminID uint, resource, action string) (bool, error)

	// 用户分配管理
	GetAdminUserAssignments(adminID uint) (*model.DataScopeConfig, error)
	AssignUsersToAdmin(adminID uint, userIDs []uint) error
	AssignUserRangeToAdmin(adminID uint, start, end uint) error
	RemoveUserAssignments(adminID uint) error
	CheckUserAccess(adminID uint, userID uint) (bool, error)
}

type rbacService struct {
	db *gorm.DB
}

// NewRBACService 创建权限管理服务实例
func NewRBACService(db *gorm.DB) RBACService {
	return &rbacService{db: db}
}

// CreateRole 创建角色
func (s *rbacService) CreateRole(role *model.Role) error {
	// 检查角色名称是否已存在
	var count int64
	if err := s.db.Model(&model.Role{}).Where("name = ?", role.Name).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("角色名称已存在")
	}

	return s.db.Create(role).Error
}

// UpdateRole 更新角色
func (s *rbacService) UpdateRole(id uint, updates map[string]interface{}) error {
	// 检查角色是否存在
	var role model.Role
	if err := s.db.First(&role, id).Error; err != nil {
		return err
	}

	// 系统预设角色不允许修改某些字段
	if role.IsSystem {
		delete(updates, "is_system")
	}

	return s.db.Model(&role).Updates(updates).Error
}

// DeleteRole 删除角色
func (s *rbacService) DeleteRole(id uint) error {
	// 检查角色是否存在
	var role model.Role
	if err := s.db.First(&role, id).Error; err != nil {
		return err
	}

	// 系统预设角色不允许删除
	if role.IsSystem {
		return errors.New("系统预设角色不允许删除")
	}

	// 检查是否有管理員使用该角色
	var count int64
	if err := s.db.Model(&model.AdminRole{}).Where("role_id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("该角色下还有 %d 個管理員，无法删除", count)
	}

	return s.db.Delete(&role).Error
}

// GetRole 获取角色详情
func (s *rbacService) GetRole(id uint) (*model.Role, error) {
	var role model.Role
	if err := s.db.First(&role, id).Error; err != nil {
		return nil, err
	}

	// 手动加载权限(避免Preload查询中间表的不存在字段)
	var permissions []*model.Permission
	err := s.db.Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Where("role_permissions.role_id = ?", id).
		Find(&permissions).Error
	if err != nil {
		return nil, err
	}

	// 转换为非指针类型
	role.Permissions = make([]model.Permission, len(permissions))
	for i, p := range permissions {
		role.Permissions[i] = *p
	}

	// 统计管理員数量
	s.db.Model(&model.AdminRole{}).Where("role_id = ?", id).Count(&role.UserCount)

	return &role, nil
}

// GetRoleList 获取角色列表
func (s *rbacService) GetRoleList(page, limit int, filters map[string]interface{}) ([]*model.Role, int64, error) {
	var roles []*model.Role
	var total int64

	query := s.db.Model(&model.Role{})

	// 应用筛选条件
	if status, ok := filters["status"].(string); ok && status != "" {
		query = query.Where("status = ?", status)
	}
	if isSystem, ok := filters["is_system"].(bool); ok {
		query = query.Where("is_system = ?", isSystem)
	}
	if keyword, ok := filters["keyword"].(string); ok && keyword != "" {
		query = query.Where("name LIKE ? OR code LIKE ? OR description LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * limit
	if err := query.Offset(offset).Limit(limit).Order("id DESC").Find(&roles).Error; err != nil {
		return nil, 0, err
	}

	// 统计每个角色的管理員数量
	for _, role := range roles {
		s.db.Model(&model.AdminRole{}).Where("role_id = ?", role.ID).Count(&role.UserCount)
	}

	return roles, total, nil
}

// GetRoleWithPermissions 获取角色及其权限
func (s *rbacService) GetRoleWithPermissions(id uint) (*model.RoleWithPermissions, error) {
	role, err := s.GetRole(id)
	if err != nil {
		return nil, err
	}

	var permissionIDs []uint
	for _, perm := range role.Permissions {
		permissionIDs = append(permissionIDs, perm.ID)
	}

	return &model.RoleWithPermissions{
		Role:          *role,
		PermissionIDs: permissionIDs,
	}, nil
}

// GetPermissionList 获取权限列表
func (s *rbacService) GetPermissionList(page, limit int, filters map[string]interface{}) ([]*model.Permission, int64, error) {
	var permissions []*model.Permission
	var total int64

	query := s.db.Model(&model.Permission{})

	// 应用筛选条件
	if module, ok := filters["module"].(string); ok && module != "" {
		query = query.Where("module = ?", module)
	}
	if resource, ok := filters["resource"].(string); ok && resource != "" {
		query = query.Where("resource = ?", resource)
	}
	if keyword, ok := filters["keyword"].(string); ok && keyword != "" {
		query = query.Where("name LIKE ? OR code LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * limit
	if err := query.Offset(offset).Limit(limit).Order("module, id").Find(&permissions).Error; err != nil {
		return nil, 0, err
	}

	return permissions, total, nil
}

// GetAllPermissions 获取所有权限
func (s *rbacService) GetAllPermissions() ([]*model.Permission, error) {
	var permissions []*model.Permission
	if err := s.db.Order("module, id").Find(&permissions).Error; err != nil {
		return nil, err
	}
	return permissions, nil
}

// GetPermissionsByModule 按模块获取权限
func (s *rbacService) GetPermissionsByModule(module string) ([]*model.Permission, error) {
	var permissions []*model.Permission
	if err := s.db.Where("module = ?", module).Order("id").Find(&permissions).Error; err != nil {
		return nil, err
	}
	return permissions, nil
}

// AssignPermissionsToRole 为角色分配权限
func (s *rbacService) AssignPermissionsToRole(roleID uint, permissionIDs []uint) error {
	// 检查角色是否存在
	var role model.Role
	if err := s.db.First(&role, roleID).Error; err != nil {
		return err
	}

	// 删除现有权限关联
	if err := s.db.Where("role_id = ?", roleID).Delete(&model.RolePermission{}).Error; err != nil {
		return err
	}

	// 批量创建新的权限关联
	if len(permissionIDs) > 0 {
		rolePermissions := make([]model.RolePermission, len(permissionIDs))
		for i, permID := range permissionIDs {
			rolePermissions[i] = model.RolePermission{
				RoleID:       roleID,
				PermissionID: permID,
			}
		}

		// 使用批量插入,大幅提升性能
		if err := s.db.Create(&rolePermissions).Error; err != nil {
			return err
		}
	}

	return nil
}

// RemovePermissionsFromRole 移除角色的权限
func (s *rbacService) RemovePermissionsFromRole(roleID uint, permissionIDs []uint) error {
	return s.db.Where("role_id = ? AND permission_id IN ?", roleID, permissionIDs).
		Delete(&model.RolePermission{}).Error
}

// GetRolePermissions 获取角色的所有权限
func (s *rbacService) GetRolePermissions(roleID uint) ([]*model.Permission, error) {
	var permissions []*model.Permission
	err := s.db.Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Where("role_permissions.role_id = ?", roleID).
		Find(&permissions).Error
	return permissions, err
}

// AssignRolesToAdmin 为管理員分配角色
func (s *rbacService) AssignRolesToAdmin(adminID uint, roleIDs []uint, isPrimary bool) error {
	// 删除现有角色关联
	if err := s.db.Where("admin_id = ?", adminID).Delete(&model.AdminRole{}).Error; err != nil {
		return err
	}

	// 批量创建新的角色关联
	if len(roleIDs) > 0 {
		adminRoles := make([]model.AdminRole, len(roleIDs))
		for i, roleID := range roleIDs {
			adminRoles[i] = model.AdminRole{
				AdminID:   adminID,
				RoleID:    roleID,
				IsPrimary: isPrimary && i == 0, // 第一个角色设为主要角色
			}
		}

		// 使用批量插入,大幅提升性能
		if err := s.db.Create(&adminRoles).Error; err != nil {
			return err
		}
	}

	return nil
}

// RemoveRolesFromAdmin 移除管理員的角色
func (s *rbacService) RemoveRolesFromAdmin(adminID uint, roleIDs []uint) error {
	return s.db.Where("admin_id = ? AND role_id IN ?", adminID, roleIDs).
		Delete(&model.AdminRole{}).Error
}

// GetAdminRoles 获取管理員的所有角色
func (s *rbacService) GetAdminRoles(adminID uint) ([]*model.Role, error) {
	var roles []*model.Role
	err := s.db.Joins("JOIN admin_roles ON admin_roles.role_id = roles.id").
		Where("admin_roles.admin_id = ?", adminID).
		Find(&roles).Error
	return roles, err
}

// GetAdminPermissions 获取管理員的所有权限(通过角色)
func (s *rbacService) GetAdminPermissions(adminID uint) ([]*model.Permission, error) {
	var permissions []*model.Permission
	err := s.db.Distinct().
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN admin_roles ON admin_roles.role_id = role_permissions.role_id").
		Where("admin_roles.admin_id = ?", adminID).
		Find(&permissions).Error
	return permissions, err
}

// CheckAdminPermission 检查管理員是否拥有指定权限
func (s *rbacService) CheckAdminPermission(adminID uint, resource, action string) (bool, error) {
	var count int64
	err := s.db.Model(&model.Permission{}).
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN admin_roles ON admin_roles.role_id = role_permissions.role_id").
		Where("admin_roles.admin_id = ? AND permissions.resource = ? AND permissions.action = ?",
			adminID, resource, action).
		Count(&count).Error

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// Helper functions

// SerializeDataScope 序列化数据权限配置
func SerializeDataScope(config *model.DataScopeConfig) (string, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeserializeDataScope 反序列化数据权限配置
func DeserializeDataScope(data string) (*model.DataScopeConfig, error) {
	var config model.DataScopeConfig
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// ========== 用户分配管理方法 ==========

// GetAdminUserAssignments 获取管理员的用户分配范围
func (s *rbacService) GetAdminUserAssignments(adminID uint) (*model.DataScopeConfig, error) {
	var adminRole model.AdminRole
	err := s.db.Where("admin_id = ? AND is_primary = ?", adminID, true).
		First(&adminRole).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // 没有主要角色，返回 nil
		}
		return nil, err
	}

	// 反序列化 data_scope
	if adminRole.DataScope == "" {
		return nil, nil
	}

	config, err := DeserializeDataScope(adminRole.DataScope)
	if err != nil {
		return nil, err
	}

	return config, nil
}

// AssignUsersToAdmin 为管理员分配用户（显式分配）
func (s *rbacService) AssignUsersToAdmin(adminID uint, userIDs []uint) error {
	dataScope := model.DataScopeConfig{
		Type:            "assigned_users",
		AssignedUserIDs: userIDs,
	}

	dataScopeJSON, err := SerializeDataScope(&dataScope)
	if err != nil {
		return err
	}

	return s.db.Model(&model.AdminRole{}).
		Where("admin_id = ? AND is_primary = ?", adminID, true).
		Update("data_scope", dataScopeJSON).Error
}

// AssignUserRangeToAdmin 为管理员分配用户范围（如 1-10）
func (s *rbacService) AssignUserRangeToAdmin(adminID uint, start, end uint) error {
	dataScope := model.DataScopeConfig{
		Type:           "assigned_users",
		UserRangeStart: &start,
		UserRangeEnd:   &end,
	}

	dataScopeJSON, err := SerializeDataScope(&dataScope)
	if err != nil {
		return err
	}

	return s.db.Model(&model.AdminRole{}).
		Where("admin_id = ? AND is_primary = ?", adminID, true).
		Update("data_scope", dataScopeJSON).Error
}

// RemoveUserAssignments 移除用户分配
func (s *rbacService) RemoveUserAssignments(adminID uint) error {
	return s.db.Model(&model.AdminRole{}).
		Where("admin_id = ? AND is_primary = ?", adminID, true).
		Update("data_scope", "").Error
}

// CheckUserAccess 检查管理员是否有权访问指定用户
func (s *rbacService) CheckUserAccess(adminID uint, userID uint) (bool, error) {
	dataScope, err := s.GetAdminUserAssignments(adminID)
	if err != nil {
		return false, err
	}

	// 如果没有配置 data_scope 或类型不是 assigned_users，则允许访问所有用户
	if dataScope == nil || dataScope.Type != "assigned_users" {
		return true, nil
	}

	// 检查显式分配
	if len(dataScope.AssignedUserIDs) > 0 {
		for _, id := range dataScope.AssignedUserIDs {
			if id == userID {
				return true, nil
			}
		}
	}

	// 检查范围分配
	if dataScope.UserRangeStart != nil && dataScope.UserRangeEnd != nil {
		if userID >= *dataScope.UserRangeStart && userID <= *dataScope.UserRangeEnd {
			return true, nil
		}
	}

	return false, nil
}
