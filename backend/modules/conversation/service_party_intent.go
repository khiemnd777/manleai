package conversation

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/manleai/ai-receptionist/modules/booking"
)

const (
	partyIntentSourcePersonCount       = "person_count"
	partyIntentSourceServiceGroupCount = "service_group_counts"
	partyIntentSourceServiceForPeople  = "service_for_people"
	partyIntentSourceSelectedService   = "selected_service"

	partyIntentClarifyReasonServiceCountMismatch = "party_size_service_count_mismatch"
)

type partyIntent struct {
	IsParty       bool
	PartySize     int
	Groups        []PartyPlanGroup
	Source        string
	Confidence    float64
	ClarifyReason string
	Evidence      []PartyPlanEvidence
}

type partyIntentGroupMatch struct {
	Start     int
	End       int
	Count     int
	Phrase    partyPlanPhrase
	PhraseLen int
	Source    string
}

type partyIntentServiceCountMatch struct {
	Start     int
	End       int
	Count     int
	Service   ServiceOption
	PhraseLen int
}

func extractPartyIntent(message string, session Session, serviceUnderstanding serviceUnderstandingResult, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) partyIntent {
	normalized := normalizeServiceText(message)
	loose := normalizeLooseText(message)
	if normalized == "" && loose == "" {
		return partyIntent{}
	}

	partySize, partySource, personEvidence, hasPartySize := extractPartyPersonCount(loose)
	if hasPartySize && partySize < 2 {
		return partyIntent{}
	}
	groups := partyIntentGroupsFromMessage(message, services, aliases, categoryAliases)
	evidence := make([]PartyPlanEvidence, 0, len(personEvidence)+len(groups))
	evidence = append(evidence, personEvidence...)
	for _, group := range groups {
		evidence = append(evidence, PartyPlanEvidence{
			Kind:       "service_count",
			Source:     group.Source,
			Text:       group.Label,
			Value:      group.Count,
			Confidence: 0.9,
		})
	}

	total := partyPlanGroupsTotal(groups)
	if total > 0 {
		if total < 2 && !hasPartySize {
			return partyIntent{}
		}
		source := partySource
		confidence := 0.95
		if !hasPartySize {
			partySize = total
			source = partyIntentSourceServiceGroupCount
			confidence = 0.85
		}
		if hasPartySize && partySize != total {
			return partyIntent{
				IsParty:       true,
				PartySize:     partySize,
				Groups:        groups,
				Source:        firstNonEmpty(source, partyIntentSourcePersonCount),
				Confidence:    confidence,
				ClarifyReason: partyIntentClarifyReasonServiceCountMismatch,
				Evidence:      evidence,
			}
		}
		return partyIntent{
			IsParty:    true,
			PartySize:  partySize,
			Groups:     groups,
			Source:     firstNonEmpty(source, partyIntentSourceServiceGroupCount),
			Confidence: confidence,
			Evidence:   evidence,
		}
	}

	if !hasPartySize || partySize < 2 {
		return partyIntent{}
	}
	group := partyPlanGroupFromUnderstanding(partySize, session, serviceUnderstanding, services)
	if group.Count <= 0 {
		return partyIntent{}
	}
	return partyIntent{
		IsParty:    true,
		PartySize:  partySize,
		Groups:     []PartyPlanGroup{group},
		Source:     firstNonEmpty(partySource, partyIntentSourcePersonCount),
		Confidence: 0.9,
		Evidence:   evidence,
	}
}

func extractPartyPersonCount(normalized string) (int, string, []PartyPlanEvidence, bool) {
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return 0, "", nil, false
	}
	if value, source, evidence, ok := extractRelationshipPartyCount(normalized); ok {
		return value, source, []PartyPlanEvidence{evidence}, true
	}

	patterns := []struct {
		source  string
		pattern *regexp.Regexp
	}{
		{"party_of", regexp.MustCompile(`\b(?:party|group)\s+of\s+(` + partyCountTokenPattern() + `)\b`)},
		{"for_people", regexp.MustCompile(`\bfor\s+(` + partyCountTokenPattern() + `)\s+(?:person|people|persons|guest|guests|client|clients)\b`)},
		{"people", regexp.MustCompile(`\b(` + partyCountTokenPattern() + `)\s+(?:person|people|persons|guest|guests|client|clients)\b`)},
		{"appointments", regexp.MustCompile(`\b(` + partyCountTokenPattern() + `)\s+appointments\b`)},
	}
	for _, item := range patterns {
		indexes := item.pattern.FindStringSubmatchIndex(normalized)
		if len(indexes) < 4 {
			continue
		}
		token := normalized[indexes[2]:indexes[3]]
		value, ok := partyCountTokenValue(token)
		if !ok {
			continue
		}
		return value, item.source, []PartyPlanEvidence{{
			Kind:       "person_count",
			Source:     item.source,
			Text:       normalized[indexes[0]:indexes[1]],
			Value:      value,
			Start:      indexes[0],
			End:        indexes[1],
			Confidence: 0.95,
		}}, true
	}
	return 0, "", nil, false
}

func extractRelationshipPartyCount(normalized string) (int, string, PartyPlanEvidence, bool) {
	person := `(?:friend|client|guest|sister|brother|mom|mother|dad|father|daughter|son|wife|husband|partner|cousin|aunt|uncle)`
	pluralPerson := `(?:friends|clients|guests|sisters|brothers|daughters|sons|cousins|aunts|uncles)`
	patterns := []struct {
		source       string
		pattern      *regexp.Regexp
		countFromSub int
		fixedCount   int
	}{
		{"me_and_one", regexp.MustCompile(`\b(?:(?:my\s+` + person + `)\s+and\s+i|me\s+(?:and|plus)\s+(?:my\s+)?` + person + `)\b`), -1, 2},
		{"me_and_count", regexp.MustCompile(`\bme\s+(?:and|plus)\s+(` + partyCountTokenPattern() + `)\s+` + pluralPerson + `\b`), 1, 0},
	}
	for _, item := range patterns {
		indexes := item.pattern.FindStringSubmatchIndex(normalized)
		if len(indexes) == 0 {
			continue
		}
		value := item.fixedCount
		if item.countFromSub >= 0 {
			subStart := indexes[item.countFromSub*2]
			subEnd := indexes[item.countFromSub*2+1]
			parsed, ok := partyCountTokenValue(normalized[subStart:subEnd])
			if !ok {
				continue
			}
			value = parsed + 1
		}
		return value, item.source, PartyPlanEvidence{
			Kind:       "person_count",
			Source:     item.source,
			Text:       normalized[indexes[0]:indexes[1]],
			Value:      value,
			Start:      indexes[0],
			End:        indexes[1],
			Confidence: 0.8,
		}, true
	}
	return 0, "", PartyPlanEvidence{}, false
}

func partyIntentGroupsFromMessage(message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) []PartyPlanGroup {
	normalized := normalizeServiceText(message)
	if normalized == "" {
		return nil
	}
	phrases := partyPlanPhrases(services, aliases, categoryAliases)
	matches := make([]partyIntentGroupMatch, 0)
	for _, phrase := range phrases {
		if strings.TrimSpace(phrase.Phrase) == "" || len(phrase.Candidates) == 0 {
			continue
		}
		beforePattern := regexp.MustCompile(`\b(` + partyCountTokenPattern() + `)\s+` + regexp.QuoteMeta(phrase.Phrase) + `\b`)
		for _, indexes := range beforePattern.FindAllStringSubmatchIndex(normalized, -1) {
			if len(indexes) < 4 {
				continue
			}
			count, ok := partyCountTokenValue(normalized[indexes[2]:indexes[3]])
			if !ok || count < 1 {
				continue
			}
			matches = append(matches, partyIntentGroupMatch{
				Start:     indexes[0],
				End:       indexes[1],
				Count:     count,
				Phrase:    phrase,
				PhraseLen: len(phrase.Phrase),
				Source:    partyIntentSourceServiceGroupCount,
			})
		}
		afterPattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(phrase.Phrase) + `\s+(?:for\s+)?(` + partyCountTokenPattern() + `)\s+(?:person|people|persons|guest|guests|client|clients|appointments)\b`)
		for _, indexes := range afterPattern.FindAllStringSubmatchIndex(normalized, -1) {
			if len(indexes) < 4 {
				continue
			}
			count, ok := partyCountTokenValue(normalized[indexes[2]:indexes[3]])
			if !ok || count < 1 {
				continue
			}
			matches = append(matches, partyIntentGroupMatch{
				Start:     indexes[0],
				End:       indexes[1],
				Count:     count,
				Phrase:    phrase,
				PhraseLen: len(phrase.Phrase),
				Source:    partyIntentSourceServiceForPeople,
			})
		}
	}
	return partyPlanGroupsFromIntentMatches(matches)
}

func partyPlanGroupsFromIntentMatches(matches []partyIntentGroupMatch) []PartyPlanGroup {
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Start == matches[j].Start {
			return matches[i].PhraseLen > matches[j].PhraseLen
		}
		return matches[i].Start < matches[j].Start
	})
	accepted := make([]partyIntentGroupMatch, 0, len(matches))
	for _, match := range matches {
		if partyIntentGroupMatchOverlaps(accepted, match) {
			continue
		}
		accepted = append(accepted, match)
	}
	groups := make([]PartyPlanGroup, 0, len(accepted))
	for _, match := range accepted {
		group := PartyPlanGroup{
			Label:               partyPlanGroupLabel(match.Phrase),
			Count:               match.Count,
			CandidateServiceIDs: serviceIDsFromOptions(match.Phrase.Candidates),
			Source:              match.Source,
		}
		if len(group.CandidateServiceIDs) == 1 {
			group.ResolvedServiceIDs = repeatedString(group.CandidateServiceIDs[0], group.Count)
		}
		if group.Count > 0 && len(group.CandidateServiceIDs) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func partyIntentGroupMatchOverlaps(accepted []partyIntentGroupMatch, candidate partyIntentGroupMatch) bool {
	for _, item := range accepted {
		if candidate.Start < item.End && item.Start < candidate.End {
			return true
		}
	}
	return false
}

func partyPlanGroupFromUnderstanding(count int, session Session, result serviceUnderstandingResult, services []ServiceOption) PartyPlanGroup {
	signalGroup := partySignalGroupFromUnderstanding(count, session, result, services)
	if signalGroup.Count <= 0 {
		return PartyPlanGroup{}
	}
	return PartyPlanGroup{
		Label:               strings.TrimSpace(signalGroup.Label),
		Count:               signalGroup.Count,
		CandidateServiceIDs: nonEmptyStrings(signalGroup.CandidateServiceIDs),
		ResolvedServiceIDs:  nonEmptyStrings(signalGroup.ResolvedServiceIDs),
		Source:              firstNonEmpty(signalGroup.Source, partyIntentSourceSelectedService),
	}
}

func partyServiceCountSegmentsFromIntent(message string, services []ServiceOption, session Session) []booking.BookingSegmentRequest {
	normalized := normalizeServiceText(message)
	if normalized == "" {
		return nil
	}
	phrases := partyServicePhrases(services)
	matches := make([]partyIntentServiceCountMatch, 0)
	for _, phrase := range phrases {
		if strings.TrimSpace(phrase.Phrase) == "" || strings.TrimSpace(phrase.Service.ID) == "" {
			continue
		}
		pattern := regexp.MustCompile(`\b(` + partyCountTokenPattern() + `)\s+` + regexp.QuoteMeta(phrase.Phrase) + `\b`)
		for _, indexes := range pattern.FindAllStringSubmatchIndex(normalized, -1) {
			if len(indexes) < 4 {
				continue
			}
			count, ok := partyCountTokenValue(normalized[indexes[2]:indexes[3]])
			if !ok || count < 1 {
				continue
			}
			matches = append(matches, partyIntentServiceCountMatch{
				Start:     indexes[0],
				End:       indexes[1],
				Count:     count,
				Service:   phrase.Service,
				PhraseLen: len(phrase.Phrase),
			})
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Start == matches[j].Start {
			return matches[i].PhraseLen > matches[j].PhraseLen
		}
		return matches[i].Start < matches[j].Start
	})
	accepted := make([]partyIntentServiceCountMatch, 0, len(matches))
	for _, match := range matches {
		if partyIntentServiceCountMatchOverlaps(accepted, match) {
			continue
		}
		accepted = append(accepted, match)
	}
	mode := staffSelectionModeForServiceRequest(session)
	staffID := strings.TrimSpace(session.StaffID)
	if mode == booking.StaffSelectionAnyone {
		staffID = ""
	}
	segments := make([]booking.BookingSegmentRequest, 0)
	for _, match := range accepted {
		for i := 0; i < match.Count; i++ {
			segments = append(segments, booking.BookingSegmentRequest{
				ServiceID:          strings.TrimSpace(match.Service.ID),
				StaffID:            staffID,
				StaffSelectionMode: mode,
			})
		}
	}
	return segments
}

func partyIntentServiceCountMatchOverlaps(accepted []partyIntentServiceCountMatch, candidate partyIntentServiceCountMatch) bool {
	for _, item := range accepted {
		if candidate.Start < item.End && item.Start < candidate.End {
			return true
		}
	}
	return false
}

func partyCountTokenPattern() string {
	return `12|11|10|[1-9]|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|couple|both|a|an`
}

func partyCountTokenValue(token string) (int, bool) {
	token = strings.TrimSpace(strings.ToLower(token))
	switch token {
	case "a", "an", "one":
		return 1, true
	case "two", "couple", "both":
		return 2, true
	case "three":
		return 3, true
	case "four":
		return 4, true
	case "five":
		return 5, true
	case "six":
		return 6, true
	case "seven":
		return 7, true
	case "eight":
		return 8, true
	case "nine":
		return 9, true
	case "ten":
		return 10, true
	case "eleven":
		return 11, true
	case "twelve":
		return 12, true
	}
	value, err := strconv.Atoi(token)
	if err != nil || value < 1 || value > 12 {
		return 0, false
	}
	return value, true
}
