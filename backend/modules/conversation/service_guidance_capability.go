package conversation

import "strings"

type ServiceGuidanceCapabilityStatus string

const (
	ServiceGuidanceCapabilityRecommendationReady ServiceGuidanceCapabilityStatus = "recommendation_ready"
	ServiceGuidanceCapabilityCatalogOnly         ServiceGuidanceCapabilityStatus = "catalog_only"
	ServiceGuidanceCapabilityDisabled            ServiceGuidanceCapabilityStatus = "consultation_disabled"
	ServiceGuidanceCapabilityCatalogUnavailable  ServiceGuidanceCapabilityStatus = "catalog_unavailable"
)

// serviceGuidanceCapability is derived only from the runtime catalog and the
// salon-managed consultation configuration. It deliberately contains no
// caller wording, so recognizing an intent can never be confused with whether
// the salon is currently able to fulfill it.
type serviceGuidanceCapability struct {
	Status              ServiceGuidanceCapabilityStatus
	CatalogAvailable    bool
	RecommendationReady bool
}

func resolveServiceGuidanceCapability(services []ServiceOption, cfg *RuntimeConfig) serviceGuidanceCapability {
	if len(services) == 0 {
		return serviceGuidanceCapability{Status: ServiceGuidanceCapabilityCatalogUnavailable}
	}
	if cfg == nil || !cfg.ConsultationEnabled {
		return serviceGuidanceCapability{
			Status: ServiceGuidanceCapabilityDisabled, CatalogAvailable: true,
		}
	}
	if consultationGuidanceAvailable(services, cfg) {
		return serviceGuidanceCapability{
			Status: ServiceGuidanceCapabilityRecommendationReady, CatalogAvailable: true, RecommendationReady: true,
		}
	}
	return serviceGuidanceCapability{Status: ServiceGuidanceCapabilityCatalogOnly, CatalogAvailable: true}
}

func serviceGuidanceCapabilityReply(capability serviceGuidanceCapability, services []ServiceOption) string {
	switch capability.Status {
	case ServiceGuidanceCapabilityDisabled:
		return "I understand you'd like help choosing a nail service. Personalized service guidance isn't available right now, but I can read the salon's bookable service menu or ask the owner to help."
	case ServiceGuidanceCapabilityCatalogOnly:
		if categories := serviceGuidanceCategoryNames(services, 5); len(categories) > 0 {
			return "I can help narrow it down from the salon's bookable menu. Are you looking for " + joinHumanList(categories) + "?"
		}
		if names := serviceCandidateNames(services, 5); len(names) > 0 {
			return "I can help narrow it down from the salon's bookable menu. Which sounds closest: " + joinHumanList(names) + "?"
		}
	case ServiceGuidanceCapabilityCatalogUnavailable:
		return "I understand you'd like help choosing a nail service. I can't access the salon's service guide right now, so I won't guess. I can ask the owner to help."
	}
	return "I understand you'd like help choosing a nail service. I can ask the owner to help."
}

func serviceGuidanceCategoryNames(services []ServiceOption, limit int) []string {
	names := make([]string, 0)
	seen := map[string]bool{}
	for _, service := range services {
		name := strings.TrimSpace(service.CategoryName)
		if name == "" {
			continue
		}
		key := strings.ToLower(strings.Join(strings.Fields(name), " "))
		if seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
		if limit > 0 && len(names) >= limit {
			break
		}
	}
	return names
}

func serviceGuidanceCapabilityMetadata(capability serviceGuidanceCapability) map[string]any {
	return map[string]any{
		"service_guidance_capability":           string(capability.Status),
		"service_guidance_catalog_available":    capability.CatalogAvailable,
		"service_guidance_recommendation_ready": capability.RecommendationReady,
	}
}
