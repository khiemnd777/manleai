package schedulingretention

import "context"

const (
	PolicyVersion       = 1
	DefaultProcessBatch = 100
	MaxProcessBatch     = 500

	KindSchedulingRequest       = "scheduling_request"
	KindOwnerRetentionExpiry    = "owner_notification_retention_expiry"
	KindCustomerRetentionExpiry = "customer_notification_retention_expiry"
	KindOwnerNotification       = "owner_notification"
	KindCustomerNotification    = "customer_notification"
	KindVoiceAudio              = "voice_audio"
)

type Store interface {
	ProcessNext(ctx context.Context, kind string) (bool, error)
}
