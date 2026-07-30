package twiliovoice

import (
	"net/url"
	"regexp"
	"strings"
)

const routePrefix = "/api/voice/twilio/"

var (
	e164Pattern       = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	accountSIDPattern = regexp.MustCompile(`^AC[0-9a-fA-F]{32}$`)
)

type Paths struct {
	Incoming       string
	Turn           string
	Recording      string
	Stream         string
	StreamStatus   string
	StreamFallback string
}

func CanonicalPaths(routeID string) Paths {
	base := routePrefix + strings.TrimSpace(routeID)
	return Paths{
		Incoming:       base + "/incoming",
		Turn:           base + "/turn",
		Recording:      base + "/recording",
		Stream:         base + "/stream",
		StreamStatus:   base + "/stream/status",
		StreamFallback: base + "/stream/fallback",
	}
}

func ValidE164(value string) bool {
	return e164Pattern.MatchString(strings.TrimSpace(value))
}

func ValidAccountSID(value string) bool {
	return accountSIDPattern.MatchString(strings.TrimSpace(value))
}

func ValidPublicHTTPSBase(value string) bool {
	value = strings.TrimSpace(value)
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Path == ""
}

func HTTPURL(publicBaseURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	path = strings.TrimSpace(path)
	if base == "" || path == "" {
		return ""
	}
	return base + "/" + strings.TrimLeft(path, "/")
}

func WebSocketURL(publicBaseURL, path string) string {
	value := HTTPURL(publicBaseURL, path)
	if strings.HasPrefix(value, "https://") {
		return "wss://" + strings.TrimPrefix(value, "https://")
	}
	return ""
}
