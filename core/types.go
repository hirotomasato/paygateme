// Package core defines the domain types, ports, and error hierarchy shared by
// all provider adapters. It has no runtime dependencies and no provider-specific
// knowledge — that stays in the adapter packages.
package core

import (
	"context"
	"time"
)

// ---- Identifiers ----

// PaymentScope identifies the provider account and merchant that own a payment.
// Every payment and reconciled transaction carries one so that payments from one
// provider or store can never settle orders from another.
type PaymentScope struct {
	// Stable provider id: "gopay" or "shopee".
	Provider string
	// Optional provider account id when available (GoPay: account id; Shopee:
	// business merchant id).
	AccountID string
	// Provider-native merchant/store id used to poll transactions.
	MerchantID string
}

// PaymentStatus is the lifecycle status of a payment tracked by the gateway.
type PaymentStatus string

const (
	PaymentPending   PaymentStatus = "pending"
	PaymentPaid      PaymentStatus = "paid"
	PaymentExpired   PaymentStatus = "expired"
	PaymentCancelled PaymentStatus = "cancelled"
)

// ---- Payment model ----

// Payment is a payment intent created by the gateway. Its uniqueAmount is the
// primary discriminator used by the settlement matcher.
type Payment struct {
	// Caller-facing identifier.
	ID string
	// Provider ownership. Omitted only on explicitly unscoped records.
	Scope *PaymentScope
	// The base amount requested by the merchant, in whole rupiah.
	BaseAmount int64
	// The unique offset added for disambiguation (1..maxOffset).
	UniqueOffset int64
	// The exact amount the buyer must transfer (baseAmount + uniqueOffset).
	UniqueAmount int64
	Status       PaymentStatus
	CreatedAt    time.Time
	ExpiresAt    time.Time
	// Optional caller-supplied reference (e.g. internal order id).
	Reference string
	// Dynamic QRIS payload string when a static QRIS was configured.
	QRString string
	// Matched transaction once paid.
	Transaction *MerchantTransaction
	// Opaque metadata attached by the caller.
	Metadata map[string]any
}

// Terminal reports whether the payment is in a terminal state.
func (p *Payment) Terminal() bool {
	switch p.Status {
	case PaymentPaid, PaymentExpired, PaymentCancelled:
		return true
	}
	return false
}

// ---- Transaction model ----

// MerchantTransaction is a single transaction as returned by the provider feed,
// normalized to whole rupiah and a common shape.
type MerchantTransaction struct {
	ID          string
	OrderID     string
	Status      string
	PaymentType string
	// Amount in whole rupiah. Provider adapters MUST normalize before returning
	// to core (GoPay divides by 100; Shopee parses "30.000" → 30000).
	Amount int64
	// Real gross amount (when the feed reports both gross and real_gross). May
	// be zero when not provided.
	RealAmount int64
	Currency   string
	// TransactionTime as reported by the provider feed. Converted to UTC by
	// the adapter.
	Time time.Time
	// Opaque raw payload for advanced use.
	Raw any
}

// ---- Ports ----

// TransactionFeed is the provider-neutral transaction capability consumed by
// PaymentService. Each adapter owns its cursor/offset semantics, status
// filtering, timestamps, and conversion from provider-specific money units to
// whole rupiah.
type TransactionFeed interface {
	// ListRecent returns transactions in the given window. The adapter owns
	// pagination: GoPay uses offset, Shopee uses cursor. Neither detail leaks
	// into core.
	ListRecent(ctx context.Context, query TransactionFeedQuery) (TransactionFeedResult, error)
}

// TransactionFeedQuery narrows the transaction feed window. The payment domain
// computes the time bounds; the adapter handles pagination limits.
type TransactionFeedQuery struct {
	Scope     PaymentScope
	StartTime time.Time
	EndTime   time.Time
	// Suggested page size. Providers may clamp it to their own API limit.
	PageSize int
	// Safety ceiling for provider-owned pagination.
	MaxPages int
}

// TransactionFeedResult is the outcome of a provider-normalized transaction scan.
type TransactionFeedResult struct {
	Transactions []MerchantTransaction
	// True when the provider hit its pagination ceiling before exhausting data.
	Truncated bool
	PagesFetched int
}

// PaymentStore persists active payments. InMemoryStore is provided for
// single-process use and tests. Multi-process deployments must provide a
// durable store (Redis, SQL) implementing this interface.
type PaymentStore interface {
	Create(ctx context.Context, payment Payment) error
	Update(ctx context.Context, payment Payment) error
	Get(ctx context.Context, id string) (*Payment, error)
	// ListActive returns all payments still occupying a unique amount slot.
	// When scope is non-nil, implementations should filter by it. Callers also
	// filter defensively when a custom store returns records from other scopes.
	ListActive(ctx context.Context, scope *PaymentScope) ([]Payment, error)
}

// --- Helpers ----

// SameScope compares two complete payment scopes for equality.
func SameScope(a, b PaymentScope) bool {
	return a.Provider == b.Provider &&
		a.MerchantID == b.MerchantID &&
		a.AccountID == b.AccountID
}

// CopyScope returns a shallow copy of s.
func CopyScope(s PaymentScope) PaymentScope {
	return s
}

// CopyPaymentScope returns a shallow copy of s, or nil when s is nil.
func CopyPaymentScope(s *PaymentScope) *PaymentScope {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

// CopyPayment returns a shallow copy of p. The scope and transaction pointers
// are copied; their contents are shared.
func CopyPayment(p Payment) Payment {
	if p.Scope != nil {
		scope := *p.Scope
		p.Scope = &scope
	}
	return p
}