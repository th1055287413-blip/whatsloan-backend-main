package model

import (
	"gorm.io/datatypes"
	"time"
)

type PurchaseContract struct {
	ID               string         `gorm:"primaryKey;type:varchar(26)" json:"id"`
	Payload          datatypes.JSON `gorm:"type:jsonb;not null" json:"payload"`
	Status           string         `gorm:"type:varchar(20);default:'pending'" json:"status"`
	ExpiresAt        time.Time      `gorm:"not null" json:"expiresAt"`
	CreatedAt        time.Time      `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime" json:"updatedAt"`
	CompletedAt      *time.Time     `json:"completedAt,omitempty"`
	CompletedByPhone string         `gorm:"type:varchar(20)" json:"completedByPhone,omitempty"`
}

func (PurchaseContract) TableName() string {
	return "purchase_contracts"
}
