package conversation

import "strings"

type partySignal struct {
	IsParty       bool
	PartySize     int
	Groups        []partySignalGroup
	Source        string
	Confidence    float64
	ClarifyReason string
	Evidence      []PartyPlanEvidence
}

type partySignalGroup struct {
	Count               int
	Label               string
	CandidateServiceIDs []string
	ResolvedServiceIDs  []string
	Source              string
}

func detectPartySignal(message string, session Session, serviceUnderstanding serviceUnderstandingResult, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) partySignal {
	intent := extractPartyIntent(message, session, serviceUnderstanding, services, aliases, categoryAliases)
	if !intent.IsParty {
		return partySignal{}
	}
	groups := make([]partySignalGroup, 0, len(intent.Groups))
	for _, group := range intent.Groups {
		groups = append(groups, partySignalGroup{
			Count:               group.Count,
			Label:               group.Label,
			CandidateServiceIDs: append([]string(nil), group.CandidateServiceIDs...),
			ResolvedServiceIDs:  append([]string(nil), group.ResolvedServiceIDs...),
			Source:              group.Source,
		})
	}
	return partySignal{
		IsParty:       true,
		PartySize:     intent.PartySize,
		Groups:        groups,
		Source:        intent.Source,
		Confidence:    intent.Confidence,
		ClarifyReason: intent.ClarifyReason,
		Evidence:      append([]PartyPlanEvidence(nil), intent.Evidence...),
	}
}

func extractPartySize(message string) (int, string, bool) {
	size, source, _, ok := extractPartyPersonCount(normalizeLooseText(message))
	return size, source, ok
}

func partyPlanFromSignal(signal partySignal, session Session) (*PartyPlan, bool) {
	if !signal.IsParty || signal.PartySize < 2 || len(signal.Groups) == 0 {
		return nil, false
	}
	groups := make([]PartyPlanGroup, 0, len(signal.Groups))
	total := 0
	for _, signalGroup := range signal.Groups {
		if signalGroup.Count <= 0 {
			continue
		}
		group := PartyPlanGroup{
			Label:               strings.TrimSpace(signalGroup.Label),
			Count:               signalGroup.Count,
			CandidateServiceIDs: nonEmptyStrings(signalGroup.CandidateServiceIDs),
			ResolvedServiceIDs:  nonEmptyStrings(signalGroup.ResolvedServiceIDs),
			Source:              strings.TrimSpace(signalGroup.Source),
		}
		if group.Label == "" {
			group.Label = "service"
		}
		if len(group.CandidateServiceIDs) == 0 && len(group.ResolvedServiceIDs) > 0 {
			group.CandidateServiceIDs = uniqueStrings(group.ResolvedServiceIDs)
		}
		if len(group.CandidateServiceIDs) == 0 {
			continue
		}
		total += group.Count
		groups = append(groups, group)
	}
	if len(groups) == 0 {
		return nil, false
	}
	plan := &PartyPlan{
		PartySize:       signal.PartySize,
		Groups:          groups,
		ParseSource:     signal.Source,
		ParseConfidence: signal.Confidence,
		ClarifyReason:   signal.ClarifyReason,
		Evidence:        append([]PartyPlanEvidence(nil), signal.Evidence...),
	}
	if total != signal.PartySize && signal.ClarifyReason != partyIntentClarifyReasonServiceCountMismatch {
		return nil, false
	}
	autoResolveSingleCandidatePartyGroups(plan)
	return plan, true
}

func partySignalGroupFromUnderstanding(count int, session Session, result serviceUnderstandingResult, services []ServiceOption) partySignalGroup {
	if count < 2 {
		return partySignalGroup{}
	}
	switch result.Status {
	case serviceUnderstandingStatusSelected:
		service := result.Selected
		if service == nil && len(result.Candidates) > 0 {
			candidate := result.Candidates[0]
			service = &candidate
		}
		if service == nil || strings.TrimSpace(service.ID) == "" {
			break
		}
		serviceID := strings.TrimSpace(service.ID)
		return partySignalGroup{
			Count:               count,
			Label:               firstNonEmpty(service.Name, partyLabelFromServiceUnderstanding(result)),
			CandidateServiceIDs: []string{serviceID},
			ResolvedServiceIDs:  repeatedString(serviceID, count),
			Source:              "selected_service",
		}
	case serviceUnderstandingStatusAmbiguous:
		candidateIDs := serviceIDsFromOptions(result.Candidates)
		if len(candidateIDs) > 0 {
			return partySignalGroup{
				Count:               count,
				Label:               partyLabelFromServiceUnderstanding(result),
				CandidateServiceIDs: candidateIDs,
				Source:              "ambiguous_service",
			}
		}
	}
	if serviceID := strings.TrimSpace(session.ServiceID); serviceID != "" {
		return partySignalGroup{
			Count:               count,
			Label:               firstNonEmpty(session.ServiceName, "service"),
			CandidateServiceIDs: []string{serviceID},
			ResolvedServiceIDs:  repeatedString(serviceID, count),
			Source:              "session_service",
		}
	}
	if len(services) > 0 {
		return partySignalGroup{
			Count:               count,
			Label:               "service",
			CandidateServiceIDs: serviceIDsFromOptions(services),
			Source:              "party_size_only",
		}
	}
	return partySignalGroup{}
}

func partySignalGroupsFromPlanGroups(groups []PartyPlanGroup, source string) []partySignalGroup {
	out := make([]partySignalGroup, 0, len(groups))
	for _, group := range groups {
		out = append(out, partySignalGroup{
			Count:               group.Count,
			Label:               group.Label,
			CandidateServiceIDs: append([]string(nil), group.CandidateServiceIDs...),
			ResolvedServiceIDs:  append([]string(nil), group.ResolvedServiceIDs...),
			Source:              source,
		})
	}
	return out
}

func partyPlanGroupsTotal(groups []PartyPlanGroup) int {
	total := 0
	for _, group := range groups {
		total += group.Count
	}
	return total
}

func partyLabelFromServiceUnderstanding(result serviceUnderstandingResult) string {
	if value := strings.TrimSpace(result.MatchedCategoryName); value != "" {
		return value
	}
	if token := singularServiceToken(result.MatchedToken); token != "" {
		return token
	}
	if len(result.Candidates) == 0 {
		return "service"
	}
	categoryName := commonServiceCategoryName(result.Candidates)
	if categoryName != "" {
		return categoryName
	}
	token := commonServiceNameToken(result.Candidates)
	if token != "" {
		return token
	}
	return "service"
}

func commonServiceCategoryName(services []ServiceOption) string {
	common := ""
	for _, service := range services {
		value := strings.TrimSpace(firstNonEmpty(service.CategoryName, service.CategorySlug))
		if value == "" {
			return ""
		}
		if common == "" {
			common = value
			continue
		}
		if normalizeServiceText(common) != normalizeServiceText(value) {
			return ""
		}
	}
	return common
}

func commonServiceNameToken(services []ServiceOption) string {
	counts := map[string]int{}
	total := 0
	for _, service := range services {
		seen := map[string]bool{}
		for _, token := range serviceNameTokens(service.Name) {
			token = singularServiceToken(token)
			if token == "" || seen[token] {
				continue
			}
			seen[token] = true
			counts[token]++
		}
		total++
	}
	if total == 0 {
		return ""
	}
	for token, count := range counts {
		if count == total {
			return token
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func partySizeFromMessage(message string) int {
	size, _, ok := extractPartySize(message)
	if !ok {
		return 0
	}
	return size
}
