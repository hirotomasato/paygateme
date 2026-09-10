// Package core constants shared across all providers.
package core

import "time"

// Payment-domain defaults.
const (
	// DefaultPollInterval is the delay between background reconciliation ticks.
	DefaultPollInterval = 3 * time.Second

	// DefaultPaymentExpiry is how long a payment stays pending before expiring.
	DefaultPaymentExpiry = 5 * time.Minute

	// DefaultClockSkew is the tolerance added to payment windows and matching
	// ranges so a slightly off-clock transaction or feed indexing delay is not
	// falsely rejected.
	DefaultClockSkew = 60 * time.Second

	// DefaultTransactionLookback is the rolling window the poller scans the
	// transaction feed. It's a rolling window rather than a calendar day to
	// avoid timezone/day-boundary gaps between the runtime (often UTC) and the
	// merchant's local day.
	DefaultTransactionLookback = 24 * time.Hour

	// DefaultRequestTimeout is the default HTTP request timeout.
	DefaultRequestTimeout = 20 * time.Second
)

// Feed pagination.
const (
	// MaxTransactionPageSize is the largest page size the feed accepts. For
	// GoPay, exceeding this triggers HTTP 422. Adapters clamp to this ceiling.
	MaxTransactionPageSize = 100

	// DefaultTransactionPageSize is the default feed page size.
	DefaultTransactionPageSize = MaxTransactionPageSize

	// MaxTransactionPagesPerTick is the safety ceiling for provider-owned
	// pagination in one reconciliation pass.
	MaxTransactionPagesPerTick = 10
)

// Amount allocator.
const (
	// DefaultMaxUniqueOffset is the amount uniqueness window. QRIS amounts are
	// whole rupiah, so unique "cents" are encoded as an integer offset added to
	// the base amount.
	DefaultMaxUniqueOffset = 999

	// TransactionAmountScale is the divisor for converting GoPay merchant-
	// analytics amounts (minor unit) into whole rupiah.
	TransactionAmountScale = 100
)

// GoPay defaults — moved here from gopay package since the payment core and
// GoPay adapter both reference them. Shopee-specific constants stay in the
// shopee package.
const (
	GoBizAPIBaseURL = "https://api.gobiz.co.id"
	GojekAPIBaseURL = "https://api.gojekapi.com"

	DefaultGoIDClientID = "go-biz-web-new"
	DefaultAppVersion   = "platform-v3.109.0-d4b20f12"
)

// Transaction status labels that represent money actually received by the
// merchant. Compared case-insensitively by the matcher.
var PaidTransactionStatuses = []string{"settlement", "capture"}

// DefaultPaymentTypes is the payment types the gateway polls for.
var DefaultPaymentTypes = []string{"qris", "gopay"}