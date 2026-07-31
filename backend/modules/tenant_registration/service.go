package tenant_registration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

var (
	ErrValidation         = errors.New("tenant registration validation failed")
	ErrNotFound           = errors.New("tenant registration request not found")
	ErrForbidden          = errors.New("tenant registration access forbidden")
	ErrSubmissionConflict = errors.New("tenant registration submission key conflict")
	ErrActionConflict     = errors.New("tenant registration action key conflict")
	ErrVersionConflict    = errors.New("tenant registration version conflict")
	ErrTransition         = errors.New("tenant registration status transition is not permitted")
	ErrTerminal           = errors.New("tenant registration request is terminal")
)

type Store interface {
	Submit(context.Context, string, string, string, PublicSubmissionRequest) (*PublicSubmissionResponse, error)
	List(context.Context, ListFilter) ([]ListItem, map[Status]int64, bool, error)
	Get(context.Context, string) (*Detail, error)
	Mutate(context.Context, string, string, string, MutationRequest) (*MutationResult, error)
	AddNote(context.Context, string, string, string, AddNoteRequest) (*AddNoteResult, error)
	RedactDue(context.Context, int) (int, error)
}

type authorizer interface {
	Authorize(context.Context, middleware.ActorContext, access.AccessCheck) error
}

type Service struct {
	repo   Store
	access authorizer
}

func NewService(repo Store, accessService authorizer) *Service {
	return &Service{repo: repo, access: accessService}
}

func (s *Service) Submit(ctx context.Context, req PublicSubmissionRequest) (*PublicSubmissionResponse, error) {
	if strings.TrimSpace(req.WebsiteConfirmation) != "" {
		return &PublicSubmissionResponse{Status: "received", RequestReference: newPublicReference(), Replayed: false}, nil
	}
	normalized, err := normalizeSubmission(req)
	if err != nil {
		return nil, err
	}
	fingerprint, err := fingerprint(normalized)
	if err != nil {
		return nil, err
	}
	return s.repo.Submit(ctx, uuid.NewString(), newPublicReference(), fingerprint, normalized)
}

func (s *Service) List(ctx context.Context, actor middleware.ActorContext, filter ListFilter) (*ListResponse, error) {
	if err := s.authorize(ctx, actor, access.CapabilityRegistrationRead); err != nil {
		return nil, err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	filter.AssignedTo = strings.TrimSpace(filter.AssignedTo)
	if filter.AssignedTo == "me" {
		filter.AssignedTo = actor.UserID
	}
	if filter.Limit <= 0 {
		filter.Limit = 25
	}
	if filter.Limit > 100 || filter.Offset < 0 || utf8.RuneCountInString(filter.Query) > 120 || (filter.Status != "" && !validStatus(filter.Status)) {
		return nil, ErrValidation
	}
	items, counts, more, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []ListItem{}
	}
	if counts == nil {
		counts = map[Status]int64{}
	}
	return &ListResponse{Requests: items, Counts: counts, Limit: filter.Limit, Offset: filter.Offset, HasMore: more}, nil
}

func (s *Service) Get(ctx context.Context, actor middleware.ActorContext, requestID string) (*Detail, error) {
	if err := s.authorize(ctx, actor, access.CapabilityRegistrationRead); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(strings.TrimSpace(requestID)); err != nil {
		return nil, ErrValidation
	}
	item, err := s.repo.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	item.AllowedTransitions = append([]Status(nil), statusTransitions[item.Status]...)
	return item, nil
}

func (s *Service) Mutate(ctx context.Context, actor middleware.ActorContext, requestID string, req MutationRequest) (*MutationResult, error) {
	if err := s.authorize(ctx, actor, access.CapabilityRegistrationManage); err != nil {
		return nil, err
	}
	requestID = strings.TrimSpace(requestID)
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	if _, err := uuid.Parse(requestID); err != nil || !validActionKey(req.ActionKey) || req.ExpectedVersion < 1 || (req.Status == nil && req.AssignedToUserID == nil && req.ProvisioningDraft == nil) {
		return nil, ErrValidation
	}
	if req.Status != nil && !validStatus(*req.Status) {
		return nil, ErrValidation
	}
	if req.AssignedToUserID != nil {
		value := strings.TrimSpace(*req.AssignedToUserID)
		req.AssignedToUserID = &value
		if value != "" {
			if _, err := uuid.Parse(value); err != nil {
				return nil, ErrValidation
			}
		}
	}
	if req.ProvisioningDraft != nil {
		draft := normalizeProvisioningDraft(*req.ProvisioningDraft)
		if !validProvisioningDraft(draft) {
			return nil, ErrValidation
		}
		req.ProvisioningDraft = &draft
	}
	fp, err := fingerprint(struct {
		RequestID         string             `json:"request_id"`
		ExpectedVersion   int64              `json:"expected_version"`
		Status            *Status            `json:"status"`
		AssignedToUserID  *string            `json:"assigned_to_user_id"`
		ProvisioningDraft *ProvisioningDraft `json:"provisioning_draft"`
	}{requestID, req.ExpectedVersion, req.Status, req.AssignedToUserID, req.ProvisioningDraft})
	if err != nil {
		return nil, err
	}
	return s.repo.Mutate(ctx, actor.UserID, requestID, fp, req)
}

func (s *Service) AddNote(ctx context.Context, actor middleware.ActorContext, requestID string, req AddNoteRequest) (*AddNoteResult, error) {
	if err := s.authorize(ctx, actor, access.CapabilityRegistrationManage); err != nil {
		return nil, err
	}
	requestID, req.ActionKey, req.Content = strings.TrimSpace(requestID), strings.TrimSpace(req.ActionKey), strings.TrimSpace(req.Content)
	if _, err := uuid.Parse(requestID); err != nil || !validActionKey(req.ActionKey) || req.ExpectedVersion < 1 || req.Content == "" || utf8.RuneCountInString(req.Content) > 4000 {
		return nil, ErrValidation
	}
	fp, err := fingerprint(struct {
		RequestID       string `json:"request_id"`
		ExpectedVersion int64  `json:"expected_version"`
		Content         string `json:"content"`
	}{requestID, req.ExpectedVersion, req.Content})
	if err != nil {
		return nil, err
	}
	return s.repo.AddNote(ctx, actor.UserID, requestID, fp, req)
}

func (s *Service) RedactDue(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		return 0, ErrValidation
	}
	return s.repo.RedactDue(ctx, limit)
}

func (s *Service) authorize(ctx context.Context, actor middleware.ActorContext, capability access.Capability) error {
	if s == nil || s.access == nil || actor.PrincipalScope != middleware.PrincipalScopePlatform {
		return ErrForbidden
	}
	if err := s.access.Authorize(ctx, actor, access.AccessCheck{Surface: access.SurfacePlatform, Capability: capability}); err != nil {
		return ErrForbidden
	}
	return nil
}

func AllowedTransitions(status Status) []Status {
	return append([]Status(nil), statusTransitions[status]...)
}
func CanTransition(from, to Status) bool {
	for _, candidate := range statusTransitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}
func validStatus(value Status) bool { _, ok := statusTransitions[value]; return ok }
func terminalStatus(value Status) bool {
	return value == StatusConverted || value == StatusDeclined || value == StatusSpam
}
func validActionKey(value string) bool {
	n := utf8.RuneCountInString(value)
	return n >= 1 && n <= 256 && value == strings.TrimSpace(value)
}

var zipPattern = regexp.MustCompile(`^\d{5}(?:-\d{4})?$`)
var stateCodes = map[string]bool{"AL": true, "AK": true, "AZ": true, "AR": true, "CA": true, "CO": true, "CT": true, "DE": true, "FL": true, "GA": true, "HI": true, "ID": true, "IL": true, "IN": true, "IA": true, "KS": true, "KY": true, "LA": true, "ME": true, "MD": true, "MA": true, "MI": true, "MN": true, "MS": true, "MO": true, "MT": true, "NE": true, "NV": true, "NH": true, "NJ": true, "NM": true, "NY": true, "NC": true, "ND": true, "OH": true, "OK": true, "OR": true, "PA": true, "RI": true, "SC": true, "SD": true, "TN": true, "TX": true, "UT": true, "VT": true, "VA": true, "WA": true, "WV": true, "WI": true, "WY": true, "DC": true}

func normalizeSubmission(req PublicSubmissionRequest) (PublicSubmissionRequest, error) {
	trim := func(v string) string { return strings.TrimSpace(v) }
	req.SubmissionKey, req.ContactFullName, req.ContactEmail, req.ContactPhone = trim(req.SubmissionKey), trim(req.ContactFullName), strings.ToLower(trim(req.ContactEmail)), trim(req.ContactPhone)
	req.SalonName, req.SalonPhone, req.SalonWebsite, req.City = trim(req.SalonName), trim(req.SalonPhone), trim(req.SalonWebsite), trim(req.City)
	req.State, req.ZipCode = strings.ToUpper(trim(req.State)), trim(req.ZipCode)
	req.PreferredContactLanguage, req.CurrentBookingSystem = trim(req.PreferredContactLanguage), trim(req.CurrentBookingSystem)
	req.EstimatedWeeklyCallVolume, req.RequestedHelp, req.Notes = trim(req.EstimatedWeeklyCallVolume), trim(req.RequestedHelp), trim(req.Notes)
	req.Locale, req.SourcePage, req.MarketingPlanInterest, req.ConsentVersion = trim(req.Locale), trim(req.SourcePage), trim(req.MarketingPlanInterest), trim(req.ConsentVersion)
	if _, err := uuid.Parse(req.SubmissionKey); err != nil || !req.ContactConsent || req.ConsentVersion != ConsentVersion {
		return PublicSubmissionRequest{}, ErrValidation
	}
	if address, err := mail.ParseAddress(req.ContactEmail); err != nil || address.Address != req.ContactEmail {
		return PublicSubmissionRequest{}, ErrValidation
	}
	req.ContactEmailNormalized = req.ContactEmail
	var ok bool
	if req.ContactPhoneNormalized, ok = normalizeUSPhone(req.ContactPhone); !ok {
		return PublicSubmissionRequest{}, ErrValidation
	}
	if req.SalonPhoneNormalized, ok = normalizeUSPhone(req.SalonPhone); !ok {
		return PublicSubmissionRequest{}, ErrValidation
	}
	if !stateCodes[req.State] || !zipPattern.MatchString(req.ZipCode) || req.LocationCount < 1 || req.LocationCount > 100 {
		return PublicSubmissionRequest{}, ErrValidation
	}
	if req.PreferredContactLanguage != "en" && req.PreferredContactLanguage != "vi" {
		return PublicSubmissionRequest{}, ErrValidation
	}
	if req.Locale != "en" && req.Locale != "vi" {
		return PublicSubmissionRequest{}, ErrValidation
	}
	if req.SourcePage != "home" && req.SourcePage != "pricing" {
		return PublicSubmissionRequest{}, ErrValidation
	}
	if req.MarketingPlanInterest != "" && req.MarketingPlanInterest != "starter" && req.MarketingPlanInterest != "growth" && req.MarketingPlanInterest != "custom" {
		return PublicSubmissionRequest{}, ErrValidation
	}
	if req.SalonWebsite != "" {
		parsed, err := url.ParseRequestURI(req.SalonWebsite)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return PublicSubmissionRequest{}, ErrValidation
		}
	}
	limits := []struct {
		value string
		max   int
	}{{req.ContactFullName, 160}, {req.ContactEmail, 254}, {req.ContactPhone, 40}, {req.SalonName, 200}, {req.SalonPhone, 40}, {req.SalonWebsite, 500}, {req.City, 120}, {req.CurrentBookingSystem, 160}, {req.EstimatedWeeklyCallVolume, 80}, {req.RequestedHelp, 4000}, {req.Notes, 4000}}
	for _, item := range limits {
		if item.value == "" && (item.max == 160 || item.max == 254 || item.max == 40 || item.max == 200 || item.max == 120) {
			continue
		}
		if utf8.RuneCountInString(item.value) > item.max {
			return PublicSubmissionRequest{}, ErrValidation
		}
	}
	if req.ContactFullName == "" || req.SalonName == "" || req.City == "" {
		return PublicSubmissionRequest{}, ErrValidation
	}
	return req, nil
}

func normalizeUSPhone(value string) (string, bool) {
	var digits strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	result := digits.String()
	if len(result) == 11 && result[0] == '1' {
		result = result[1:]
	}
	if len(result) != 10 || result[0] < '2' || result[3] < '2' {
		return "", false
	}
	return "+1" + result, true
}

func NormalizeUSPhone(value string) (string, bool) { return normalizeUSPhone(value) }
func ValidUSState(value string) bool               { return stateCodes[strings.ToUpper(strings.TrimSpace(value))] }
func ValidUSZIP(value string) bool                 { return zipPattern.MatchString(strings.TrimSpace(value)) }

func normalizeProvisioningDraft(draft ProvisioningDraft) ProvisioningDraft {
	draft.OwnerEmail = strings.ToLower(strings.TrimSpace(draft.OwnerEmail))
	draft.OwnerFullName = strings.TrimSpace(draft.OwnerFullName)
	draft.OwnerPhone = strings.TrimSpace(draft.OwnerPhone)
	draft.SalonName = strings.TrimSpace(draft.SalonName)
	draft.SalonPhone = strings.TrimSpace(draft.SalonPhone)
	draft.Address = strings.TrimSpace(draft.Address)
	draft.City = strings.TrimSpace(draft.City)
	draft.State = strings.ToUpper(strings.TrimSpace(draft.State))
	draft.ZipCode = strings.TrimSpace(draft.ZipCode)
	draft.Timezone = strings.TrimSpace(draft.Timezone)
	draft.PrimaryLanguage = strings.TrimSpace(draft.PrimaryLanguage)
	draft.SecondaryLanguage = strings.TrimSpace(draft.SecondaryLanguage)
	draft.HandoffPhone = strings.TrimSpace(draft.HandoffPhone)
	return draft
}

func validProvisioningDraft(draft ProvisioningDraft) bool {
	address, emailErr := mail.ParseAddress(draft.OwnerEmail)
	_, ownerPhoneOK := normalizeUSPhone(draft.OwnerPhone)
	if draft.OwnerPhone == "" {
		ownerPhoneOK = true
	}
	_, salonPhoneOK := normalizeUSPhone(draft.SalonPhone)
	_, handoffPhoneOK := normalizeUSPhone(draft.HandoffPhone)
	if draft.HandoffPhone == "" {
		handoffPhoneOK = true
	}
	_, timezoneErr := time.LoadLocation(draft.Timezone)
	return emailErr == nil && address.Address == draft.OwnerEmail && utf8.RuneCountInString(draft.OwnerEmail) <= 320 &&
		draft.OwnerFullName != "" && utf8.RuneCountInString(draft.OwnerFullName) <= 160 && ownerPhoneOK &&
		draft.SalonName != "" && utf8.RuneCountInString(draft.SalonName) <= 200 && salonPhoneOK &&
		utf8.RuneCountInString(draft.Address) <= 300 && draft.City != "" && utf8.RuneCountInString(draft.City) <= 120 &&
		ValidUSState(draft.State) && ValidUSZIP(draft.ZipCode) && timezoneErr == nil &&
		(draft.PrimaryLanguage == "en" || draft.PrimaryLanguage == "vi") &&
		(draft.SecondaryLanguage == "en" || draft.SecondaryLanguage == "vi") && handoffPhoneOK
}

func fingerprint(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func newPublicReference() string {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "MR-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:16]
	}
	return "MR-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
}

func ParseListTime(value string, endOfDay bool) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, ErrValidation
	}
	if endOfDay {
		parsed = parsed.Add(24*time.Hour - time.Nanosecond)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}
