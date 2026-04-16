package protocol

import "testing"

func TestIsBulkCommand(t *testing.T) {
	bulkCmds := []CommandType{CmdSyncChats, CmdSyncHistory, CmdSyncContacts, CmdUpdateProfile}
	for _, cmd := range bulkCmds {
		if !IsBulkCommand(cmd) {
			t.Errorf("expected %s to be bulk command", cmd)
		}
	}

	priorityCmds := []CommandType{
		CmdSendMessage, CmdSendMedia, CmdConnect, CmdDisconnect,
		CmdGetQRCode, CmdGetPairingCode, CmdCancelLogin,
		CmdRevokeMessage, CmdBindAccount, CmdArchiveChat, CmdDeleteMessageForMe,
	}
	for _, cmd := range priorityCmds {
		if IsBulkCommand(cmd) {
			t.Errorf("expected %s to be priority command", cmd)
		}
	}
}

func TestGetPriorityCommandStreamName(t *testing.T) {
	got := GetPriorityCommandStreamName("conn-1")
	if got != "wa:cmd:priority:conn-1" {
		t.Errorf("got %s", got)
	}
}

func TestGetBulkCommandStreamName(t *testing.T) {
	got := GetBulkCommandStreamName("conn-1")
	if got != "wa:cmd:bulk:conn-1" {
		t.Errorf("got %s", got)
	}
}
