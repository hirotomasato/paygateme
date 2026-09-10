package shopee

import (
	"regexp"
	"strings"
)

// parseShopeeAmount parses Shopee's Indonesian grouped integer format into
// whole rupiah. Returns 0 and false when the value cannot be parsed safely.
//
// Examples: "30.000" → 30000, "1.250.000" → 1250000, "30000" → 30000.
// Rejects: "30.00", "30,000", "30.000.00", "-100", "1e5", decimals.
func ParseShopeeAmount(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	// Accept either plain digits or Indonesian grouped format (digits
	// separated by dots, e.g. "30.000" or "1.250.000").
	plainDigits := regexp.MustCompile(`^\d+$`)
	indonesianGroup := regexp.MustCompile(`^\d{1,3}(?:\.\d{3})+$`)

	if !plainDigits.MatchString(value) && !indonesianGroup.MatchString(value) {
		return 0, false
	}

	normalized := strings.ReplaceAll(value, ".", "")
	var amount int64
	for _, r := range normalized {
		amount = amount*10 + int64(r-'0')
	}
	// Safe integer check: Go int64 max is 9,223,372,036,854,775,807
	// which is more than enough for any real payment amount.
	if amount < 0 {
		return 0, false
	}
	return amount, true
}
