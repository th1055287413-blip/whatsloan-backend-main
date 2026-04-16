package common

import (
	"time"

	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"
)

// ChatSourceInfo 帳號來源資訊（可選，注入到每個 chat response）
type ChatSourceInfo struct {
	SourceType      string
	SourceAgentName *string
}

// BuildChatsResponse 組裝聊天列表回應（admin / agent 共用）
// sourceInfo 可為 nil，不注入 source 欄位
func BuildChatsResponse(chats []*model.WhatsAppChat, total int64, page, pageSize int, accountID uint, chatTagService contentSvc.ChatTagService, sourceInfo ...*ChatSourceInfo) map[string]interface{} {
	chatJIDs := make([]string, 0, len(chats))
	chatNumIDs := make([]uint, 0, len(chats))
	for _, chat := range chats {
		chatJIDs = append(chatJIDs, chat.JID)
		chatNumIDs = append(chatNumIDs, chat.ID)
	}

	chatTags := make(map[string][]string)
	chatSummaries := make(map[uint]string)
	if chatTagService != nil && len(chatJIDs) > 0 {
		if tags, err := chatTagService.GetTagsByChatIDs(accountID, chatJIDs); err == nil {
			chatTags = tags
		}
		if sums, err := chatTagService.GetSummariesByChatIDs(accountID, chatNumIDs); err == nil {
			chatSummaries = sums
		}
	}

	chatResponses := make([]map[string]interface{}, 0, len(chats))
	for _, chat := range chats {
		chatResponse := map[string]interface{}{
			"id":           chat.ID,
			"jid":          chat.JID,
			"phone_jid":    chat.PhoneJID,
			"name":         chat.Name,
			"avatar":       chat.Avatar,
			"last_message": chat.LastMessage,
			"last_time":    chat.LastTime.Format(time.RFC3339),
			"unread_count": chat.UnreadCount,
			"is_group":     chat.IsGroup,
			"participants": chat.Participants,
			"archived":     chat.Archived,
			"created_at":   chat.CreatedAt.Format(time.RFC3339),
			"updated_at":   chat.UpdatedAt.Format(time.RFC3339),
		}
		if chat.ArchivedAt != nil {
			chatResponse["archived_at"] = chat.ArchivedAt.Format(time.RFC3339)
		} else {
			chatResponse["archived_at"] = nil
		}

		if tags, ok := chatTags[chat.JID]; ok {
			chatResponse["tags"] = tags
		} else {
			chatResponse["tags"] = []string{}
		}

		if summary, ok := chatSummaries[chat.ID]; ok {
			chatResponse["ai_summary"] = summary
		} else {
			chatResponse["ai_summary"] = nil
		}

		if len(sourceInfo) > 0 && sourceInfo[0] != nil {
			chatResponse["source_type"] = sourceInfo[0].SourceType
			if sourceInfo[0].SourceAgentName != nil {
				chatResponse["source_agent_name"] = *sourceInfo[0].SourceAgentName
			}
		}

		chatResponses = append(chatResponses, chatResponse)
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	return map[string]interface{}{
		"chats":       chatResponses,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	}
}

// BuildMessagesResponse 組裝訊息列表回應（admin / agent 共用）
func BuildMessagesResponse(messages []*model.MessageWithSender, total int64, page, pageSize int, contactPhone string) map[string]interface{} {
	messageResponses := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		messageResponse := map[string]interface{}{
			"id":            msg.ID,
			"message_id":    msg.MessageID,
			"from_jid":      msg.FromJID,
			"to_jid":        msg.ToJID,
			"content":       msg.Content,
			"original_text": msg.OriginalText,
			"type":          msg.Type,
			"media_url":     msg.MediaURL,
			"is_from_me":    msg.IsFromMe,
			"timestamp":     msg.Timestamp.Format(time.RFC3339),
			"send_status":   msg.SendStatus,
			"created_at":    msg.CreatedAt.Format(time.RFC3339),
			"sender":        msg.Sender,
			"is_revoked":    msg.IsRevoked,
			"is_edited":     msg.IsEdited,
		}

		if msg.TranslatedText != "" {
			messageResponse["translated_text"] = msg.TranslatedText
			messageResponse["cached"] = true
		}

		if msg.RevokedAt != nil {
			messageResponse["revoked_at"] = msg.RevokedAt.Format(time.RFC3339)
		}
		if msg.EditedAt != nil {
			messageResponse["edited_at"] = msg.EditedAt.Format(time.RFC3339)
		}
		if msg.DeletedAt != nil {
			messageResponse["deleted_at"] = msg.DeletedAt.Format(time.RFC3339)
		}
		if msg.DeletedBy != "" {
			messageResponse["deleted_by"] = msg.DeletedBy
		}

		messageResponses = append(messageResponses, messageResponse)
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	return map[string]interface{}{
		"messages":      messageResponses,
		"total":         total,
		"page":          page,
		"limit":         pageSize,
		"total_pages":   totalPages,
		"contact_phone": contactPhone,
	}
}
