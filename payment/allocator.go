// Package payment implements amount allocation, settlement matching, and the
// reconciliation orchestrator shared by every provider adapter.
package payment

import (
	"github.com/hirotomasato/paygateme/core"
)

// AmountAllocator allocates a unique whole-rupiah offset so concurrent orders
// can be told apart purely by the amount that lands in the merchant account.
//
// Uniqueness is enforced on the resulting amount (baseAmount + offset), not on
// the offset alone. Scoping per base amount is not enough: `3500+1` and
// `3499+2` both settle at `3501`.
type AmountAllocator struct {
	maxOffset int64
}

// NewAmountAllocator creates an allocator with the given offset window
// [1, maxOffset]. maxOffset must be positive.
func NewAmountAllocator(maxOffset int64) (*AmountAllocator, error) {
	if maxOffset < 1 {
		return nil, core.NewConfigError("maxOffset must be a positive integer", nil)
	}
	return &AmountAllocator{maxOffset: maxOffset}, nil
}

// DefaultAmountAllocator returns an allocator with the default offset window.
func DefaultAmountAllocator() *AmountAllocator {
	a, _ := NewAmountAllocator(core.DefaultMaxUniqueOffset)
	return a
}

// Max returns the largest offset this allocator will hand out.
func (a *AmountAllocator) Max() int64 { return a.maxOffset }

// Allocate finds the smallest offset in [1, maxOffset] whose resulting amount
// (baseAmount + offset) is not claimed by an active payment. It returns a
// CodeAmountPoolExhausted error when every slot in range is claimed.
func (a *AmountAllocator) Allocate(baseAmount int64, taken map[int64]bool) (int64, error) {
	for offset := int64(1); offset <= a.maxOffset; offset++ {
		if !taken[baseAmount+offset] {
			return offset, nil
		}
	}
	return 0, core.NewBaseErrorf(core.CodeAmountPoolExhausted,
		"No free unique amount slot available for base amount %d (offset window 1..%d is fully claimed)",
		baseAmount, a.maxOffset)
}
