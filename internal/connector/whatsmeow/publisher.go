package whatsmeow

import (
	"context"

	"whatsapp_golang/internal/protocol"
)

// EventPublisher defines the interface for publishing WhatsApp events.
// This interface is implemented by connector.StreamProducer.
type EventPublisher interface {
	PublishMessageReceived(ctx context.Context, accountID uint, payload *protocol.MessageReceivedPayload) error
	PublishMessageSent(ctx context.Context, accountID uint, payload *protocol.MessageSentPayload) error
	PublishReceipt(ctx context.Context, accountID uint, payload *protocol.ReceiptPayload) error
	PublishConnected(ctx context.Context, accountID uint, payload *protocol.ConnectedPayload) error
	PublishDisconnected(ctx context.Context, accountID uint, payload *protocol.DisconnectedPayload) error
	PublishLoggedOut(ctx context.Context, accountID uint, payload *protocol.LoggedOutPayload) error
	PublishQRCode(ctx context.Context, accountID uint, payload *protocol.QRCodePayload) error
	PublishPairingCode(ctx context.Context, accountID uint, payload *protocol.PairingCodePayload) error
	PublishLoginSuccess(ctx context.Context, accountID uint, payload *protocol.LoginSuccessPayload) error
	PublishLoginFailed(ctx context.Context, accountID uint, payload *protocol.LoginFailedPayload) error
	PublishLoginCancelled(ctx context.Context, accountID uint, payload *protocol.LoginCancelledPayload) error
	PublishSyncComplete(ctx context.Context, accountID uint, payload *protocol.SyncCompletePayload) error
	PublishGroupsSync(ctx context.Context, accountID uint, payload *protocol.GroupsSyncPayload) error
	PublishProfileUpdated(ctx context.Context, accountID uint, payload *protocol.ProfileUpdatedPayload) error
	PublishChatsUpdated(ctx context.Context, accountID uint, payload *protocol.ChatsUpdatedPayload) error
	PublishMessageRevoked(ctx context.Context, accountID uint, payload *protocol.MessageRevokedPayload) error
	PublishMessageEdited(ctx context.Context, accountID uint, payload *protocol.MessageEditedPayload) error
	PublishMessageDeletedForMe(ctx context.Context, accountID uint, payload *protocol.MessageDeletedForMePayload) error
	PublishChatArchiveChanged(ctx context.Context, accountID uint, payload *protocol.ChatArchiveChangedPayload) error
	PublishChatArchiveBatch(ctx context.Context, accountID uint, payload *protocol.ChatArchiveBatchPayload) error
	PublishMessageReceivedBatch(ctx context.Context, accountID uint, payloads []*protocol.MessageReceivedPayload) (int, error)
	PublishMediaDownloaded(ctx context.Context, accountID uint, payload *protocol.MediaDownloadedPayload) error
}
