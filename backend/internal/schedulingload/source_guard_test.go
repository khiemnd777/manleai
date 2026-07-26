package schedulingload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessSourceHasNoDestructiveSQLOrRealProviderRuntimeImport(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Clean(entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", entry.Name(), err)
		}
		upper := strings.ToUpper(string(raw))
		for _, forbidden := range []string{"DELETE FROM", "TRUNCATE ", "DROP DATABASE", "DROP TABLE"} {
			if strings.Contains(upper, forbidden) {
				t.Fatalf("%s contains prohibited destructive SQL %q", entry.Name(), forbidden)
			}
		}
		for _, forbidden := range []string{"scheduling_external_provider", "modules/twilio", "modules/openai", "square.New"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("%s imports or initializes prohibited real provider runtime %q", entry.Name(), forbidden)
			}
		}
	}
}
