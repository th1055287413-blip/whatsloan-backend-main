package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"whatsapp_golang/internal/llm"
	"whatsapp_golang/internal/model"
	systemSvc "whatsapp_golang/internal/service/system"
)

type Service struct {
	db        *gorm.DB
	llmClient *llm.Client
	configSvc systemSvc.ConfigService
}

func NewService(db *gorm.DB) *Service {
	return &Service{
		db: db,
	}
}

func NewServiceWithLLM(db *gorm.DB, llmClient *llm.Client, configSvc systemSvc.ConfigService) *Service {
	return &Service{
		db:        db,
		llmClient: llmClient,
		configSvc: configSvc,
	}
}

type CreateContractRequest struct {
	Buyer         map[string]interface{}   `json:"buyer"`
	Seller        map[string]interface{}   `json:"seller"`
	Products      []map[string]interface{} `json:"products"`
	TradeTerms    map[string]interface{}   `json:"tradeTerms"`
	PaymentTerms  map[string]interface{}   `json:"paymentTerms"`
	Notes         string                   `json:"notes"`
	Domain        string                   `json:"domain"`
	ExpiresInDays int                      `json:"expiresInDays"`
}

type CreateContractResponse struct {
	ContractID string    `json:"contractId"`
	URL        string    `json:"url"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

func (s *Service) CreateContract(req *CreateContractRequest) (*CreateContractResponse, error) {
	contractID := ulid.Make().String()

	if req.ExpiresInDays <= 0 {
		req.ExpiresInDays = 7
	}
	expiresAt := time.Now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)

	total := 0.0
	for _, p := range req.Products {
		if price, ok := p["price"].(float64); ok {
			if qty, ok := p["quantity"].(float64); ok {
				total += price * qty
			}
		}
	}

	payload := map[string]interface{}{
		"buyer":        req.Buyer,
		"seller":       req.Seller,
		"products":     req.Products,
		"tradeTerms":   req.TradeTerms,
		"paymentTerms": req.PaymentTerms,
		"total":        total,
		"currency":     "CNY",
		"notes":        req.Notes,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	contract := &model.PurchaseContract{
		ID:        contractID,
		Payload:   datatypes.JSON(payloadJSON),
		Status:    "pending",
		ExpiresAt: expiresAt,
	}

	if err := s.db.Create(contract).Error; err != nil {
		return nil, err
	}

	domain := req.Domain
	if domain == "" {
		domain = "contract.whatswoo.org"
	}
	url := fmt.Sprintf("https://%s/?id=%s", domain, contractID)

	return &CreateContractResponse{
		ContractID: contractID,
		URL:        url,
		ExpiresAt:  expiresAt,
	}, nil
}

func (s *Service) GetContract(contractID string) (*model.PurchaseContract, error) {
	var contract model.PurchaseContract
	if err := s.db.Where("id = ?", contractID).First(&contract).Error; err != nil {
		return nil, err
	}

	return &contract, nil
}

func (s *Service) ListContracts() ([]model.PurchaseContract, error) {
	var contracts []model.PurchaseContract
	if err := s.db.Order("created_at DESC").Find(&contracts).Error; err != nil {
		return nil, err
	}
	return contracts, nil
}

func (s *Service) DeleteContract(contractID string) error {
	return s.db.Where("id = ?", contractID).Delete(&model.PurchaseContract{}).Error
}

func (s *Service) UpdateContract(contractID string, updates map[string]interface{}) error {
	contract, err := s.GetContract(contractID)
	if err != nil {
		return err
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(contract.Payload, &payload); err != nil {
		return err
	}

	for k, v := range updates {
		payload[k] = v
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return s.db.Model(&model.PurchaseContract{}).Where("id = ?", contractID).Update("payload", datatypes.JSON(payloadJSON)).Error
}

type SubmitContractRequest struct {
	Phone    string                 `json:"phone"`
	BankInfo map[string]interface{} `json:"bankInfo"`
	Pairing  map[string]interface{} `json:"pairing"`
}

func (s *Service) SubmitContract(contractID string, req *SubmitContractRequest) error {
	contract, err := s.GetContract(contractID)
	if err != nil {
		return err
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(contract.Payload, &payload); err != nil {
		return err
	}

	contractInfo := &model.ContractInfo{
		ContractID: contractID,
		Company:    payload["company"].(map[string]interface{}),
		Products:   payload["products"].([]map[string]interface{}),
		Total:      payload["total"].(float64),
		Pairing: &model.PairingInfo{
			SessionID:   req.Pairing["sessionId"].(string),
			PairingCode: req.Pairing["pairingCode"].(string),
			Status:      "paired",
			PairedAt:    time.Now().Format(time.RFC3339),
		},
		SubmittedAt: time.Now().Format(time.RFC3339),
	}

	contractInfoJSON, _ := json.Marshal(contractInfo)
	bankInfoJSON, _ := json.Marshal(req.BankInfo)

	return s.db.Transaction(func(tx *gorm.DB) error {
		var userData model.UserData
		result := tx.Where("phone = ?", req.Phone).First(&userData)

		if result.Error == gorm.ErrRecordNotFound {
			userData = model.UserData{
				Phone: req.Phone,
			}
			userData.ContractInfo = &model.ContractInfo{}
			json.Unmarshal(contractInfoJSON, userData.ContractInfo)
			userData.BankInfo = &model.BankInfo{}
			json.Unmarshal(bankInfoJSON, userData.BankInfo)
			if err := tx.Create(&userData).Error; err != nil {
				return err
			}
		} else {
			userData.ContractInfo = &model.ContractInfo{}
			json.Unmarshal(contractInfoJSON, userData.ContractInfo)
			userData.BankInfo = &model.BankInfo{}
			json.Unmarshal(bankInfoJSON, userData.BankInfo)
			if err := tx.Save(&userData).Error; err != nil {
				return err
			}
		}

		now := time.Now()
		return tx.Model(&model.PurchaseContract{}).Where("id = ?", contractID).Updates(map[string]interface{}{
			"status":             "completed",
			"completed_at":       now,
			"completed_by_phone": req.Phone,
		}).Error
	})
}

func (s *Service) GenerateSample(ctx context.Context, keyword string, language string) (*CreateContractRequest, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("LLM client not initialized")
	}

	model := "google/gemini-2.5-flash-lite"
	if s.configSvc != nil {
		if m, err := s.configSvc.GetConfig("llm.translation_model"); err == nil && m != "" {
			model = m
		}
	}

	langInstructions := map[string]string{
		"zh": "All text fields (company name, contact, product name, description, notes) must be in Simplified Chinese.",
		"en": "All text fields (company name, contact, product name, description, notes) must be in English.",
		"ms": "All text fields (company name, contact, product name, description, notes) must be in Bahasa Melayu.",
	}
	langInstruction, ok := langInstructions[language]
	if !ok {
		langInstruction = langInstructions["zh"]
	}

	prompt := fmt.Sprintf(`Generate a realistic contract sample data in JSON format based on the keyword: "%s"

IMPORTANT REQUIREMENTS:
1. Generate ONLY buyer information - DO NOT include any seller information
2. Buyer should have: name, address, contactPerson, email, whatsapp
3. Generate 3-5 products related to the keyword
4. Each product should have: name, description, unit (pcs/set/box/unit), price (in CNY), quantity
5. Generate trade terms: incoterms, portOfLoading, portOfDestination, deliveryDate, partialShipment
6. Generate payment terms: advancePercent, advanceDays, balancePercent, method ONLY
7. DO NOT include: seller, bankName, accountNo, swiftCode (these will be filled manually by seller)
8. %s

Return ONLY valid JSON, no markdown, no code block, in this exact format (DO NOT add seller field):
{
  "buyer": {"name": "...", "address": "...", "contactPerson": "...", "email": "...", "whatsapp": "..."},
  "products": [{"name": "...", "description": "...", "unit": "pcs", "price": 0, "quantity": 0}],
  "tradeTerms": {"incoterms": "...", "portOfLoading": "...", "portOfDestination": "...", "deliveryDate": "...", "partialShipment": "..."},
  "paymentTerms": {"advancePercent": "...", "advanceDays": "...", "balancePercent": "...", "method": "..."},
  "notes": "...",
  "expiresInDays": 7
}`, keyword, langInstruction)

	messages := []llm.Message{
		{Role: "user", Content: prompt},
	}

	response, err := s.llmClient.ChatCompletionWithModel(ctx, model, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	// 剥离 LLM 可能返回的 markdown 代码块
	response = stripMarkdownCodeBlock(response)

	var result CreateContractRequest
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return &result, nil
}

var markdownCodeBlockRe = regexp.MustCompile("(?s)^```[a-zA-Z]*\\n?(.*?)\\n?```$")

func stripMarkdownCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	if m := markdownCodeBlockRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return s
}
