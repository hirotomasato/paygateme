package payment

import (
	"testing"

	"github.com/hirotomasato/paygateme/core"
)

func TestAllocatePicksSmallestFreeSlot(t *testing.T) {
	a := DefaultAmountAllocator()
	taken := map[int64]bool{1001: true, 1002: true}
	offset, err := a.Allocate(1000, taken)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if offset != 3 {
		t.Fatalf("offset = %d, want 3", offset)
	}
}

func TestAllocateOnFinalAmount(t *testing.T) {
	// Two different base amounts landing on the same total must conflict:
	// 3500+1 and 3499+2 both settle at 3501.
	a := DefaultAmountAllocator()
	// 3501 is taken by another payment.
	taken := map[int64]bool{3501: true}
	offset, err := a.Allocate(3500, taken)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if offset == 1 {
		t.Fatal("offset 1 collides with a taken final amount")
	}
}

func TestAllocatePoolExhausted(t *testing.T) {
	a, err := NewAmountAllocator(2)
	if err != nil {
		t.Fatalf("NewAmountAllocator: %v", err)
	}
	taken := map[int64]bool{11: true, 12: true}
	if _, err := a.Allocate(10, taken); err == nil {
		t.Fatal("expected pool exhaustion error")
	} else if !core.IsErrorCode(err, core.CodeAmountPoolExhausted) {
		t.Fatalf("error code = %v, want AMOUNT_POOL_EXHAUSTED", err)
	}
}

func TestNewAmountAllocatorRejectsInvalidMax(t *testing.T) {
	if _, err := NewAmountAllocator(0); err == nil {
		t.Fatal("expected error for maxOffset 0")
	}
	if _, err := NewAmountAllocator(-1); err == nil {
		t.Fatal("expected error for negative maxOffset")
	}
}
