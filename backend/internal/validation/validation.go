package validation

import "strings"

func Required(value string) bool {
	return strings.TrimSpace(value) != ""
}

func NormalizePhone(value string) string {
	replacer := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "")
	return replacer.Replace(strings.TrimSpace(value))
}
