package gopay

// Base URLs and endpoint paths derived from the GoBiz merchant dashboard
// network flow. Required for the private API to accept requests.
const (
	// GoBiz merchant API host.
	GoBizAPIBaseURL = "https://api.gobiz.co.id"
	// GoID/Gojek host (login/token flows and transaction feed).
	GojekAPIBaseURL = "https://api.gojekapi.com"

	// Endpoint paths.
	epLoginRequest    = "/goid/login/request"
	epToken           = "/goid/token"
	epUsersMe         = "/v1/users/me"
	epMerchantsSearch = "/v1/merchants/search"
	epTransactions    = "/merchant-analytics/v2/merchants/transactions"
)

// merchantDetail returns the endpoint path for a specific merchant.
func merchantDetail(merchantID string) string {
	return "/v1/merchants/" + merchantID
}

// GoID client identifiers used by the web dashboard. They can be overridden
// through the client configuration when Gojek rotates them.
const (
	DefaultGoIDClientID = "go-biz-web-new"
	DefaultAppID        = "go-biz-web-dashboard"
	DefaultAppVersion   = "platform-v3.109.0-d4b20f12"
)

// DefaultStaticHeaders returns the baseline headers required by the GoID/GoBiz
// gateway. The dashboard identifies itself as a web merchant client.
func DefaultStaticHeaders() map[string]string {
	return map[string]string{
		"Accept":              "application/json, text/plain, */*",
		"Authentication-Type": "go-id",
		"X-PhoneMake":         "Web",
		"X-PhoneModel":        "Node.js Client",
		"x-DeviceOS":          "Web",
		"X-User-Locale":       "id",
		"Gojek-Country-Code":  "ID",
		"Gojek-Timezone":      "Asia/Jakarta",
		"X-Platform":          "Web",
		"X-User-Type":         "merchant",
		"x-appId":             DefaultAppID,
	}
}

// Transaction statuses that represent money actually received by the merchant.
var PaidTransactionStatuses = []string{"settlement", "capture"}

// Payment types the gateway polls for. GoPay QRIS is the primary channel.
var DefaultPaymentTypes = []string{"qris", "gopay"}

// Polling, expiry, and request timeouts (milliseconds).
const (
	DefaultPollIntervalMS        = 3000
	DefaultPaymentExpiryMS       = 5 * 60 * 1000
	DefaultRequestTimeoutMS      = 20000
	DefaultTransactionLookbackMS = 24 * 60 * 60 * 1000
)

// Transaction feed page sizing.
const (
	// Largest size the merchant-analytics feed accepts (rejects >100 with 422).
	MaxTransactionPageSize = 100
	// How many transactions each feed page holds.
	DefaultTransactionPageSize = MaxTransactionPageSize
	// How many feed pages one reconciliation pass fetches at most.
	MaxTransactionPagesPerTick = 10
)

// DefaultMaxUniqueOffset is the amount-uniqueness window for dynamic QRIS.
const DefaultMaxUniqueOffset = 999

// TransactionAmountScale is the divisor converting merchant-analytics amounts
// (minor units) into whole rupiah.
const TransactionAmountScale = 100

// DefaultRefreshBeforeExpiryMS is the token pre-expiry refresh buffer.
const DefaultRefreshBeforeExpiryMS = 5 * 60 * 1000
