package integrationconfig

import (
	"errors"
	"testing"
	"time"
)

func TestScanConfigRejectsMalformedOrNonStringSettings(t *testing.T) {
	for _, settings := range []string{
		"",
		"null",
		"[]",
		`{"client_id":null}`,
		`{"client_id":true}`,
		`{"client_id":{"nested":"value"}}`,
		`{"client_id":`,
	} {
		t.Run(settings, func(t *testing.T) {
			item, err := scanConfig(configRowScanner{settings: settings})
			if item != nil || !errors.Is(err, ErrInvalidSettings) {
				t.Fatalf("scanConfig(%q) = %#v, %v; want ErrInvalidSettings", settings, item, err)
			}
		})
	}
}

func TestScanConfigAcceptsStringSettingsObject(t *testing.T) {
	item, err := scanConfig(configRowScanner{settings: `{"client_id":"stored-client","empty":""}`})
	if err != nil {
		t.Fatalf("scanConfig returned error: %v", err)
	}
	if item.Settings["client_id"] != "stored-client" {
		t.Fatalf("settings = %#v", item.Settings)
	}
}

type configRowScanner struct {
	settings string
	err      error
}

func (s configRowScanner) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	now := time.Now().UTC()
	*dest[0].(*string) = "config_1"
	*dest[1].(*string) = "salon_1"
	*dest[2].(*string) = ProviderSquare
	*dest[3].(*bool) = true
	*dest[4].(*string) = s.settings
	*dest[5].(*string) = ""
	*dest[6].(*time.Time) = now
	*dest[7].(*time.Time) = now
	return nil
}
