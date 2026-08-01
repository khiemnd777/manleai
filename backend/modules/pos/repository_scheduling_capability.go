package pos

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
)

const squareSchedulingCapabilityContract = "square-buyer-single-create-v1"

type squareCapabilityFence struct {
	ConnectionID       string
	ConnectionVersion  int64
	ConnectionStatus   string
	LocationID         string
	SnapshotGeneration int64
	Scopes             []string
	LastSyncAt         sql.NullTime
	ConfigID           string
	ConfigVersion      int64
	ConfigEnabled      bool
	APIVersion         string
}

func normalizeOAuthScopes(scopes []string) []string {
	unique := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.ToUpper(strings.TrimSpace(scope))
		if scope != "" {
			unique[scope] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for scope := range unique {
		result = append(result, scope)
	}
	sort.Strings(result)
	return result
}

func OAuthScopeFingerprint(scopes []string) string {
	digest := sha256.Sum256([]byte(strings.Join(normalizeOAuthScopes(scopes), " ")))
	return hex.EncodeToString(digest[:])
}

func squareWritePermissionMode(scopes []string) (string, bool, string) {
	normalized := normalizeOAuthScopes(scopes)
	allowedScopes := map[string]struct{}{
		"APPOINTMENTS_READ": {}, "APPOINTMENTS_ALL_READ": {},
		"APPOINTMENTS_WRITE": {}, "APPOINTMENTS_ALL_WRITE": {},
		"APPOINTMENTS_BUSINESS_SETTINGS_READ": {},
		"CUSTOMERS_READ":                      {}, "CUSTOMERS_WRITE": {},
		"ITEMS_READ": {}, "ITEMS_WRITE": {},
		"MERCHANT_PROFILE_READ": {},
		"EMPLOYEES_READ":        {}, "EMPLOYEES_WRITE": {},
	}
	hasBuyerWrite := false
	hasSellerWrite := false
	for _, scope := range normalized {
		if _, known := allowedScopes[scope]; !known {
			return SchedulingWriteModeUnsupported, true, "SQUARE_OAUTH_SCOPES_UNRECOGNIZED"
		}
		switch scope {
		case "APPOINTMENTS_WRITE":
			hasBuyerWrite = true
		case "APPOINTMENTS_ALL_WRITE":
			hasSellerWrite = true
		}
	}
	if hasSellerWrite {
		return SchedulingWriteModeSeller, true, "SQUARE_SELLER_WRITE_UNSAFE"
	}
	if hasBuyerWrite {
		return SchedulingWriteModeBuyer, false, ""
	}
	return SchedulingWriteModeUnsupported, true, "SQUARE_APPOINTMENTS_WRITE_REQUIRED"
}

func (r *Repository) squareCapabilityFence(ctx context.Context, query rowScanner, salonID string) (squareCapabilityFence, error) {
	var item squareCapabilityFence
	err := query.Scan(
		&item.ConnectionID,
		&item.ConnectionVersion,
		&item.ConnectionStatus,
		&item.LocationID,
		&item.SnapshotGeneration,
		pq.Array(&item.Scopes),
		&item.LastSyncAt,
		&item.ConfigID,
		&item.ConfigVersion,
		&item.ConfigEnabled,
		&item.APIVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return squareCapabilityFence{}, ErrNotFound
	}
	return item, err
}

const squareCapabilityFenceQuery = `
	SELECT connection.id::text,connection.booking_write_capability_version,
	       connection.status,COALESCE(connection.location_id,''),connection.snapshot_generation,
	       connection.scopes,connection.last_sync_at,
	       config.id::text,COALESCE(version.version,0),config.enabled,
	       COALESCE(config.settings->>'api_version','')
	FROM pos_connections connection
	JOIN salon_integration_configs config
	  ON config.salon_id=connection.salon_id AND config.provider=connection.provider
	LEFT JOIN technical_resource_versions version
	  ON version.salon_id=config.salon_id
	 AND version.resource_type='integration_config'
	 AND version.resource_id=config.provider
	WHERE connection.salon_id=$1 AND connection.provider='square'
`

func squareCapabilityBlocker(fence squareCapabilityFence) string {
	switch {
	case fence.ConnectionID == "" || fence.ConnectionStatus != StatusActive:
		return "SQUARE_NOT_CONNECTED"
	case strings.TrimSpace(fence.LocationID) == "":
		return "SQUARE_LOCATION_REQUIRED"
	case fence.SnapshotGeneration <= 0 || !fence.LastSyncAt.Valid:
		return "SQUARE_SYNC_REQUIRED"
	case fence.ConfigID == "" || fence.ConfigVersion <= 0 || !fence.ConfigEnabled:
		return "SQUARE_CONFIG_REQUIRED"
	case strings.TrimSpace(fence.APIVersion) == "":
		return "SQUARE_API_VERSION_REQUIRED"
	default:
		_, _, blocker := squareWritePermissionMode(fence.Scopes)
		return blocker
	}
}

func evaluationFromFence(fence squareCapabilityFence) SchedulingCapabilityEvaluation {
	mode, reconnectRequired, scopeBlocker := squareWritePermissionMode(fence.Scopes)
	blocker := squareCapabilityBlocker(fence)
	if blocker == "" {
		blocker = scopeBlocker
	}
	return SchedulingCapabilityEvaluation{
		ConnectionCapabilityVersion: fence.ConnectionVersion,
		IntegrationConfigVersion:    fence.ConfigVersion,
		WritePermissionMode:         mode,
		ReconnectRequired:           reconnectRequired,
		BlockerCode:                 blocker,
	}
}

// GetSquareSchedulingCapabilityEvaluation returns only evidence matching every
// current persisted fence. Missing or stale evidence is represented explicitly
// and never falls back to environment configuration.
func (r *Repository) GetSquareSchedulingCapabilityEvaluation(ctx context.Context, salonID string) (SchedulingCapabilityEvaluation, error) {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" {
		return SchedulingCapabilityEvaluation{}, ErrValidation
	}
	fence, err := r.squareCapabilityFence(ctx, r.db.QueryRowContext(ctx, squareCapabilityFenceQuery, salonID), salonID)
	if err != nil {
		return SchedulingCapabilityEvaluation{}, err
	}
	result := evaluationFromFence(fence)
	var verifiedAt time.Time
	var expiresAt time.Time
	err = r.db.QueryRowContext(ctx, `
		SELECT evidence.id::text,evidence.atomic_create_no_overlap,
		       evidence.atomic_reschedule_no_overlap,evidence.atomic_party_create,
		       evidence.resource_capacity_enforced,evidence.write_permission_mode,
		       evidence.reconnect_required,COALESCE(evidence.blocker_code,''),
		       evidence.verified_at,evidence.expires_at
		FROM external_provider_scheduling_capability_evidence evidence
		WHERE evidence.salon_id=$1
		  AND evidence.provider='square'
		  AND evidence.connection_id=$2
		  AND evidence.connection_capability_version=$3
		  AND evidence.integration_config_id=$4
		  AND evidence.config_version=$5
		  AND evidence.provider_location_id=$6
		  AND evidence.provider_api_version=$7
		  AND evidence.oauth_scope_fingerprint=$8
		  AND evidence.verification_contract_version=$9
		  AND evidence.verified_at <= now() AND evidence.expires_at > now()
		ORDER BY evidence.verified_at DESC,evidence.id DESC
		LIMIT 1
	`, salonID, fence.ConnectionID, fence.ConnectionVersion, fence.ConfigID, fence.ConfigVersion,
		fence.LocationID, fence.APIVersion, OAuthScopeFingerprint(fence.Scopes), squareSchedulingCapabilityContract).Scan(
		&result.EvidenceID,
		&result.AutomaticSingleCreate,
		&result.AutomaticReschedule,
		&result.AutomaticPartyCreate,
		&result.ResourceCapacity,
		&result.WritePermissionMode,
		&result.ReconnectRequired,
		&result.BlockerCode,
		&verifiedAt,
		&expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if result.BlockerCode == "" {
			result.BlockerCode = "SQUARE_CAPABILITY_EVIDENCE_REQUIRED"
		}
		return result, nil
	}
	if err != nil {
		return SchedulingCapabilityEvaluation{}, err
	}
	result.EvidenceCurrent = true
	result.EvidenceVerifiedAt = &verifiedAt
	result.EvidenceExpiresAt = &expiresAt
	return result, nil
}

// ReevaluateSquareSchedulingCapability persists an immutable provider-contract
// review. The client supplies only expected versions and an idempotency key;
// every capability is derived from current database state.
func (r *Repository) ReevaluateSquareSchedulingCapability(ctx context.Context, input SchedulingCapabilityEvaluationInput) (SchedulingCapabilityEvaluation, bool, error) {
	input.SalonID = strings.TrimSpace(input.SalonID)
	input.ActorUserID = strings.TrimSpace(input.ActorUserID)
	input.ActionKey = strings.TrimSpace(input.ActionKey)
	input.RequestFingerprint = strings.TrimSpace(input.RequestFingerprint)
	if input.SalonID == "" || input.ActorUserID == "" || input.ActionKey == "" || len(input.ActionKey) > 256 ||
		len(input.RequestFingerprint) != 64 || input.ExpectedConnectionCapabilityVersion <= 0 || input.ExpectedIntegrationConfigVersion <= 0 {
		return SchedulingCapabilityEvaluation{}, false, ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return SchedulingCapabilityEvaluation{}, false, err
	}
	defer tx.Rollback()
	if err := lockSchedulingMutationFenceTx(ctx, tx, input.SalonID); err != nil {
		return SchedulingCapabilityEvaluation{}, false, err
	}
	var storedFingerprint string
	var storedResponse []byte
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint,response::text
		FROM external_provider_scheduling_capability_actions
		WHERE salon_id=$1 AND action_key=$2
	`, input.SalonID, input.ActionKey).Scan(&storedFingerprint, &storedResponse)
	if err == nil {
		if storedFingerprint != input.RequestFingerprint {
			return SchedulingCapabilityEvaluation{}, false, ErrTechnicalActionConflict
		}
		var replay SchedulingCapabilityEvaluation
		if err := json.Unmarshal(storedResponse, &replay); err != nil {
			return SchedulingCapabilityEvaluation{}, false, err
		}
		return replay, true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SchedulingCapabilityEvaluation{}, false, err
	}
	fence, err := r.squareCapabilityFence(ctx, tx.QueryRowContext(ctx, squareCapabilityFenceQuery+" FOR UPDATE OF connection,config", input.SalonID), input.SalonID)
	if err != nil {
		return SchedulingCapabilityEvaluation{}, false, err
	}
	if fence.ConnectionVersion != input.ExpectedConnectionCapabilityVersion || fence.ConfigVersion != input.ExpectedIntegrationConfigVersion {
		return SchedulingCapabilityEvaluation{}, false, ErrCapabilityVersionConflict
	}

	result := evaluationFromFence(fence)
	mode, reconnectRequired, _ := squareWritePermissionMode(fence.Scopes)
	blocker := squareCapabilityBlocker(fence)
	safeSingleCreate := blocker == "" && mode == SchedulingWriteModeBuyer
	if !safeSingleCreate && blocker == "" {
		blocker = "SQUARE_CAPABILITY_NOT_SAFE"
	}
	verifiedAt := time.Now().UTC()
	expiresAt := verifiedAt.Add(24 * time.Hour)
	var evidenceID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO external_provider_scheduling_capability_evidence(
			salon_id,integration_config_id,provider,provider_location_id,config_version,
			verification_contract_version,verification_source,
			atomic_create_no_overlap,atomic_reschedule_no_overlap,concrete_staff_assignment,
			resource_capacity_enforced,atomic_party_create,verified_at,expires_at,evidence,
			connection_id,connection_capability_version,provider_api_version,
			oauth_scope_fingerprint,write_permission_mode,reviewer_user_id,action_key,
			blocker_code,reconnect_required
		) VALUES(
			$1,$2,'square',$3,$4,$5,'provider_contract',$6,false,$6,false,false,$7,$8,
			jsonb_build_object('contract',$5::text,'blocker_code',NULLIF($9::text,'')),
			$10,$11,$12,$13,$14,$15,$16,NULLIF($9,''),$17
		) RETURNING id::text
	`, input.SalonID, fence.ConfigID, fence.LocationID, fence.ConfigVersion,
		squareSchedulingCapabilityContract, safeSingleCreate, verifiedAt, expiresAt, blocker,
		fence.ConnectionID, fence.ConnectionVersion, fence.APIVersion, OAuthScopeFingerprint(fence.Scopes),
		mode, input.ActorUserID, input.ActionKey, reconnectRequired).Scan(&evidenceID)
	if err != nil {
		return SchedulingCapabilityEvaluation{}, false, err
	}
	result.EvidenceID = evidenceID
	result.AutomaticSingleCreate = safeSingleCreate
	result.AutomaticReschedule = false
	result.AutomaticPartyCreate = false
	result.ResourceCapacity = false
	result.WritePermissionMode = mode
	result.ReconnectRequired = reconnectRequired
	result.EvidenceCurrent = true
	result.EvidenceVerifiedAt = &verifiedAt
	result.EvidenceExpiresAt = &expiresAt
	result.BlockerCode = blocker
	response, err := json.Marshal(result)
	if err != nil {
		return SchedulingCapabilityEvaluation{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO external_provider_scheduling_capability_actions(
			salon_id,action_key,request_fingerprint,evidence_id,actor_user_id,response
		) VALUES($1,$2,$3,$4,$5,$6::jsonb)
	`, input.SalonID, input.ActionKey, input.RequestFingerprint, evidenceID, input.ActorUserID, response); err != nil {
		return SchedulingCapabilityEvaluation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return SchedulingCapabilityEvaluation{}, false, err
	}
	return result, false, nil
}
