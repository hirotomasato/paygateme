// Package qris implements EMVCo/QRIS payload parsing and dynamic QR generation.
package qris

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/utils"
)

// EMVCo tag ids.
const (
	TagPayloadFormat      = "00"
	TagPointOfInitiation  = "01"
	TagTransactionAmount  = "54"
	TagCRC                = "63"
)

// Point-of-initiation values.
const (
	POIStatic  = "11"
	POIDynamic = "12"
)

// EmvTlvMap is a parsed EMVCo QR data object: tag id → raw value string.
// Nested templates keep their inner payload as the raw value.
type EmvTlvMap map[string]string

// ParseEmv parses an EMVCo/QRIS payload string into a flat TLV map. Each
// element is `<2-digit tag><2-digit length><value>`.
func ParseEmv(payload string) (EmvTlvMap, error) {
	// EMVCo lengths count bytes, not characters. Walking the UTF-8 bytes keeps
	// parsing correct for merchant names outside ASCII.
	bytes := []byte(payload)
	m := make(EmvTlvMap)
	cursor := 0

	for cursor < len(bytes) {
		malformed := func() error {
			return core.NewBaseError(core.CodeQRISParseError,
				fmt.Sprintf("Malformed QRIS payload near byte %d", cursor))
		}

		if cursor+4 > len(bytes) {
			return nil, malformed()
		}
		tag := string(bytes[cursor : cursor+2])
		cursor += 2

		lengthText := string(bytes[cursor : cursor+2])
		cursor += 2
		// A length must be exactly two digits.
		if !isTwoDigits(lengthText) {
			return nil, malformed()
		}
		length, _ := strconv.Atoi(lengthText)

		// Bound check: a truncated payload must not parse "successfully".
		if cursor+length > len(bytes) {
			return nil, malformed()
		}

		value := string(bytes[cursor : cursor+length])
		cursor += length
		m[tag] = value
	}

	return m, nil
}

// EncodeTLV serializes a single TLV element with a zero-padded two-digit
// length, measured in UTF-8 bytes. Values over 99 bytes are rejected.
func EncodeTLV(tag, value string) (string, error) {
	byteLength := len([]byte(value))
	if byteLength > 99 {
		return "", core.NewBaseError(core.CodeQRISParseError,
			fmt.Sprintf("QRIS tag %s value is %d bytes, exceeding the 99-byte limit", tag, byteLength))
	}
	return fmt.Sprintf("%s%02d%s", tag, byteLength, value), nil
}

// BuildEmv rebuilds an EMVCo payload from a TLV map and appends a freshly
// computed CRC. Tags are emitted in ascending numeric order except the CRC tag
// which is always last.
func BuildEmv(m EmvTlvMap) (string, error) {
	tags := make([]string, 0, len(m))
	for tag := range m {
		if tag != TagCRC {
			tags = append(tags, tag)
		}
	}
	sort.Slice(tags, func(i, j int) bool {
		ni, _ := strconv.Atoi(tags[i])
		nj, _ := strconv.Atoi(tags[j])
		return ni < nj
	})

	body := ""
	for _, tag := range tags {
		encoded, err := EncodeTLV(tag, m[tag])
		if err != nil {
			return "", err
		}
		body += encoded
	}

	// CRC is computed over the payload including the CRC tag id and length.
	withCrcHeader := body + TagCRC + "04"
	crc := utils.CRC16CCITT(withCrcHeader)
	return withCrcHeader + crc, nil
}

// isValidTwoDigits reports whether s is exactly two ASCII digits.
func isTwoDigits(s string) bool {
	if len(s) != 2 {
		return false
	}
	for i := 0; i < 2; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// IsValidQRISChecksum validates the trailing CRC of a QRIS payload.
func IsValidQRISChecksum(payload string) bool {
	if len(payload) < 8 {
		return false
	}
	withoutCrc := payload[:len(payload)-4]
	provided := payload[len(payload)-4:]
	// Compare case-insensitively by uppercasing the provided CRC.
	return utils.CRC16CCITT(withoutCrc) == toUpper(provided)
}

func toUpper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'f' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}
