package model

import (
	"errors"
	"strings"
	"time"
)

// MessageSearchRequest 消息搜索请求
type MessageSearchRequest struct {
	AccountID   uint    `json:"account_id" binding:"omitempty,min=0"` // 0表示搜索所有账号
	Keyword     string  `json:"keyword" binding:"required,min=1,max=100"`
	ChatJID     string  `json:"chat_jid" binding:"max=100"`
	MessageType string  `json:"message_type" binding:"omitempty,oneof=all text image video audio document"`
	DateFrom    string  `json:"date_from" binding:"omitempty"`
	DateTo      string  `json:"date_to" binding:"omitempty"`
	IsFromMe    *bool   `json:"is_from_me"`
	Limit       int     `json:"limit" binding:"omitempty,min=1,max=100"`
	Offset      int     `json:"offset" binding:"omitempty,min=0"`
	SortOrder   string  `json:"sort_order" binding:"omitempty,oneof=desc asc"`
}

// Validate 验证请求参数
func (r *MessageSearchRequest) Validate() error {
	// 设置默认值
	if r.Limit == 0 {
		r.Limit = 20
	}
	if r.SortOrder == "" {
		r.SortOrder = "desc"
	}
	if r.MessageType == "" {
		r.MessageType = "all"
	}

	// 去除关键词首尾空格
	r.Keyword = strings.TrimSpace(r.Keyword)
	if r.Keyword == "" {
		return errors.New("关键词不能为空")
	}

	// 验证日期格式
	if r.DateFrom != "" {
		if _, err := time.Parse(time.RFC3339, r.DateFrom); err != nil {
			return errors.New("开始日期格式错误,请使用ISO8601格式")
		}
	}
	if r.DateTo != "" {
		if _, err := time.Parse(time.RFC3339, r.DateTo); err != nil {
			return errors.New("结束日期格式错误,请使用ISO8601格式")
		}
	}

	return nil
}

// MessageSearchResult 搜索结果
type MessageSearchResult struct {
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Results  []MessageSearchItem  `json:"results"`
}

// MessageSearchItem 搜索结果项
type MessageSearchItem struct {
	ID               uint      `json:"id"`
	AccountID        uint      `json:"account_id"`
	AccountPhone     string    `json:"account_phone"`      // 账号电话号码
	ChatID           uint      `json:"chat_id"`
	ChatJID          string    `json:"chat_jid"`
	ChatName         string    `json:"chat_name"`
	IsGroupChat      bool      `json:"is_group_chat"`      // 是否群组聊天
	MessageID        string    `json:"message_id"`
	FromJID          string    `json:"from_jid"`
	FromName         string    `json:"from_name"`          // 发送者名称
	ToJID            string    `json:"to_jid"`
	Content          string    `json:"content"`
	Type             string    `json:"type"`
	MediaURL         string    `json:"media_url"`
	Timestamp        time.Time `json:"timestamp"`
	IsFromMe         bool      `json:"is_from_me"`
	IsRead           bool      `json:"is_read"`
	SendStatus       string    `json:"send_status"`
	MatchedSnippet   string    `json:"matched_snippet"`
	CreatedAt        time.Time `json:"created_at"`
}

// MessageContextRequest 消息上下文请求
type MessageContextRequest struct {
	MessageID uint `uri:"message_id" binding:"required,min=1"`
	Before    int  `form:"before" binding:"omitempty,min=0,max=20"`
	After     int  `form:"after" binding:"omitempty,min=0,max=20"`
}

// Validate 验证请求参数
func (r *MessageContextRequest) Validate() error {
	// 设置默认值
	if r.Before == 0 {
		r.Before = 3
	}
	if r.After == 0 {
		r.After = 3
	}
	return nil
}

// MessageContextResult 消息上下文结果
type MessageContextResult struct {
	TargetMessage  WhatsAppMessage   `json:"target_message"`
	BeforeMessages []WhatsAppMessage `json:"before_messages"`
	AfterMessages  []WhatsAppMessage `json:"after_messages"`
	ChatInfo       ChatInfo          `json:"chat_info"`
}

// ChatInfo 对话信息
type ChatInfo struct {
	ChatID   uint   `json:"chat_id"`
	ChatJID  string `json:"chat_jid"`
	ChatName string `json:"chat_name"`
	IsGroup  bool   `json:"is_group"`
}
