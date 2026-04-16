package content

import (
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

type AiTagDefinitionService interface {
	GetEnabledByCategory(category string) ([]model.AiTagDefinition, error)
	GetAllEnabled() ([]model.AiTagDefinition, error)
	List(page, pageSize int) ([]model.AiTagDefinition, int64, error)
	Create(def *model.AiTagDefinition) error
	Update(def *model.AiTagDefinition) error
	Delete(id uint) error
}

type aiTagDefinitionService struct {
	db *gorm.DB
}

func NewAiTagDefinitionService(db *gorm.DB) AiTagDefinitionService {
	return &aiTagDefinitionService{db: db}
}

func (s *aiTagDefinitionService) GetEnabledByCategory(category string) ([]model.AiTagDefinition, error) {
	var defs []model.AiTagDefinition
	err := s.db.Where("category = ? AND enabled = ?", category, true).
		Order("sort_order ASC").Find(&defs).Error
	return defs, err
}

func (s *aiTagDefinitionService) GetAllEnabled() ([]model.AiTagDefinition, error) {
	var defs []model.AiTagDefinition
	err := s.db.Where("enabled = ?", true).
		Order("category ASC, sort_order ASC").Find(&defs).Error
	return defs, err
}

func (s *aiTagDefinitionService) List(page, pageSize int) ([]model.AiTagDefinition, int64, error) {
	var defs []model.AiTagDefinition
	var total int64
	query := s.db.Model(&model.AiTagDefinition{})
	query.Count(&total)
	err := query.Order("category ASC, sort_order ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&defs).Error
	return defs, total, err
}

func (s *aiTagDefinitionService) Create(def *model.AiTagDefinition) error {
	return s.db.Create(def).Error
}

func (s *aiTagDefinitionService) Update(def *model.AiTagDefinition) error {
	return s.db.Save(def).Error
}

func (s *aiTagDefinitionService) Delete(id uint) error {
	return s.db.Delete(&model.AiTagDefinition{}, id).Error
}

