package qris

import (
	"strconv"

	"github.com/hirotomasato/paygateme/core"
)

// StaticToDynamicQris converts a static QRIS payload into a dynamic one
// carrying a fixed amount. It injects tag 54 (transaction amount), flips the
// point-of-initiation method (tag 01) from static (11) to dynamic (12), and
// recomputes the CRC.
func StaticToDynamicQris(staticPayload string, amount int64) (string, error) {
	if amount <= 0 {
		return "", core.NewBaseError(core.CodeQRISParseError,
			"QRIS amount must be a positive integer")
	}

	// Verify the source checksum before trusting the payload. A corrupted or
	// hand-edited payload (e.g. a swapped merchant PAN) would otherwise be
	// re-emitted as a perfectly valid QR redirecting the payment.
	if !IsValidQRISChecksum(staticPayload) {
		return "", core.NewBaseError(core.CodeQRISParseError,
			"QRIS checksum is invalid; refusing to build a payable QR from it")
	}

	m, err := ParseEmv(staticPayload)
	if err != nil {
		return "", err
	}

	if _, ok := m[TagPayloadFormat]; !ok {
		return "", core.NewBaseError(core.CodeQRISParseError,
			"Input does not look like a QRIS payload (missing tag 00)")
	}

	m[TagPointOfInitiation] = POIDynamic
	m[TagTransactionAmount] = strconv.FormatInt(amount, 10)

	return BuildEmv(m)
}
