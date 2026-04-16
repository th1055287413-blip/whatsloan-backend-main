package workgroup

import (
	"strings"

	"whatsapp_golang/internal/logger"
	"whatsapp_golang/internal/model"

	"gorm.io/gorm"
)

// AutoAssignAccount 根據渠道或推薦人綁定自動分配帳號到工作組
// 優先級：推薦人綁定 > 渠道綁定
func (s *workgroupService) AutoAssignAccount(accountID uint, channelID *uint, sourceAgentID *uint) error {
	var workgroupID *uint
	var assignSource string

	// 1. 查帳號的推薦人
	var account model.WhatsAppAccount
	if err := s.db.Select("id, referred_by_account_id").First(&account, accountID).Error; err != nil {
		return err
	}

	// 2. 推薦人優先：查推薦人所屬的工作組（從 workgroup_accounts 取第一筆 active 的）
	if account.ReferredByAccountID != nil {
		var wa model.WorkgroupAccount
		err := s.db.Joins("JOIN workgroups ON workgroups.id = workgroup_accounts.workgroup_id AND workgroups.status = ? AND workgroups.deleted_at IS NULL", model.WorkgroupStatusActive).
			Where("workgroup_accounts.account_id = ?", *account.ReferredByAccountID).
			First(&wa).Error
		if err == nil {
			workgroupID = &wa.WorkgroupID
			assignSource = "referral"
			logger.Infow("自動分配：透過推薦人找到工作組", "account_id", accountID, "referred_by", *account.ReferredByAccountID, "workgroup_id", wa.WorkgroupID)
		} else if err == gorm.ErrRecordNotFound {
			logger.Infow("自動分配：推薦人無所屬工作組", "account_id", accountID, "referred_by", *account.ReferredByAccountID)
		} else {
			return err
		}
	} else {
		logger.Debugw("自動分配：帳號無推薦人", "account_id", accountID)
	}

	// 3. 推薦人無綁定 → 查渠道綁定
	if workgroupID == nil && channelID != nil {
		var channel model.Channel
		err := s.db.Select("id, workgroup_id").First(&channel, *channelID).Error
		if err == nil && channel.WorkgroupID != nil {
			workgroupID = channel.WorkgroupID
			assignSource = "channel"
			logger.Infow("自動分配：透過渠道找到工作組", "account_id", accountID, "channel_id", *channelID, "workgroup_id", *channel.WorkgroupID)
		} else if err == nil {
			logger.Infow("自動分配：渠道未綁定工作組", "account_id", accountID, "channel_id", *channelID)
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
	}

	if workgroupID == nil {
		logger.Infow("自動分配：無可分配的工作組", "account_id", accountID, "has_referrer", account.ReferredByAccountID != nil, "has_channel", channelID != nil)
		return nil
	}

	// 4. 驗證工作組存在且 active
	var wg model.Workgroup
	if err := s.db.First(&wg, *workgroupID).Error; err != nil {
		return err
	}
	if wg.Status != model.WorkgroupStatusActive {
		logger.Infow("自動分配：工作組非 active，跳過", "account_id", accountID, "workgroup_id", *workgroupID, "status", wg.Status)
		return nil
	}

	// 5. 呼叫 AssignAccounts，unique constraint violation 靜默忽略
	err := s.AssignAccounts(*workgroupID, []uint{accountID}, model.WorkgroupAutoAssignedBy)
	if err != nil && isUniqueViolation(err) {
		logger.Infow("自動分配：帳號已在該工作組中", "account_id", accountID, "workgroup_id", *workgroupID)
		return nil
	}
	if err != nil {
		return err
	}
	logger.Infow("自動分配成功", "account_id", accountID, "workgroup_id", *workgroupID, "source", assignSource)

	// 6. assigned 模式下，自動分配給引入的業務員
	if sourceAgentID != nil && wg.AccountVisibility == "assigned" {
		var agent model.Agent
		if err := s.db.Where("id = ? AND workgroup_id = ? AND deleted_at IS NULL", *sourceAgentID, *workgroupID).First(&agent).Error; err != nil {
			logger.Infow("自動分配組員：agent 不屬於該工作組，跳過", "account_id", accountID, "agent_id", *sourceAgentID, "workgroup_id", *workgroupID)
			return nil
		}
		if err := s.db.Model(&model.WorkgroupAccount{}).
			Where("workgroup_id = ? AND account_id = ?", *workgroupID, accountID).
			Update("assigned_agent_id", *sourceAgentID).Error; err != nil {
			logger.Warnw("自動分配組員失敗", "account_id", accountID, "agent_id", *sourceAgentID, "error", err)
			return nil
		}
		logger.Infow("自動分配組員成功", "account_id", accountID, "agent_id", *sourceAgentID, "agent_username", agent.Username, "workgroup_id", *workgroupID)
	}

	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
