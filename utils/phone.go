// Package utils — Indonesian mobile number parser.
package utils

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/hirotomasato/paygateme/core"
)

const (
	IndonesiaCountryCode = "62"

	minSubscriberDigits = 9
	maxSubscriberDigits = 13
)

// IndonesianMobileNumber is a parsed and validated Indonesian mobile number
// in every canonical form.
type IndonesianMobileNumber struct {
	// CountryCode is always "62" for Indonesia.
	CountryCode string
	// Subscriber is the number starting with 8, without trunk 0 or country code.
	Subscriber string
	// National is the form with a single leading 0, e.g. "0812xxxxxxx".
	National string
	// E164 is the international form without a plus, e.g. "62812xxxxxxx".
	E164 string
}

// ParseIndonesianMobile parses and validates an Indonesian mobile number from
// free-form user input. Returns a ConfigError when the value cannot be a valid
// Indonesian mobile number.
func ParseIndonesianMobile(input string) (IndonesianMobileNumber, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return IndonesianMobileNumber{}, core.NewConfigError(
			"Enter a valid Indonesian mobile number", nil)
	}

	digits := stripNonDigits(input)
	if digits == "" {
		return IndonesianMobileNumber{}, core.NewConfigError(
			"Enter a valid Indonesian mobile number", nil)
	}

	// Peel international access code, country code, then trunk zero.
	digits = strings.TrimPrefix(digits, "00")
	digits = strings.TrimPrefix(digits, IndonesiaCountryCode)
	digits = strings.TrimLeft(digits, "0")

	// Indonesian mobile subscriber numbers always start with 8.
	if !strings.HasPrefix(digits, "8") ||
		len(digits) < minSubscriberDigits ||
		len(digits) > maxSubscriberDigits {
		return IndonesianMobileNumber{}, core.NewConfigError(
			"Enter a valid Indonesian mobile number", nil)
	}

	return IndonesianMobileNumber{
		CountryCode: IndonesiaCountryCode,
		Subscriber:  digits,
		National:    "0" + digits,
		E164:        IndonesiaCountryCode + digits,
	}, nil
}

func stripNonDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FormatIndonesianPhone formats a number for display, e.g. "0812-3456-7890".
func FormatIndonesianPhone(subscriber string) string {
	if len(subscriber) < 4 {
		return subscriber
	}
	return fmt.Sprintf("0%s-%s-%s",
		subscriber[:3], subscriber[3:len(subscriber)-4], subscriber[len(subscriber)-4:])
}