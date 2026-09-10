// Package gopay implements the GoPay / GoBiz merchant provider adapter.
// It reuses the provider-agnostic payment, qris, and core packages from
// paygateme — only the GoPay-specific auth, merchant, feed, and token
// management live here.
package gopay

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hirotomasato/paygateme/core"
)

// ---- Auth ----

// TokenSet is an OAuth-style token pair returned by the GoID token endpoint.
type TokenSet struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	// Absolute expiry as epoch milliseconds, when derivable from the response.
	ExpiresAt int64  `json:"expiresAt,omitempty"`
	TokenType string `json:"tokenType"`
}

// SessionState is a persisted authentication session.
type SessionState struct {
	Tokens TokenSet `json:"tokens"`
	// Stable device identifier for the x-uniqueid header across sessions.
	DeviceID string `json:"deviceId,omitempty"`
	// When the session was last refreshed (epoch ms), for debugging.
	LastRefreshedAt int64 `json:"lastRefreshedAt,omitempty"`
}

// LoginRequestResult is the result of requesting an OTP.
type LoginRequestResult struct {
	OTPToken string          `json:"otpToken,omitempty"`
	Raw      json.RawMessage `json:"raw"`
}

// ---- Merchant ----

// MerchantProfile is a normalized merchant profile.
type MerchantProfile struct {
	ID           string           `json:"id"`
	MerchantName string           `json:"merchantName"`
	OutletName   string           `json:"outletName,omitempty"`
	Phone        string           `json:"phone,omitempty"`
	Email        string           `json:"email,omitempty"`
	ServerKey    string           `json:"serverKey,omitempty"`
	ClientKey    string           `json:"clientKey,omitempty"`
	Timezone     string           `json:"timezone,omitempty"`
	Outlets      []MerchantOutlet `json:"outlets"`
	Raw          json.RawMessage  `json:"raw"`
}

// MerchantOutlet is a point-of-payment with its GoPay static QRIS.
type MerchantOutlet struct {
	PopID      string `json:"popId"`
	Name       string `json:"name,omitempty"`
	Status     string `json:"status,omitempty"`
	ReceiverID string `json:"receiverId,omitempty"`
	// The static EMVCo QRIS payload for this outlet (from aspi_qr_string).
	QRString string          `json:"qrString,omitempty"`
	Raw      json.RawMessage `json:"raw"`
}

// StoredMerchant is a persisted merchant record with resolved outlets.
type StoredMerchant struct {
	ID           string           `json:"id"`
	MerchantName string           `json:"merchantName"`
	OutletName   string           `json:"outletName,omitempty"`
	Phone        string           `json:"phone,omitempty"`
	Email        string           `json:"email,omitempty"`
	BusinessType string           `json:"businessType,omitempty"`
	MerchantType string           `json:"merchantType,omitempty"`
	ServiceArea  string           `json:"serviceArea,omitempty"`
	Outlets      []MerchantOutlet `json:"outlets"`
	// The primary outlet's static QRIS, when available.
	QRString string          `json:"qrString,omitempty"`
	Raw      json.RawMessage `json:"raw"`
}

// CurrentUser is the resolved authenticated user from /v1/users/me.
type CurrentUser struct {
	MerchantID string `json:"merchantId"`
}

// ---- Transaction ----

// GoPayTransaction is a single transaction as returned by the merchant analytics API.
// Amounts are in whole rupiah (the feed sends minor units; the adapter divides by 100).
type GoPayTransaction struct {
	ID                string          `json:"id"`
	OrderID           string          `json:"orderId"`
	MerchantID        string          `json:"merchantId"`
	Status            string          `json:"status"`
	PaymentType       string          `json:"paymentType"`
	GrossAmount       int64           `json:"grossAmount"`
	RealGrossAmount   int64           `json:"realGrossAmount,omitempty"`
	Currency          string          `json:"currency"`
	TransactionTime   string          `json:"transactionTime"`
	SettlementTime    string          `json:"settlementTime,omitempty"`
	TransactionSource string          `json:"transactionSource,omitempty"`
	Raw               json.RawMessage `json:"raw"`
}

// TransactionQuery is a filter for listing transactions.
type TransactionQuery struct {
	From         int64     `json:"from"`
	Size         int       `json:"size"`
	Statuses     []string  `json:"statuses,omitempty"`
	PaymentTypes []string  `json:"paymentTypes,omitempty"`
	StartTime    time.Time `json:"startTime"`
	EndTime      time.Time `json:"endTime"`
}

// ---- Ports ----

// TokenRefresher is the single capability TokenManager needs from the auth layer.
type TokenRefresher interface {
	Refresh(ctx context.Context, refreshToken string) (*TokenSet, error)
}

// TransactionLister is the single capability PaymentService needs from the feed.
type TransactionLister interface {
	List(merchantID string, query TransactionQuery) ([]GoPayTransaction, error)
}

// ---- Provider config ----

// ProviderConfig configures a GoPayProvider.
type ProviderConfig struct {
	// Restore a previous session instead of logging in again.
	Session *SessionState
	// Merchant static QRIS payload to derive dynamic per-order QRIS strings.
	StaticQRIS string
	// GoID client id override.
	ClientID string
	// App version header override.
	AppVersion string
	// Stable device identifier; restored from session or generated when omitted.
	DeviceID string
	// Custom payment store. Defaults to in-memory.
	Store core.PaymentStore
	// Polling and payment tuning (milliseconds; zero = defaults).
	PollInterval  int64
	DefaultExpiry int64
	ClockSkew     int64
	// Refresh tokens this many milliseconds before expiry. Default: 5 minutes.
	RefreshBeforeExpiryMs int64
	// Callback invoked after automatic token refresh for persistence.
	OnTokenRefreshed func(session SessionState) error
	// Maximum unique-amount offset for dynamic QRIS amount encoding.
	MaxUniqueOffset int
}
