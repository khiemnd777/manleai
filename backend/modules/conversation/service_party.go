package conversation

import (
	"fmt"
	"github.com/manleai/ai-receptionist/modules/booking"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type partyBookingPlan struct {
	PartySize int
	Segments  []booking.BookingSegmentRequest
}

type partyServiceCountMatch struct {
	Start     int
	End       int
	Count     int
	Service   ServiceOption
	PhraseLen int
}

type partyServicePhrase struct {
	Service ServiceOption
	Phrase  string
	Family  bool
}

func partyBookingPlanFromMessage(message string, services []ServiceOption, session Session) (partyBookingPlan, bool) {
	partySize := partySizeFromMessage(message)
	counted := partyServiceCountSegmentsFromMessage(message, services, session)
	if len(counted) > 0 {
		if partySize == 0 && len(counted) < 2 {
			return partyBookingPlan{}, false
		}
		if partySize > 0 && len(counted) != partySize {
			return partyBookingPlan{}, false
		}
		if partySize == 0 {
			partySize = len(counted)
		}
		return partyBookingPlan{PartySize: partySize, Segments: counted}, true
	}
	if partySize < 2 {
		return partyBookingPlan{}, false
	}
	service, ok := singlePartyServiceFromMessage(message, services)
	if !ok {
		if strings.TrimSpace(session.ServiceID) == "" {
			return partyBookingPlan{}, false
		}
		service = ServiceOption{ID: session.ServiceID, Name: session.ServiceName}
	}
	segments := partySegmentsForService(service, partySize, session)
	if len(segments) != partySize {
		return partyBookingPlan{}, false
	}
	return partyBookingPlan{PartySize: partySize, Segments: segments}, true
}

type partyPlanPhrase struct {
	Phrase     string
	Label      string
	Candidates []ServiceOption
}

type partyPlanCountMatch struct {
	Start     int
	End       int
	Count     int
	Phrase    partyPlanPhrase
	PhraseLen int
}

type partyPlanServiceSelection struct {
	Start     int
	End       int
	Service   ServiceOption
	PhraseLen int
}

func partyPlanFromMessage(message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, session Session) (*PartyPlan, bool) {
	serviceUnderstanding := interpretServiceForSession(message, session, services, aliases, categoryAliases)
	signal := detectPartySignal(message, session, serviceUnderstanding, services, aliases, categoryAliases)
	if plan, ok := partyPlanFromSignal(signal, session); ok {
		return plan, true
	}
	partySize := partySizeFromMessage(message)
	groups := partyPlanGroupsFromMessage(message, services, aliases, categoryAliases)
	if len(groups) > 0 {
		total := 0
		for _, group := range groups {
			total += group.Count
		}
		if total < 2 {
			return nil, false
		}
		if partySize > 0 && total != partySize {
			return nil, false
		}
		if partySize == 0 {
			partySize = total
		}
		plan := &PartyPlan{PartySize: partySize, Groups: groups}
		autoResolveSingleCandidatePartyGroups(plan)
		return plan, true
	}
	if plan, ok := partyBookingPlanFromMessage(message, services, session); ok {
		return completedPartyPlanFromSegments(plan, services), true
	}
	return nil, false
}

func partyPlanGroupsFromMessage(message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) []PartyPlanGroup {
	normalized := normalizeServiceText(message)
	if normalized == "" {
		return nil
	}
	phrases := partyPlanPhrases(services, aliases, categoryAliases)
	matches := make([]partyPlanCountMatch, 0)
	for _, phrase := range phrases {
		if strings.TrimSpace(phrase.Phrase) == "" || len(phrase.Candidates) == 0 {
			continue
		}
		beforePattern := regexp.MustCompile(`\b(` + partyCountTokenPattern() + `)\s+` + regexp.QuoteMeta(phrase.Phrase) + `\b`)
		for _, indexes := range beforePattern.FindAllStringSubmatchIndex(normalized, -1) {
			if len(indexes) < 4 {
				continue
			}
			countToken := normalized[indexes[2]:indexes[3]]
			count, ok := partyCountTokenValue(countToken)
			if !ok || count < 1 {
				continue
			}
			matches = append(matches, partyPlanCountMatch{
				Start:     indexes[0],
				End:       indexes[1],
				Count:     count,
				Phrase:    phrase,
				PhraseLen: len(phrase.Phrase),
			})
		}
		afterPattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(phrase.Phrase) + `\s+(?:for\s+)?(` + partyCountTokenPattern() + `)\s+(?:people|persons|guests|clients|appointments)\b`)
		for _, indexes := range afterPattern.FindAllStringSubmatchIndex(normalized, -1) {
			if len(indexes) < 4 {
				continue
			}
			countToken := normalized[indexes[2]:indexes[3]]
			count, ok := partyCountTokenValue(countToken)
			if !ok || count < 1 {
				continue
			}
			matches = append(matches, partyPlanCountMatch{
				Start:     indexes[0],
				End:       indexes[1],
				Count:     count,
				Phrase:    phrase,
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
	accepted := make([]partyPlanCountMatch, 0, len(matches))
	for _, match := range matches {
		if partyPlanCountMatchOverlaps(accepted, match) {
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

func partyPlanPhrases(services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) []partyPlanPhrase {
	servicesByID := map[string]ServiceOption{}
	categoryServices := map[string][]ServiceOption{}
	tokenServices := map[string][]ServiceOption{}
	for _, service := range services {
		serviceID := strings.TrimSpace(service.ID)
		if serviceID == "" {
			continue
		}
		servicesByID[serviceID] = service
		if categoryID := strings.TrimSpace(service.CategoryID); categoryID != "" {
			categoryServices[categoryID] = appendUniqueService(categoryServices[categoryID], service)
		}
		for _, token := range serviceNameTokens(service.Name) {
			tokenServices[singularServiceToken(token)] = appendUniqueService(tokenServices[singularServiceToken(token)], service)
		}
	}
	seen := map[string]bool{}
	phrases := make([]partyPlanPhrase, 0)
	addPhrase := func(label string, phrase string, candidates []ServiceOption) {
		phrase = normalizeServiceText(phrase)
		candidates = orderedServices(candidates)
		if phrase == "" || len(candidates) == 0 {
			return
		}
		key := phrase + "\x00" + strings.Join(serviceIDsFromOptions(candidates), ",")
		if seen[key] {
			return
		}
		seen[key] = true
		phrases = append(phrases, partyPlanPhrase{
			Phrase:     phrase,
			Label:      strings.TrimSpace(label),
			Candidates: candidates,
		})
	}
	for _, service := range services {
		name := strings.TrimSpace(service.Name)
		if strings.TrimSpace(service.ID) == "" || name == "" {
			continue
		}
		addPhrase(name, name, []ServiceOption{service})
		addPhrase(name, pluralServicePhrase(name), []ServiceOption{service})
	}
	for _, alias := range aliases {
		service, ok := servicesByID[strings.TrimSpace(alias.ServiceID)]
		if !ok {
			continue
		}
		phrase := strings.TrimSpace(alias.NormalizedAlias)
		if phrase == "" {
			phrase = alias.Alias
		}
		addPhrase(service.Name, phrase, []ServiceOption{service})
		addPhrase(service.Name, pluralServicePhrase(phrase), []ServiceOption{service})
	}
	categoryLabels := map[string]string{}
	for categoryID, items := range categoryServices {
		label := ""
		for _, item := range items {
			if item.CategoryName != "" {
				label = item.CategoryName
				break
			}
		}
		if label == "" {
			continue
		}
		categoryLabels[categoryID] = label
		addPhrase(label, label, items)
		addPhrase(label, pluralServicePhrase(label), items)
	}
	for _, alias := range categoryAliases {
		items := categoryServices[strings.TrimSpace(alias.CategoryID)]
		if len(items) == 0 {
			continue
		}
		label := strings.TrimSpace(alias.CategoryName)
		if label == "" {
			label = categoryLabels[strings.TrimSpace(alias.CategoryID)]
		}
		if label == "" {
			label = alias.Alias
		}
		phrase := strings.TrimSpace(alias.NormalizedAlias)
		if phrase == "" {
			phrase = alias.Alias
		}
		addPhrase(label, phrase, items)
		addPhrase(label, pluralServicePhrase(phrase), items)
	}
	for token, items := range tokenServices {
		if token == "" {
			continue
		}
		addPhrase(token, token, items)
		addPhrase(token, pluralServicePhrase(token), items)
	}
	sort.SliceStable(phrases, func(i, j int) bool {
		if len(phrases[i].Phrase) == len(phrases[j].Phrase) {
			return phrases[i].Phrase < phrases[j].Phrase
		}
		return len(phrases[i].Phrase) > len(phrases[j].Phrase)
	})
	return phrases
}

func partyPlanGroupLabel(phrase partyPlanPhrase) string {
	label := normalizeServiceText(phrase.Label)
	if label == "" {
		label = normalizeServiceText(phrase.Phrase)
	}
	parts := strings.Fields(label)
	if len(parts) == 0 {
		return "service"
	}
	return singularServiceToken(parts[len(parts)-1])
}

func partyPlanCountMatchOverlaps(accepted []partyPlanCountMatch, candidate partyPlanCountMatch) bool {
	for _, item := range accepted {
		if candidate.Start < item.End && item.Start < candidate.End {
			return true
		}
	}
	return false
}

func completedPartyPlanFromSegments(plan partyBookingPlan, services []ServiceOption) *PartyPlan {
	if len(plan.Segments) == 0 {
		return nil
	}
	groups := make([]PartyPlanGroup, 0, len(plan.Segments))
	for _, segment := range plan.Segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		label := serviceName(serviceID, services, "service")
		if len(groups) > 0 {
			last := &groups[len(groups)-1]
			if len(last.ResolvedServiceIDs) > 0 && last.ResolvedServiceIDs[0] == serviceID {
				last.Count++
				last.ResolvedServiceIDs = append(last.ResolvedServiceIDs, serviceID)
				continue
			}
		}
		groups = append(groups, PartyPlanGroup{
			Label:               label,
			Count:               1,
			CandidateServiceIDs: []string{serviceID},
			ResolvedServiceIDs:  []string{serviceID},
		})
	}
	if len(groups) == 0 {
		return nil
	}
	partySize := plan.PartySize
	if partySize == 0 {
		for _, group := range groups {
			partySize += group.Count
		}
	}
	return &PartyPlan{PartySize: partySize, Groups: groups}
}

func activePartyPlan(plan *PartyPlan) bool {
	return plan != nil && (plan.PartySize > 0 || len(plan.Groups) > 0)
}

func clonePartyPlan(plan *PartyPlan) *PartyPlan {
	if plan == nil {
		return nil
	}
	out := &PartyPlan{
		PartySize:              plan.PartySize,
		Groups:                 make([]PartyPlanGroup, 0, len(plan.Groups)),
		SplitOptions:           make([]PartySplitOption, 0, len(plan.SplitOptions)),
		SelectedSplitOptionID:  plan.SelectedSplitOptionID,
		SplitBookingAttemptIDs: append([]string(nil), plan.SplitBookingAttemptIDs...),
		SplitAppointmentIDs:    append([]string(nil), plan.SplitAppointmentIDs...),
	}
	for _, group := range plan.Groups {
		out.Groups = append(out.Groups, PartyPlanGroup{
			Label:               group.Label,
			Count:               group.Count,
			CandidateServiceIDs: append([]string(nil), group.CandidateServiceIDs...),
			ResolvedServiceIDs:  append([]string(nil), group.ResolvedServiceIDs...),
		})
	}
	for _, option := range plan.SplitOptions {
		cloned := PartySplitOption{
			ID:                   option.ID,
			DatePolicy:           option.DatePolicy,
			RequiresDateConsent:  option.RequiresDateConsent,
			DateConsentConfirmed: option.DateConsentConfirmed,
			SpanMinutes:          option.SpanMinutes,
			FinishSpreadMinutes:  option.FinishSpreadMinutes,
			Blocks:               make([]PartySplitBlock, 0, len(option.Blocks)),
		}
		for _, block := range option.Blocks {
			cloned.Blocks = append(cloned.Blocks, PartySplitBlock{
				StartTime: block.StartTime,
				EndTime:   block.EndTime,
				Segments:  append([]booking.BookingSegmentRequest(nil), block.Segments...),
			})
		}
		out.SplitOptions = append(out.SplitOptions, cloned)
	}
	return out
}

func partyPlanComplete(plan *PartyPlan) bool {
	if plan == nil || plan.PartySize < 2 || len(plan.Groups) == 0 {
		return false
	}
	total := 0
	for _, group := range plan.Groups {
		if group.Count <= 0 {
			return false
		}
		total += group.Count
		resolved := nonEmptyStrings(group.ResolvedServiceIDs)
		if len(resolved) != group.Count {
			return false
		}
	}
	return total == plan.PartySize
}

func autoResolveSingleCandidatePartyGroups(plan *PartyPlan) {
	if plan == nil {
		return
	}
	for i := range plan.Groups {
		group := &plan.Groups[i]
		candidates := nonEmptyStrings(group.CandidateServiceIDs)
		if len(candidates) != 1 || group.Count <= 0 {
			continue
		}
		resolved := nonEmptyStrings(group.ResolvedServiceIDs)
		for len(resolved) < group.Count {
			resolved = append(resolved, candidates[0])
		}
		if len(resolved) > group.Count {
			resolved = resolved[:group.Count]
		}
		group.ResolvedServiceIDs = resolved
	}
}

func partyPlanServiceMenuReply(message string, session Session, services []ServiceOption, cfg *RuntimeConfig) (string, bool) {
	plan := session.PartyPlan
	activeGroupIndex := firstUnresolvedPartyPlanGroup(plan)
	if activeGroupIndex < 0 {
		return "", false
	}
	menuGroupIndex, ok := partyPlanMenuGroupIndex(message, plan, services)
	if !ok {
		return "", false
	}
	group := plan.Groups[menuGroupIndex]
	candidates := servicesByIDs(services, group.CandidateServiceIDs)
	if len(candidates) == 0 {
		return "", false
	}
	options := serviceCandidateNames(candidates, 6)
	if len(options) == 0 {
		return "", false
	}
	label := strings.TrimSpace(group.Label)
	if label == "" {
		label = "service"
	}
	reply := "For " + pluralServicePhrase(label) + ", we offer " + joinHumanList(options) + ". "
	if menuGroupIndex == activeGroupIndex {
		reply += partyPlanMenuFollowUpPrompt(group)
	} else {
		reply += partyPlanClarificationPrompt(session, plan, services, cfg)
	}
	if partyPlanDeclinesServiceSuggestion(message) {
		reply = "No problem. " + reply
	}
	return reply, true
}

func partyPlanMenuGroupIndex(message string, plan *PartyPlan, services []ServiceOption) (int, bool) {
	if plan == nil || !isPartyPlanServiceMenuQuestion(message) {
		return -1, false
	}
	normalized := normalizeLooseText(message)
	for i, group := range plan.Groups {
		candidates := servicesByIDs(services, group.CandidateServiceIDs)
		if len(candidates) == 0 {
			continue
		}
		if partyPlanQuestionMentionsGroup(normalized, group, candidates) {
			return i, true
		}
	}
	if asksServiceMenu(message) {
		if groupIndex := firstUnresolvedPartyPlanGroup(plan); groupIndex >= 0 {
			return groupIndex, true
		}
	}
	return -1, false
}

func isPartyPlanServiceMenuQuestion(message string) bool {
	if asksServiceMenu(message) {
		return true
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	if !strings.Contains(normalized, "what") && !strings.Contains(normalized, "which") && !strings.Contains(normalized, "list") {
		return false
	}
	if !strings.Contains(normalized, "service") && !strings.Contains(normalized, "option") && !strings.Contains(normalized, "have") && !strings.Contains(normalized, "offer") {
		return false
	}
	return true
}

func partyPlanQuestionMentionsGroup(normalized string, group PartyPlanGroup, candidates []ServiceOption) bool {
	for _, phrase := range partyPlanGroupMenuPhrases(group, candidates) {
		if containsLoosePhrase(normalized, phrase) {
			return true
		}
	}
	return false
}

func partyPlanGroupMenuPhrases(group PartyPlanGroup, candidates []ServiceOption) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	add := func(value string) {
		value = normalizeServiceText(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
		if plural := pluralServicePhrase(value); plural != "" && !seen[plural] {
			seen[plural] = true
			out = append(out, plural)
		}
	}
	add(group.Label)
	for _, candidate := range candidates {
		add(candidate.CategoryName)
		add(candidate.CategorySlug)
		for _, token := range serviceNameTokens(candidate.Name) {
			add(singularServiceToken(token))
		}
	}
	return out
}

func resolvePartyPlanFromMessage(plan *PartyPlan, message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) bool {
	if plan == nil {
		return false
	}
	groupIndex := firstUnresolvedPartyPlanGroup(plan)
	if groupIndex < 0 {
		return false
	}
	group := &plan.Groups[groupIndex]
	remaining := group.Count - len(nonEmptyStrings(group.ResolvedServiceIDs))
	if remaining <= 0 {
		return false
	}
	candidates := servicesByIDs(services, group.CandidateServiceIDs)
	if isAffirmativeOnly(message) {
		switch {
		case len(candidates) == 1:
			group.ResolvedServiceIDs = append(group.ResolvedServiceIDs, repeatedString(candidates[0].ID, remaining)...)
			return true
		default:
			return false
		}
	}
	selected := partyPlanSelectedServices(message, candidates, services, aliases, categoryAliases, *group)
	if len(selected) == 0 {
		return narrowPartyPlanGroupFromMessage(group, message, candidates, services, aliases, categoryAliases)
	}
	ids := serviceIDsFromOptions(selected)
	switch {
	case len(ids) == remaining:
		group.ResolvedServiceIDs = append(group.ResolvedServiceIDs, ids...)
	case len(ids) == 1 && remaining > 1 && partyPlanCanRepeatSingleSelection(message):
		group.ResolvedServiceIDs = append(group.ResolvedServiceIDs, repeatedString(ids[0], remaining)...)
	case len(ids) < remaining:
		group.ResolvedServiceIDs = append(group.ResolvedServiceIDs, ids...)
	default:
		return false
	}
	return true
}

func firstUnresolvedPartyPlanGroup(plan *PartyPlan) int {
	if plan == nil {
		return -1
	}
	for i, group := range plan.Groups {
		if group.Count > len(nonEmptyStrings(group.ResolvedServiceIDs)) {
			return i
		}
	}
	return -1
}

func partyPlanSelectedServices(message string, candidates []ServiceOption, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, group PartyPlanGroup) []ServiceOption {
	normalized := normalizeServiceText(message)
	if normalized == "" || len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 && isAffirmativeOnly(message) {
		return append([]ServiceOption(nil), candidates[0])
	}
	pool := partyPlanSelectionPool(services, candidates, group)
	phrases := partyServicePhrasesWithAliases(pool, aliases)
	matches := make([]partyPlanServiceSelection, 0)
	for _, phrase := range phrases {
		index := indexNormalizedPhrase(normalized, phrase.Phrase)
		if index < 0 {
			continue
		}
		matches = append(matches, partyPlanServiceSelection{
			Start:     index,
			End:       index + len(phrase.Phrase),
			Service:   phrase.Service,
			PhraseLen: len(phrase.Phrase),
		})
	}
	if len(matches) == 0 {
		result := interpretServiceWithCategoryAliases(message, pool, aliases, categoryAliases)
		if result.Status == serviceUnderstandingStatusSelected {
			return append([]ServiceOption(nil), result.Candidates...)
		}
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Start == matches[j].Start {
			return matches[i].PhraseLen > matches[j].PhraseLen
		}
		return matches[i].Start < matches[j].Start
	})
	accepted := make([]partyPlanServiceSelection, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		if seen[strings.TrimSpace(match.Service.ID)] {
			continue
		}
		overlap := false
		for _, existing := range accepted {
			if match.Start < existing.End && existing.Start < match.End {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		accepted = append(accepted, match)
		seen[strings.TrimSpace(match.Service.ID)] = true
	}
	out := make([]ServiceOption, 0, len(accepted))
	for _, match := range accepted {
		out = append(out, match.Service)
	}
	return out
}

func narrowPartyPlanGroupFromMessage(group *PartyPlanGroup, message string, candidates []ServiceOption, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) bool {
	if group == nil {
		return false
	}
	pool := partyPlanSelectionPool(services, candidates, *group)
	result := interpretServiceWithCategoryAliases(message, pool, aliases, categoryAliases)
	if result.Status != serviceUnderstandingStatusAmbiguous || len(result.Candidates) == 0 {
		return false
	}
	group.CandidateServiceIDs = serviceIDsFromOptions(result.Candidates)
	group.Label = partyLabelFromServiceUnderstanding(result)
	group.ResolvedServiceIDs = nil
	return len(group.CandidateServiceIDs) > 0
}

func partyPlanSelectionPool(services []ServiceOption, candidates []ServiceOption, group PartyPlanGroup) []ServiceOption {
	out := make([]ServiceOption, 0, len(candidates))
	for _, candidate := range candidates {
		out = appendUniqueService(out, candidate)
	}
	for _, service := range services {
		if partyServiceFitsPartyGroup(service, candidates, group) {
			out = appendUniqueService(out, service)
		}
	}
	return orderedServices(out)
}

func partyServiceFitsPartyGroup(service ServiceOption, candidates []ServiceOption, group PartyPlanGroup) bool {
	serviceID := strings.TrimSpace(service.ID)
	if serviceID == "" {
		return false
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ID) == serviceID {
			return true
		}
	}
	serviceCategoryID := strings.TrimSpace(service.CategoryID)
	serviceCategoryName := normalizeServiceText(firstNonEmpty(service.CategoryName, service.CategorySlug))
	groupLabel := normalizeServiceText(group.Label)
	for _, candidate := range candidates {
		if serviceCategoryID != "" && serviceCategoryID == strings.TrimSpace(candidate.CategoryID) {
			return true
		}
		candidateCategory := normalizeServiceText(firstNonEmpty(candidate.CategoryName, candidate.CategorySlug))
		if serviceCategoryName != "" && candidateCategory != "" && serviceCategoryName == candidateCategory {
			return true
		}
	}
	if groupLabel == "" {
		return false
	}
	if serviceCategoryName != "" && (serviceCategoryName == groupLabel || containsNormalizedPhrase(serviceCategoryName, groupLabel)) {
		return true
	}
	serviceName := normalizeServiceText(service.Name)
	return serviceName != "" && containsNormalizedPhrase(serviceName, groupLabel)
}

func partyServicePhrasesWithAliases(services []ServiceOption, aliases []ServiceAlias) []partyServicePhrase {
	phrases := partyServicePhrases(services)
	seen := map[string]bool{}
	servicesByID := map[string]ServiceOption{}
	for _, service := range services {
		servicesByID[strings.TrimSpace(service.ID)] = service
	}
	for _, phrase := range phrases {
		seen[strings.TrimSpace(phrase.Service.ID)+"\x00"+strings.TrimSpace(phrase.Phrase)] = true
	}
	addAliasPhrase := func(service ServiceOption, phrase string) {
		phrase = normalizeServiceText(phrase)
		if phrase == "" {
			return
		}
		key := strings.TrimSpace(service.ID) + "\x00" + phrase
		if seen[key] {
			return
		}
		seen[key] = true
		phrases = append(phrases, partyServicePhrase{Service: service, Phrase: phrase})
	}
	for _, alias := range aliases {
		service, ok := servicesByID[strings.TrimSpace(alias.ServiceID)]
		if !ok {
			continue
		}
		phrase := strings.TrimSpace(alias.NormalizedAlias)
		if phrase == "" {
			phrase = alias.Alias
		}
		addAliasPhrase(service, phrase)
		addAliasPhrase(service, pluralServicePhrase(phrase))
	}
	sortPartyServicePhrases(phrases)
	return phrases
}

func sortPartyServicePhrases(phrases []partyServicePhrase) {
	sort.SliceStable(phrases, func(i, j int) bool {
		if len(phrases[i].Phrase) == len(phrases[j].Phrase) {
			if phrases[i].Family == phrases[j].Family {
				return phrases[i].Phrase < phrases[j].Phrase
			}
			return !phrases[i].Family
		}
		return len(phrases[i].Phrase) > len(phrases[j].Phrase)
	})
}

func partyPlanCanRepeatSingleSelection(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	normalized := normalizeLooseText(message)
	if strings.Contains(lower, "&") {
		return false
	}
	for _, signal := range []string{"and", "plus", "also", "each", "one each"} {
		if containsLoosePhrase(normalized, signal) {
			return false
		}
	}
	return true
}

func partyPlanSegments(plan *PartyPlan, session Session) []booking.BookingSegmentRequest {
	if plan == nil {
		return nil
	}
	mode := staffSelectionModeForServiceRequest(session)
	staffID := strings.TrimSpace(session.StaffID)
	if mode == booking.StaffSelectionAnyone {
		staffID = ""
	}
	segments := make([]booking.BookingSegmentRequest, 0, plan.PartySize)
	for _, group := range plan.Groups {
		for _, serviceID := range nonEmptyStrings(group.ResolvedServiceIDs) {
			segments = append(segments, booking.BookingSegmentRequest{
				ServiceID:          serviceID,
				StaffID:            staffID,
				StaffSelectionMode: mode,
			})
		}
	}
	return segments
}

func partyPlanClarificationPrompt(session Session, plan *PartyPlan, services []ServiceOption, cfg *RuntimeConfig) string {
	groupIndex := firstUnresolvedPartyPlanGroup(plan)
	if groupIndex < 0 {
		return "Which service would you like for the group appointment?"
	}
	group := plan.Groups[groupIndex]
	candidates := servicesByIDs(services, group.CandidateServiceIDs)
	options := serviceCandidateNames(candidates, 6)
	if len(options) == 0 {
		return "Which service would you like for the group appointment?"
	}
	label := strings.TrimSpace(group.Label)
	if label == "" {
		label = "service"
	}
	remaining := group.Count - len(nonEmptyStrings(group.ResolvedServiceIDs))
	if remaining < 1 {
		remaining = group.Count
	}
	countLabel := partyPlanCountLabel(remaining, label)
	switch {
	case len(options) == 1:
		return "Should I book " + serviceCountSpeech(remaining, options[0]) + "?"
	case remaining > 1:
		return "For " + countLabel + ", which services would you like: " + joinChoiceList(options) + "?"
	default:
		return "Which " + label + " service would you like: " + joinChoiceList(options) + "?"
	}
}

func partyPlanClarificationReply(message string, session Session, plan *PartyPlan, services []ServiceOption, cfg *RuntimeConfig) string {
	reply := partyPlanClarificationPrompt(session, plan, services, cfg)
	if partyPlanDeclinesServiceSuggestion(message) {
		return "No problem. " + reply
	}
	return reply
}

func partyPlanMenuFollowUpPrompt(group PartyPlanGroup) string {
	remaining := group.Count - len(nonEmptyStrings(group.ResolvedServiceIDs))
	if remaining <= 1 {
		return "Which one should I book?"
	}
	return "Which " + countWord(remaining) + " should I book?"
}

func partyPlanDeclinesServiceSuggestion(message string) bool {
	normalized := normalizeLooseText(message)
	return normalized == "no" ||
		normalized == "nope" ||
		strings.HasPrefix(normalized, "no ") ||
		strings.HasPrefix(normalized, "nope ") ||
		strings.Contains(normalized, "not those") ||
		strings.Contains(normalized, "not that") ||
		strings.Contains(normalized, "don t want that") ||
		strings.Contains(normalized, "do not want that")
}

func partyPlanCountLabel(count int, label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "service"
	}
	switch count {
	case 1:
		return "the " + label
	case 2:
		return "the two " + pluralServicePhrase(label)
	case 3:
		return "the three " + pluralServicePhrase(label)
	case 4:
		return "the four " + pluralServicePhrase(label)
	default:
		return fmt.Sprintf("the %d %s", count, pluralServicePhrase(label))
	}
}

func serviceCountSpeech(count int, service string) string {
	service = strings.TrimSpace(service)
	if count <= 1 {
		return "one " + service
	}
	return countWord(count) + " " + pluralDisplayName(service)
}

func oneEachServiceSpeech(options []string) string {
	parts := make([]string, 0, len(options))
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option != "" {
			parts = append(parts, "one "+option)
		}
	}
	return joinHumanList(parts)
}

func countWord(count int) string {
	switch count {
	case 1:
		return "one"
	case 2:
		return "two"
	case 3:
		return "three"
	case 4:
		return "four"
	case 5:
		return "five"
	case 6:
		return "six"
	case 7:
		return "seven"
	case 8:
		return "eight"
	case 9:
		return "nine"
	default:
		return fmt.Sprintf("%d", count)
	}
}

func pluralDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return name
	}
	last := parts[len(parts)-1]
	lower := strings.ToLower(last)
	switch {
	case strings.HasSuffix(lower, "s"):
		return name
	case strings.HasSuffix(lower, "y"):
		parts[len(parts)-1] = strings.TrimSuffix(last, last[len(last)-1:]) + "ies"
	default:
		parts[len(parts)-1] = last + "s"
	}
	return strings.Join(parts, " ")
}

func applyPartyPlanMetadata(turn *TurnRecord, plan *PartyPlan) {
	if turn == nil || plan == nil {
		return
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"party_plan_active":     true,
		"party_plan_complete":   partyPlanComplete(plan),
		"party_plan_party_size": plan.PartySize,
	})
}

func serviceIDsFromOptions(services []ServiceOption) []string {
	out := make([]string, 0, len(services))
	seen := map[string]bool{}
	for _, service := range services {
		id := strings.TrimSpace(service.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func repeatedString(value string, count int) []string {
	if strings.TrimSpace(value) == "" || count <= 0 {
		return nil
	}
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, value)
	}
	return out
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func partyServiceCountSegmentsFromMessage(message string, services []ServiceOption, session Session) []booking.BookingSegmentRequest {
	normalized := normalizeServiceText(message)
	if normalized == "" {
		return nil
	}
	phrases := partyServicePhrases(services)
	matches := make([]partyServiceCountMatch, 0)
	for _, phrase := range phrases {
		pattern := regexp.MustCompile(`\b(` + partyCountTokenPattern() + `)\s+` + regexp.QuoteMeta(phrase.Phrase) + `\b`)
		for _, indexes := range pattern.FindAllStringSubmatchIndex(normalized, -1) {
			if len(indexes) < 4 {
				continue
			}
			countToken := normalized[indexes[2]:indexes[3]]
			count, ok := partyCountTokenValue(countToken)
			if !ok || count < 1 {
				continue
			}
			matches = append(matches, partyServiceCountMatch{
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
	accepted := make([]partyServiceCountMatch, 0, len(matches))
	for _, match := range matches {
		if partyCountMatchOverlaps(accepted, match) {
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

func partyCountMatchOverlaps(accepted []partyServiceCountMatch, candidate partyServiceCountMatch) bool {
	for _, item := range accepted {
		if candidate.Start < item.End && item.Start < candidate.End {
			return true
		}
	}
	return false
}

func partySegmentsForService(service ServiceOption, count int, session Session) []booking.BookingSegmentRequest {
	serviceID := strings.TrimSpace(service.ID)
	if serviceID == "" || count <= 0 {
		return nil
	}
	mode := staffSelectionModeForServiceRequest(session)
	staffID := strings.TrimSpace(session.StaffID)
	if mode == booking.StaffSelectionAnyone {
		staffID = ""
	}
	segments := make([]booking.BookingSegmentRequest, 0, count)
	for i := 0; i < count; i++ {
		segments = append(segments, booking.BookingSegmentRequest{
			ServiceID:          serviceID,
			StaffID:            staffID,
			StaffSelectionMode: mode,
		})
	}
	return segments
}

func singlePartyServiceFromMessage(message string, services []ServiceOption) (ServiceOption, bool) {
	normalized := normalizeServiceText(message)
	if normalized == "" {
		return ServiceOption{}, false
	}
	phrases := partyServicePhrases(services)
	matched := map[string]ServiceOption{}
	exactMatched := map[string]ServiceOption{}
	for _, phrase := range phrases {
		if !containsNormalizedPhrase(normalized, phrase.Phrase) {
			continue
		}
		serviceID := strings.TrimSpace(phrase.Service.ID)
		if serviceID == "" {
			continue
		}
		matched[serviceID] = phrase.Service
		if !phrase.Family {
			exactMatched[serviceID] = phrase.Service
		}
	}
	if len(exactMatched) == 1 {
		for _, service := range exactMatched {
			return service, true
		}
	}
	if len(matched) == 1 {
		for _, service := range matched {
			return service, true
		}
	}
	return ServiceOption{}, false
}

func partyServicePhrases(services []ServiceOption) []partyServicePhrase {
	familyCounts := map[string]int{}
	for _, service := range services {
		for _, token := range serviceNameTokens(service.Name) {
			familyCounts[singularServiceToken(token)]++
		}
	}
	seen := map[string]bool{}
	phrases := make([]partyServicePhrase, 0)
	addPhrase := func(service ServiceOption, phrase string, family bool) {
		phrase = normalizeServiceText(phrase)
		if phrase == "" {
			return
		}
		key := strings.TrimSpace(service.ID) + "\x00" + phrase
		if seen[key] {
			return
		}
		seen[key] = true
		phrases = append(phrases, partyServicePhrase{Service: service, Phrase: phrase, Family: family})
	}
	for _, service := range services {
		name := normalizeServiceText(service.Name)
		if name == "" || strings.TrimSpace(service.ID) == "" {
			continue
		}
		addPhrase(service, name, false)
		addPhrase(service, pluralServicePhrase(name), false)
		for _, token := range serviceNameTokens(service.Name) {
			singular := singularServiceToken(token)
			if familyCounts[singular] != 1 {
				continue
			}
			addPhrase(service, singular, true)
			addPhrase(service, pluralServicePhrase(singular), true)
		}
	}
	sortPartyServicePhrases(phrases)
	return phrases
}

func pluralServicePhrase(phrase string) string {
	phrase = normalizeServiceText(phrase)
	if phrase == "" {
		return ""
	}
	parts := strings.Fields(phrase)
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	switch {
	case strings.HasSuffix(last, "s"):
		return phrase
	case strings.HasSuffix(last, "y"):
		parts[len(parts)-1] = strings.TrimSuffix(last, "y") + "ies"
	default:
		parts[len(parts)-1] = last + "s"
	}
	return strings.Join(parts, " ")
}

func singularServiceToken(token string) string {
	token = normalizeServiceText(token)
	switch {
	case strings.HasSuffix(token, "ies") && len(token) > 3:
		return strings.TrimSuffix(token, "ies") + "y"
	case strings.HasSuffix(token, "s") && len(token) > 3:
		return strings.TrimSuffix(token, "s")
	default:
		return token
	}
}

func partyCountTokenPattern() string {
	return `[1-9]|one|two|three|four|five|six|seven|eight|nine`
}

func partyCountTokenValue(token string) (int, bool) {
	token = strings.TrimSpace(strings.ToLower(token))
	switch token {
	case "one":
		return 1, true
	case "two":
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
	}
	value, err := strconv.Atoi(token)
	if err != nil || value < 1 || value > 9 {
		return 0, false
	}
	return value, true
}

func applyPartyBookingPlan(session *Session, plan partyBookingPlan) bool {
	if session == nil || len(plan.Segments) == 0 {
		return false
	}
	before := selectedServiceIDs(*session)
	segments := make([]booking.BookingSegmentRequest, 0, len(plan.Segments))
	for _, segment := range plan.Segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		mode := normalizeConversationStaffSelectionMode(segment.StaffSelectionMode)
		if mode == "" {
			mode = staffSelectionModeForServiceRequest(*session)
		}
		staffID := strings.TrimSpace(segment.StaffID)
		if mode == booking.StaffSelectionAnyone {
			staffID = ""
		}
		segments = append(segments, booking.BookingSegmentRequest{
			ServiceID:          serviceID,
			StaffID:            staffID,
			StaffSelectionMode: mode,
		})
	}
	if len(segments) == 0 {
		return false
	}
	session.ServiceID = segments[0].ServiceID
	session.BookingSegments = segments
	session.StaffSelectionMode = segments[0].StaffSelectionMode
	if session.StaffSelectionMode == booking.StaffSelectionAnyone {
		session.StaffID = ""
		session.StaffName = ""
	}
	session.OfferedSlots = nil
	return !sameStringSlices(before, selectedServiceIDs(*session))
}

func applyPartyBookingMetadata(turn *TurnRecord, session Session) {
	if turn == nil {
		return
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"party_booking":        true,
		"party_segment_count":  len(session.BookingSegments),
		"party_service_counts": partyServiceCountsForMetadata(session.BookingSegments),
	})
}

func partyServiceCountsForMetadata(segments []booking.BookingSegmentRequest) map[string]int {
	counts := map[string]int{}
	for _, segment := range segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		counts[serviceID]++
	}
	return counts
}
