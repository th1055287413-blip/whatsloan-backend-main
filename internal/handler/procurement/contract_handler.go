package procurement

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Contract struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Number      string `json:"number"`
	Status      string `json:"status"`
	Supplier    string `json:"supplier"`
	ValidUntil  string `json:"validUntil"`
	BudgetLabel string `json:"budgetLabel"`
	BudgetValue string `json:"budgetValue"`
}

type LineItem struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	SKU          string  `json:"sku"`
	Price        float64 `json:"price"`
	UOM          string  `json:"uom"`
	MinQty       int     `json:"minQty"`
	MaxQty       int     `json:"maxQty"`
	LeadTime     string  `json:"leadTime"`
	Availability string  `json:"availability"`
}

type Order struct {
	ID          string      `json:"id"`
	OrderNumber string      `json:"orderNumber"`
	ContractRef string      `json:"contractRef"`
	Total       float64     `json:"total"`
	Status      string      `json:"status"`
	SubmittedAt time.Time   `json:"submittedAt"`
	Items       []OrderItem `json:"items"`
}

type OrderItem struct {
	ItemID   int     `json:"itemId"`
	Name     string  `json:"name"`
	SKU      string  `json:"sku"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
}

type ContractHandler struct{}

func NewContractHandler() *ContractHandler {
	return &ContractHandler{}
}

func (h *ContractHandler) GetContracts(c *gin.Context) {
	contracts := []Contract{
		{ID: 1, Title: "IT Equipment & Software", Number: "CT-2024-001", Status: "Active", Supplier: "TechCorp Solutions", ValidUntil: "Dec 31, 2024", BudgetLabel: "Remaining Budget", BudgetValue: "$45,250"},
		{ID: 2, Title: "Office Supplies & Stationery", Number: "CT-2024-002", Status: "Expiring Soon", Supplier: "Office Supplies Inc", ValidUntil: "Jan 15, 2025", BudgetLabel: "Remaining Budget", BudgetValue: "$12,800"},
		{ID: 3, Title: "Maintenance & Facility Services", Number: "CT-2024-003", Status: "Active", Supplier: "FacilityPro Services", ValidUntil: "Jun 30, 2025", BudgetLabel: "Service Hours", BudgetValue: "850 hrs remaining"},
		{ID: 4, Title: "Industrial Equipment Rental", Number: "CT-2024-004", Status: "Active", Supplier: "Industrial Equipment Co", ValidUntil: "Mar 31, 2025", BudgetLabel: "Rental Credits", BudgetValue: "$28,500"},
		{ID: 5, Title: "Catering & Food Services", Number: "CT-2023-005", Status: "Expired", Supplier: "Fresh Catering Co", ValidUntil: "Dec 15, 2024", BudgetLabel: "Final Budget", BudgetValue: "$0"},
	}
	c.JSON(http.StatusOK, gin.H{"data": contracts})
}

func (h *ContractHandler) GetContractDetails(c *gin.Context) {
	id := c.Param("id")

	contract := Contract{
		ID: 1, Title: "IT Equipment & Software", Number: "CT-2024-001",
		Status: "Active", Supplier: "TechCorp Solutions", ValidUntil: "Dec 31, 2024",
	}

	lineItems := []LineItem{
		{ID: 1, Name: "Dell Laptop - Latitude 5520", SKU: "DL-LAT-5520-i7", Price: 1245.00, UOM: "Each", MinQty: 1, MaxQty: 50, LeadTime: "3-5 days", Availability: "In Stock"},
		{ID: 2, Name: "Microsoft Office 365 License", SKU: "MS-O365-BUS-PREM", Price: 22.00, UOM: "Per User/Month", MinQty: 5, MaxQty: 500, LeadTime: "1-2 days", Availability: "In Stock"},
		{ID: 3, Name: "HP LaserJet Pro Printer", SKU: "HP-LJ-PRO-M404n", Price: 279.00, UOM: "Each", MinQty: 1, MaxQty: 3, LeadTime: "2-4 days", Availability: "Limited"},
	}

	c.JSON(http.StatusOK, gin.H{"contract": contract, "lineItems": lineItems, "id": id})
}

func (h *ContractHandler) SubmitOrder(c *gin.Context) {
	var req struct {
		Items        []OrderItem `json:"items"`
		Total        float64     `json:"total"`
		Address      string      `json:"address"`
		DeliveryDate string      `json:"deliveryDate"`
		CostCenter   string      `json:"costCenter"`
		Notes        string      `json:"notes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	order := Order{
		OrderNumber: "PO-2024-0847",
		ContractRef: "CT-2024-001",
		Total:       req.Total,
		Status:      "pending",
		SubmittedAt: time.Now(),
		Items:       req.Items,
	}

	c.JSON(http.StatusOK, gin.H{"order": order})
}
