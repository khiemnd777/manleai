package conversation

import (
	"sort"
	"strings"
	"unicode"
)

const (
	serviceUnderstandingUnknown         = "unknown_service"
	serviceUnderstandingExact           = "exact_service"
	serviceUnderstandingAlias           = "service_alias"
	serviceUnderstandingFuzzyService    = "fuzzy_service"
	serviceUnderstandingCatalogToken    = "catalog_token"
	serviceUnderstandingAmbiguousFamily = "ambiguous_family"
	serviceUnderstandingFuzzyFamily     = "fuzzy_family"
)

const (
	fuzzyServiceStrictThreshold  = 0.74
	fuzzyServicePendingThreshold = 0.61
	fuzzyServiceStrictMinToken   = 0.55
	fuzzyServicePendingMinToken  = 0.50
	fuzzyServiceStrictMargin     = 0.12
	fuzzyServicePendingMargin    = 0.16
)

type serviceUnderstandingStatus string

const (
	serviceUnderstandingStatusUnknown   serviceUnderstandingStatus = "unknown"
	serviceUnderstandingStatusSelected  serviceUnderstandingStatus = "selected"
	serviceUnderstandingStatusAmbiguous serviceUnderstandingStatus = "ambiguous"
)

type serviceUnderstandingResult struct {
	Status          serviceUnderstandingStatus
	Reason          string
	Confidence      float64
	Selected        *ServiceOption
	Candidates      []ServiceOption
	MatchedToken    string
	MatchedSource   string
	MatchedAliasID  string
	MatchedAlias    string
	NormalizedInput string
}

func interpretService(message string, services []ServiceOption, aliasSets ...[]ServiceAlias) serviceUnderstandingResult {
	aliases := []ServiceAlias(nil)
	if len(aliasSets) > 0 {
		aliases = aliasSets[0]
	}
	index := newServiceCatalogIndex(services, aliases)
	return index.Interpret(message)
}

func interpretServiceForSession(message string, session Session, services []ServiceOption, aliases []ServiceAlias) serviceUnderstandingResult {
	if pending := pendingServiceCandidateServices(session, services); len(pending) > 0 {
		result := newServiceCatalogIndex(pending, nil).InterpretPending(message)
		if result.Status != serviceUnderstandingStatusUnknown {
			return result
		}
	}
	return interpretService(message, services, aliases)
}

func pendingServiceCandidateServices(session Session, services []ServiceOption) []ServiceOption {
	ids := pendingServiceCandidateIDs(session)
	if len(ids) == 0 {
		return nil
	}
	byID := map[string]ServiceOption{}
	for _, service := range services {
		byID[strings.TrimSpace(service.ID)] = service
	}
	out := make([]ServiceOption, 0, len(ids))
	for _, id := range ids {
		if service, ok := byID[strings.TrimSpace(id)]; ok {
			out = append(out, service)
		}
	}
	return out
}

func pendingServiceCandidateIDs(session Session) []string {
	if strings.TrimSpace(session.ServiceID) != "" {
		return nil
	}
	for i := len(session.Transcript) - 1; i >= 0; i-- {
		msg := session.Transcript[i]
		if msg.Speaker != SpeakerAI {
			continue
		}
		if metadataBool(msg.Metadata, "pending_service_candidates_cleared") {
			return nil
		}
		if ids := metadataStringSlice(msg.Metadata, "pending_service_candidate_ids"); len(ids) > 0 {
			return ids
		}
	}
	return nil
}

type serviceCatalogIndex struct {
	services      []ServiceOption
	exactNames    map[string]ServiceOption
	exactPhrases  []serviceExactPhrase
	aliasPhrases  []serviceAliasPhrase
	tokenServices map[string][]ServiceOption
	familyTokens  map[string][]ServiceOption
}

type serviceExactPhrase struct {
	Phrase  string
	Service ServiceOption
}

type serviceAliasPhrase struct {
	Phrase  string
	Alias   ServiceAlias
	Service ServiceOption
}

func newServiceCatalogIndex(services []ServiceOption, aliases []ServiceAlias) serviceCatalogIndex {
	index := serviceCatalogIndex{
		services:      append([]ServiceOption(nil), services...),
		exactNames:    map[string]ServiceOption{},
		tokenServices: map[string][]ServiceOption{},
		familyTokens:  map[string][]ServiceOption{},
	}
	sort.SliceStable(index.services, func(i, j int) bool {
		return len(index.services[i].Name) > len(index.services[j].Name)
	})
	for _, service := range index.services {
		name := normalizeServiceText(service.Name)
		if name == "" {
			continue
		}
		index.exactNames[name] = service
		index.exactPhrases = append(index.exactPhrases, serviceExactPhrase{Phrase: name, Service: service})
		for _, token := range serviceNameTokens(service.Name) {
			index.tokenServices[token] = appendUniqueService(index.tokenServices[token], service)
		}
	}
	servicesByID := map[string]ServiceOption{}
	for _, service := range index.services {
		servicesByID[strings.TrimSpace(service.ID)] = service
	}
	for _, alias := range aliases {
		service, ok := servicesByID[strings.TrimSpace(alias.ServiceID)]
		if !ok {
			continue
		}
		phrase := normalizeServiceText(alias.NormalizedAlias)
		if phrase == "" {
			phrase = normalizeServiceText(alias.Alias)
		}
		if phrase == "" {
			continue
		}
		index.aliasPhrases = append(index.aliasPhrases, serviceAliasPhrase{
			Phrase:  phrase,
			Alias:   alias,
			Service: service,
		})
	}
	for token, items := range index.tokenServices {
		if len(items) > 1 {
			index.familyTokens[token] = orderedServices(items)
		}
	}
	sort.SliceStable(index.exactPhrases, func(i, j int) bool {
		return len(index.exactPhrases[i].Phrase) > len(index.exactPhrases[j].Phrase)
	})
	sort.SliceStable(index.aliasPhrases, func(i, j int) bool {
		if len(index.aliasPhrases[i].Phrase) == len(index.aliasPhrases[j].Phrase) {
			return index.aliasPhrases[i].Service.Name < index.aliasPhrases[j].Service.Name
		}
		return len(index.aliasPhrases[i].Phrase) > len(index.aliasPhrases[j].Phrase)
	})
	return index
}

func (idx serviceCatalogIndex) Interpret(message string) serviceUnderstandingResult {
	return idx.interpret(message, false)
}

func (idx serviceCatalogIndex) InterpretPending(message string) serviceUnderstandingResult {
	return idx.interpret(message, true)
}

func (idx serviceCatalogIndex) interpret(message string, pendingCandidates bool) serviceUnderstandingResult {
	normalized := normalizeServiceText(message)
	result := serviceUnderstandingResult{
		Status:          serviceUnderstandingStatusUnknown,
		Reason:          serviceUnderstandingUnknown,
		NormalizedInput: normalized,
	}
	if normalized == "" || len(idx.services) == 0 {
		return result
	}
	exactMatches := make([]serviceMatch, 0, len(idx.exactPhrases))
	for _, exact := range idx.exactPhrases {
		if index := indexNormalizedPhrase(normalized, exact.Phrase); index >= 0 {
			exactMatches = append(exactMatches, serviceMatch{
				service: exact.Service,
				index:   index,
				end:     index + len(exact.Phrase),
				token:   exact.Phrase,
			})
		}
	}
	if len(exactMatches) > 0 {
		exactMatches = removeContainedServiceMatches(exactMatches)
		sort.SliceStable(exactMatches, func(i, j int) bool {
			if exactMatches[i].index == exactMatches[j].index {
				return len(exactMatches[i].service.Name) > len(exactMatches[j].service.Name)
			}
			return exactMatches[i].index < exactMatches[j].index
		})
		candidates := make([]ServiceOption, 0, len(exactMatches))
		for _, match := range exactMatches {
			candidates = appendUniqueService(candidates, match.service)
		}
		result.Status = serviceUnderstandingStatusSelected
		result.Reason = serviceUnderstandingExact
		result.Confidence = 1
		result.Candidates = candidates
		result.MatchedToken = exactMatches[0].token
		item := candidates[0]
		result.Selected = &item
		return result
	}
	aliasMatches := idx.aliasMatches(normalized)
	if len(aliasMatches) == 1 {
		result.Status = serviceUnderstandingStatusSelected
		result.Reason = serviceUnderstandingAlias
		result.Confidence = aliasConfidence(aliasMatches[0].alias)
		result.Candidates = []ServiceOption{aliasMatches[0].service}
		result.MatchedToken = aliasMatches[0].token
		result.MatchedSource = strings.TrimSpace(aliasMatches[0].alias.Source)
		result.MatchedAliasID = strings.TrimSpace(aliasMatches[0].alias.ID)
		result.MatchedAlias = strings.TrimSpace(aliasMatches[0].alias.Alias)
		item := aliasMatches[0].service
		result.Selected = &item
		return result
	}
	if len(aliasMatches) > 1 {
		result.Status = serviceUnderstandingStatusAmbiguous
		result.Reason = serviceUnderstandingAlias
		result.Confidence = 0.7
		result.MatchedToken = aliasMatches[0].token
		result.MatchedSource = "conflict"
		candidates := make([]ServiceOption, 0, len(aliasMatches))
		for _, match := range aliasMatches {
			candidates = appendUniqueService(candidates, match.service)
		}
		result.Candidates = orderedServices(candidates)
		return result
	}
	if fuzzyMatch, ok := idx.fuzzyServiceMatch(normalized, pendingCandidates); ok {
		result.Status = serviceUnderstandingStatusSelected
		result.Reason = serviceUnderstandingFuzzyService
		result.Confidence = fuzzyMatch.score
		result.Candidates = []ServiceOption{fuzzyMatch.service}
		result.MatchedToken = fuzzyMatch.token
		item := fuzzyMatch.service
		result.Selected = &item
		return result
	}
	directMatches := idx.directTokenMatches(normalized)
	if len(directMatches) == 1 {
		result.Status = serviceUnderstandingStatusSelected
		result.Reason = serviceUnderstandingCatalogToken
		result.Confidence = 0.86
		item := directMatches[0]
		result.Selected = &item
		result.Candidates = directMatches
		return result
	}
	if len(directMatches) > 1 {
		result.Status = serviceUnderstandingStatusAmbiguous
		result.Reason = serviceUnderstandingAmbiguousFamily
		result.Confidence = 0.72
		result.Candidates = directMatches
		result.MatchedToken = firstMatchedFamilyToken(normalized, idx.familyTokens)
		return result
	}
	fuzzyToken, fuzzyCandidates := idx.fuzzyFamilyMatch(normalized)
	if len(fuzzyCandidates) > 0 {
		result.Status = serviceUnderstandingStatusAmbiguous
		result.Reason = serviceUnderstandingFuzzyFamily
		result.Confidence = 0.62
		result.Candidates = fuzzyCandidates
		result.MatchedToken = fuzzyToken
	}
	return result
}

type fuzzyServiceMatch struct {
	service       ServiceOption
	score         float64
	minTokenScore float64
	token         string
}

type serviceAliasMatch struct {
	service ServiceOption
	alias   ServiceAlias
	index   int
	end     int
	token   string
}

func (idx serviceCatalogIndex) aliasMatches(normalized string) []serviceAliasMatch {
	matches := make([]serviceAliasMatch, 0)
	for _, alias := range idx.aliasPhrases {
		if index := indexNormalizedPhrase(normalized, alias.Phrase); index >= 0 {
			matches = append(matches, serviceAliasMatch{
				service: alias.Service,
				alias:   alias.Alias,
				index:   index,
				end:     index + len(alias.Phrase),
				token:   alias.Phrase,
			})
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].index == matches[j].index {
			return len(matches[i].token) > len(matches[j].token)
		}
		return matches[i].index < matches[j].index
	})
	return removeContainedAliasMatches(matches)
}

func removeContainedAliasMatches(matches []serviceAliasMatch) []serviceAliasMatch {
	out := make([]serviceAliasMatch, 0, len(matches))
	for i, current := range matches {
		contained := false
		for j, other := range matches {
			if i == j || strings.TrimSpace(current.service.ID) == strings.TrimSpace(other.service.ID) {
				continue
			}
			if current.index >= other.index && current.end <= other.end && len(other.token) > len(current.token) {
				contained = true
				break
			}
		}
		if !contained {
			out = append(out, current)
		}
	}
	return out
}

func aliasConfidence(alias ServiceAlias) float64 {
	if alias.Confidence > 0 {
		return alias.Confidence
	}
	return 0.94
}

func (idx serviceCatalogIndex) directTokenMatches(normalized string) []ServiceOption {
	matches := []ServiceOption{}
	for token, services := range idx.tokenServices {
		if !containsNormalizedPhrase(normalized, token) {
			continue
		}
		for _, service := range services {
			matches = appendUniqueService(matches, service)
		}
	}
	return orderedServices(matches)
}

func (idx serviceCatalogIndex) fuzzyFamilyMatch(normalized string) (string, []ServiceOption) {
	inputTokens := strings.Fields(normalized)
	for _, token := range inputTokens {
		if len([]rune(token)) < 5 {
			continue
		}
		for familyToken, services := range idx.familyTokens {
			if fuzzyServiceTokenMatch(token, familyToken) {
				return familyToken, orderedServices(services)
			}
		}
	}
	return "", nil
}

func (idx serviceCatalogIndex) fuzzyServiceMatch(normalized string, pendingCandidates bool) (fuzzyServiceMatch, bool) {
	inputTokens := serviceNameTokens(normalized)
	if len(inputTokens) == 0 || len(idx.services) == 0 {
		return fuzzyServiceMatch{}, false
	}
	matches := make([]fuzzyServiceMatch, 0, len(idx.services))
	for _, service := range idx.services {
		score, minTokenScore := fuzzyServiceScore(inputTokens, service.Name)
		if score <= 0 {
			continue
		}
		matches = append(matches, fuzzyServiceMatch{
			service:       service,
			score:         score,
			minTokenScore: minTokenScore,
			token:         normalizeServiceText(service.Name),
		})
	}
	if len(matches) == 0 {
		return fuzzyServiceMatch{}, false
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].service.Name < matches[j].service.Name
		}
		return matches[i].score > matches[j].score
	})
	threshold := fuzzyServiceStrictThreshold
	minToken := fuzzyServiceStrictMinToken
	margin := fuzzyServiceStrictMargin
	if pendingCandidates {
		threshold = fuzzyServicePendingThreshold
		minToken = fuzzyServicePendingMinToken
		margin = fuzzyServicePendingMargin
	}
	top := matches[0]
	if top.score < threshold || top.minTokenScore < minToken {
		return fuzzyServiceMatch{}, false
	}
	if len(matches) > 1 && top.score-matches[1].score < margin {
		return fuzzyServiceMatch{}, false
	}
	return top, true
}

func fuzzyServiceScore(inputTokens []string, serviceName string) (float64, float64) {
	serviceTokens := serviceNameTokens(serviceName)
	if len(serviceTokens) == 0 || len(inputTokens) == 0 {
		return 0, 0
	}
	total := 0.0
	minTokenScore := 1.0
	for _, serviceToken := range serviceTokens {
		best := 0.0
		for _, inputToken := range inputTokens {
			if score := fuzzyServiceTokenScore(inputToken, serviceToken); score > best {
				best = score
			}
		}
		total += best
		if best < minTokenScore {
			minTokenScore = best
		}
	}
	return total / float64(len(serviceTokens)), minTokenScore
}

func fuzzyServiceTokenScore(input string, catalogToken string) float64 {
	input = normalizeServiceText(input)
	catalogToken = normalizeServiceText(catalogToken)
	if input == "" || catalogToken == "" {
		return 0
	}
	if input == catalogToken {
		return 1
	}
	scores := []float64{
		levenshteinSimilarity(input, catalogToken),
		levenshteinSimilarity(phoneticServiceToken(input), phoneticServiceToken(catalogToken)),
		levenshteinSimilarity(consonantSkeleton(input), consonantSkeleton(catalogToken)),
		bigramDice(phoneticServiceToken(input), phoneticServiceToken(catalogToken)),
	}
	best := 0.0
	for _, score := range scores {
		if score > best {
			best = score
		}
	}
	return best
}

func levenshteinSimilarity(left string, right string) float64 {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return 0
	}
	distance := levenshteinDistance(left, right)
	maxLen := maxInt(len([]rune(left)), len([]rune(right)))
	if maxLen == 0 {
		return 0
	}
	return 1 - float64(distance)/float64(maxLen)
}

func phoneticServiceToken(value string) string {
	value = normalizeServiceText(value)
	value = strings.NewReplacer(
		"ph", "f",
		"ck", "k",
		"qu", "kw",
		"c", "k",
		"q", "k",
		"y", "i",
		"x", "ks",
		"z", "s",
	).Replace(value)
	value = strings.TrimSuffix(value, "e")
	return collapseRepeatedRunes(value)
}

func consonantSkeleton(value string) string {
	value = phoneticServiceToken(value)
	var b strings.Builder
	for _, r := range value {
		switch r {
		case 'a', 'e', 'i', 'o', 'u':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func collapseRepeatedRunes(value string) string {
	var b strings.Builder
	var previous rune
	for _, r := range value {
		if r == previous {
			continue
		}
		b.WriteRune(r)
		previous = r
	}
	return b.String()
}

func bigramDice(left string, right string) float64 {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	if len(leftRunes) < 2 || len(rightRunes) < 2 {
		return 0
	}
	leftCounts := map[string]int{}
	for i := 0; i < len(leftRunes)-1; i++ {
		leftCounts[string(leftRunes[i:i+2])]++
	}
	intersection := 0
	rightCount := 0
	for i := 0; i < len(rightRunes)-1; i++ {
		rightCount++
		key := string(rightRunes[i : i+2])
		if leftCounts[key] > 0 {
			intersection++
			leftCounts[key]--
		}
	}
	leftCount := len(leftRunes) - 1
	if leftCount+rightCount == 0 {
		return 0
	}
	return 2 * float64(intersection) / float64(leftCount+rightCount)
}

func firstMatchedFamilyToken(normalized string, families map[string][]ServiceOption) string {
	tokens := make([]string, 0, len(families))
	for token := range families {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	for _, token := range tokens {
		if containsNormalizedPhrase(normalized, token) {
			return token
		}
	}
	return ""
}

func serviceNameTokens(value string) []string {
	normalized := normalizeServiceText(value)
	fields := strings.Fields(normalized)
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		if len([]rune(field)) < 3 {
			continue
		}
		if seen[field] {
			continue
		}
		seen[field] = true
		out = append(out, field)
	}
	return out
}

func normalizeServiceText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	previousSpace := true
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			previousSpace = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '/' || r == '&':
			if !previousSpace {
				b.WriteByte(' ')
				previousSpace = true
			}
		default:
			if !previousSpace {
				b.WriteByte(' ')
				previousSpace = true
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func containsNormalizedPhrase(normalized string, phrase string) bool {
	return indexNormalizedPhrase(normalized, phrase) >= 0
}

func indexNormalizedPhrase(normalized string, phrase string) int {
	normalized = " " + strings.Join(strings.Fields(normalized), " ") + " "
	phrase = " " + strings.Join(strings.Fields(phrase), " ") + " "
	return strings.Index(normalized, phrase)
}

func fuzzyServiceTokenMatch(input string, catalogToken string) bool {
	input = normalizeServiceText(input)
	catalogToken = normalizeServiceText(catalogToken)
	if len([]rune(input)) < 5 || len([]rune(catalogToken)) < 5 {
		return false
	}
	distance := levenshteinDistance(input, catalogToken)
	maxLen := maxInt(len([]rune(input)), len([]rune(catalogToken)))
	if maxLen == 0 {
		return false
	}
	return float64(distance)/float64(maxLen) <= 0.38
}

func levenshteinDistance(a string, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ra := range ar {
		curr := make([]int, len(br)+1)
		curr[0] = i + 1
		for j, rb := range br {
			cost := 0
			if ra != rb {
				cost = 1
			}
			curr[j+1] = minInt(
				curr[j]+1,
				prev[j+1]+1,
				prev[j]+cost,
			)
		}
		prev = curr
	}
	return prev[len(br)]
}

func appendUniqueService(items []ServiceOption, item ServiceOption) []ServiceOption {
	for _, existing := range items {
		if strings.TrimSpace(existing.ID) == strings.TrimSpace(item.ID) {
			return items
		}
	}
	return append(items, item)
}

func orderedServices(items []ServiceOption) []ServiceOption {
	out := append([]ServiceOption(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func minInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	min := values[0]
	for _, value := range values[1:] {
		if value < min {
			min = value
		}
	}
	return min
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
