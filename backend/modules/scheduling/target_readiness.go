package scheduling

// TargetReadinessCheck is a sanitized, provider-neutral readiness fact used by
// scheduling-authority switch previews. Implementations must not include raw
// provider responses, credentials, or operational configuration values.
type TargetReadinessCheck struct {
	Code     string `json:"code"`
	Ready    bool   `json:"ready"`
	Scope    string `json:"scope,omitempty"`
	EntityID string `json:"entity_id,omitempty"`
}

type TargetReadinessBlocker struct {
	Code     string `json:"code"`
	Scope    string `json:"scope,omitempty"`
	EntityID string `json:"entity_id,omitempty"`
	Message  string `json:"message"`
}

type TargetReadiness struct {
	TargetSchedulingAuthority    string                   `json:"target_scheduling_authority"`
	Ready                        bool                     `json:"ready"`
	AvailabilityReady            bool                     `json:"availability_ready"`
	ExecutionReady               bool                     `json:"execution_ready"`
	AuthorityVersion             int64                    `json:"authority_version"`
	ReadinessEvidenceVersion     int                      `json:"readiness_evidence_version,omitempty"`
	ReadinessEvidenceFingerprint string                   `json:"readiness_evidence_fingerprint,omitempty"`
	ConfigVersion                int64                    `json:"config_version,omitempty"`
	EligibleServiceCount         int                      `json:"eligible_service_count,omitempty"`
	ServiceCount                 int                      `json:"service_count,omitempty"`
	StaffCount                   int                      `json:"staff_count,omitempty"`
	BusinessHourPeriodCount      int                      `json:"business_hour_period_count,omitempty"`
	Checks                       []TargetReadinessCheck   `json:"checks"`
	Blockers                     []TargetReadinessBlocker `json:"-"`
	AvailabilityBlockers         []TargetReadinessBlocker `json:"-"`
	ExecutionBlockers            []TargetReadinessBlocker `json:"-"`
}
