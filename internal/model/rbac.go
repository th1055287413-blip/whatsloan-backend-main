package model

import (
	"time"
)

// Role 角色模型
type Role struct {
	ID          uint       `json:"id" gorm:"primaryKey"`
	Name        string     `json:"name" gorm:"size:100;not null;uniqueIndex"`
	DisplayName string     `json:"display_name" gorm:"size:100;not null"`
	Description string     `json:"description" gorm:"type:text"`
	IsSystem    bool       `json:"is_system" gorm:"default:false"`
	Status      string     `json:"status" gorm:"size:20;default:active"` // active, disabled
	SortOrder   int        `json:"sort_order" gorm:"default:0"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty" gorm:"index"`

	// 关联
	Permissions []Permission `json:"permissions,omitempty" gorm:"many2many:role_permissions;"`
	UserCount   int64        `json:"user_count" gorm:"-"` // 拥有该角色的用户数量
}

// Permission 权限模型
type Permission struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:100;not null;uniqueIndex"`
	Code        string    `json:"code" gorm:"size:100;not null;uniqueIndex"`
	Resource    string    `json:"resource" gorm:"size:100;not null"` // 资源标识
	Action      string    `json:"action" gorm:"size:50;not null"`    // view, create, update, delete, export, etc.
	Description string    `json:"description" gorm:"type:text"`
	Module      string    `json:"module" gorm:"size:50"` // 所属模块
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// RolePermission 角色权限关联
type RolePermission struct {
	RoleID       uint `json:"role_id" gorm:"primaryKey"`
	PermissionID uint `json:"permission_id" gorm:"primaryKey"`

	Role       Role       `json:"role,omitempty" gorm:"foreignKey:RoleID"`
	Permission Permission `json:"permission,omitempty" gorm:"foreignKey:PermissionID"`
}

// AdminRole 管理員角色關聯
type AdminRole struct {
	ID        uint       `json:"id" gorm:"primaryKey"`
	AdminID   uint       `json:"admin_id" gorm:"not null"`
	RoleID    uint       `json:"role_id" gorm:"not null"`
	GrantedBy *uint      `json:"granted_by"`
	GrantedAt *time.Time `json:"granted_at"`
	IsPrimary bool       `json:"is_primary" gorm:"default:false"`
	DataScope string     `json:"data_scope" gorm:"type:text"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	Role    Role       `json:"role,omitempty" gorm:"foreignKey:RoleID"`
	Admin   *AdminUser `json:"admin,omitempty" gorm:"foreignKey:AdminID"`
	Granter *AdminUser `json:"granter,omitempty" gorm:"foreignKey:GrantedBy"`
}

// DataScopeConfig 数据权限范围配置
type DataScopeConfig struct {
	Type            string   `json:"type"`              // all, custom, dept, self, assigned_users
	Countries       []string `json:"countries"`         // 可见国家
	Tags            []uint   `json:"tags"`              // 可见标签ID
	Expression      string   `json:"expression"`        // 自定义表达式
	AssignedUserIDs []uint   `json:"assigned_user_ids"` // 显式分配的用户 ID
	UserRangeStart  *uint    `json:"user_range_start"`  // 范围分配起始 ID
	UserRangeEnd    *uint    `json:"user_range_end"`    // 范围分配结束 ID
}

// RoleWithPermissions 角色及其权限
type RoleWithPermissions struct {
	Role
	PermissionIDs []uint `json:"permission_ids"`
}

// UserWithRoles 用户及其角色
type UserWithRoles struct {
	UserID    uint             `json:"user_id"`
	Username  string           `json:"username"`
	Roles     []Role           `json:"roles"`
	DataScope *DataScopeConfig `json:"data_scope,omitempty"`
}

// TableName 指定表名
func (Role) TableName() string {
	return "roles"
}

func (Permission) TableName() string {
	return "permissions"
}

func (RolePermission) TableName() string {
	return "role_permissions"
}

func (AdminRole) TableName() string {
	return "admin_roles"
}
