package scheduling_behavior

import "github.com/manleai/ai-receptionist/modules/scheduling"

type State struct {
	SchedulingAuthority string                                    `json:"scheduling_authority"`
	AuthorityVersion    int64                                     `json:"authority_version"`
	BookingMode         scheduling.BookingMode                    `json:"booking_mode"`
	PolicyVersion       int64                                     `json:"policy_version"`
	AllowedBookingModes []scheduling.BookingMode                  `json:"allowed_booking_modes"`
	EffectiveBehavior   scheduling.ConversationSchedulingBehavior `json:"effective_behavior"`
}

type PersistedState struct {
	SchedulingAuthority string
	AuthorityVersion    int64
	BookingMode         scheduling.BookingMode
	PolicyVersion       int64
}

type UpdateBookingModeRequest struct {
	BookingMode     scheduling.BookingMode `json:"booking_mode"`
	ExpectedVersion int64                  `json:"expected_version"`
	ActionKey       string                 `json:"action_key"`
}

type UpdateBookingModeCommand struct {
	SalonID            string
	ActorUserID        string
	BookingMode        scheduling.BookingMode
	ExpectedVersion    int64
	ActionKey          string
	RequestFingerprint string
}

type BookingModeMutationResult struct {
	BookingMode scheduling.BookingMode `json:"booking_mode"`
	Version     int64                  `json:"version"`
}
