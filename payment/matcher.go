package payment

import (
	"sort"
	"strings"
	"time"

	"github.com/hirotomasato/paygateme/core"
)

// settledStatuses are transaction status labels treated as success. Unknown or
// empty statuses are accepted (fail-open) to tolerate feed variations; known
// failure statuses are rejected.
var settledStatuses = map[string]bool{
	"settlement": true,
	"capture":    true,
	"success":    true,
	"paid":       true,
	"settled":    true,
	"completed":  true,
}

// MatchesPayment reports whether a transaction settles a given pending payment.
//
// Matching rules:
//   - The transaction amount must equal the payment's unique amount.
//   - The transaction time must fall within the payment's validity window
//     (created .. expires), with a small tolerance for clock skew.
//
// The amount is the primary discriminator; uniqueness of amounts across active
// payments is guaranteed by the allocator.
func MatchesPayment(payment core.Payment, tx core.MerchantTransaction, clockSkew time.Duration) bool {
	// The paid amount may be reported in gross or real gross depending on the
	// feed; accept either.
	amountMatches := tx.Amount == payment.UniqueAmount ||
		(tx.RealAmount != 0 && tx.RealAmount == payment.UniqueAmount)
	if !amountMatches {
		return false
	}

	// Only settle on success-like statuses so pending/failed transactions that
	// carry the same amount never settle a payment. Unknown/empty statuses are
	// treated as success (fail-open).
	status := strings.ToLower(strings.TrimSpace(tx.Status))
	if status != "" && !settledStatuses[status] {
		return false
	}

	// A zero transaction time means the adapter could not parse it; fall back
	// to amount + status only.
	if tx.Time.IsZero() {
		return true
	}

	windowStart := payment.CreatedAt.Add(-clockSkew)
	windowEnd := payment.ExpiresAt.Add(clockSkew)
	return !tx.Time.Before(windowStart) && !tx.Time.After(windowEnd)
}

// Match is a matched payment/transaction pair.
type Match struct {
	Payment     core.Payment
	Transaction core.MerchantTransaction
}

// Reconcile returns the pairs that match. Each transaction is consumed by at
// most one payment to avoid double-settling when amounts repeat outside their
// windows. Pending payments are ordered by ascending unique amount for
// deterministic assignment.
func Reconcile(pending []core.Payment, transactions []core.MerchantTransaction, clockSkew time.Duration) []Match {
	// Prefer the smallest unique amount first for deterministic assignment.
	ordered := make([]core.Payment, len(pending))
	copy(ordered, pending)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].UniqueAmount < ordered[j].UniqueAmount
	})

	matches := make([]Match, 0, len(pending))
	consumed := make(map[string]bool, len(transactions))

	for _, payment := range ordered {
		for _, tx := range transactions {
			if consumed[tx.ID] {
				continue
			}
			if MatchesPayment(payment, tx, clockSkew) {
				matches = append(matches, Match{Payment: payment, Transaction: tx})
				consumed[tx.ID] = true
				break
			}
		}
	}

	return matches
}
