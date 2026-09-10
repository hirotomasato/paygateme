package payment

import (
	"testing"
	"time"

	"github.com/hirotomasato/paygateme/core"
)

func mkPayment(uniqueAmount int64, created, expires time.Time) core.Payment {
	return core.Payment{
		ID:           "p",
		BaseAmount:   uniqueAmount - 1,
		UniqueOffset: 1,
		UniqueAmount: uniqueAmount,
		Status:       core.PaymentPending,
		CreatedAt:    created,
		ExpiresAt:    expires,
	}
}

func mkTx(id string, amount int64, status string, at time.Time) core.MerchantTransaction {
	return core.MerchantTransaction{ID: id, Amount: amount, Status: status, Time: at}
}

func TestMatchesPaymentAmount(t *testing.T) {
	now := time.Now()
	p := mkPayment(10001, now, now.Add(5*time.Minute))

	if !MatchesPayment(p, mkTx("a", 10001, "settlement", now.Add(time.Minute)), time.Minute) {
		t.Fatal("expected exact amount match")
	}
	if MatchesPayment(p, mkTx("b", 10002, "settlement", now.Add(time.Minute)), time.Minute) {
		t.Fatal("wrong amount must not match")
	}
}

func TestMatchesPaymentStatusFailClosed(t *testing.T) {
	now := time.Now()
	p := mkPayment(10001, now, now.Add(5*time.Minute))

	// Known failure statuses must not settle.
	for _, status := range []string{"failed", "pending", "shopee:4", "cancelled"} {
		if MatchesPayment(p, mkTx("x", 10001, status, now.Add(time.Minute)), time.Minute) {
			t.Fatalf("status %q must not match", status)
		}
	}
}

func TestMatchesPaymentFailOpenEmptyStatus(t *testing.T) {
	now := time.Now()
	p := mkPayment(10001, now, now.Add(5*time.Minute))

	// Empty status is treated as success (fail-open).
	if !MatchesPayment(p, mkTx("a", 10001, "", now.Add(time.Minute)), time.Minute) {
		t.Fatal("empty status must match (fail-open)")
	}
	// Whitespace-only also counts as empty.
	if !MatchesPayment(p, mkTx("b", 10001, "  ", now.Add(time.Minute)), time.Minute) {
		t.Fatal("whitespace status must match (fail-open)")
	}
}

func TestMatchesPaymentTimingWindow(t *testing.T) {
	now := time.Now()
	p := mkPayment(10001, now, now.Add(5*time.Minute))
	skew := time.Minute

	// Inside window (with skew tolerance) matches.
	if !MatchesPayment(p, mkTx("a", 10001, "settlement", now.Add(-30*time.Second)), skew) {
		t.Fatal("skewed early transaction should match")
	}
	if !MatchesPayment(p, mkTx("b", 10001, "settlement", now.Add(5*time.Minute+30*time.Second)), skew) {
		t.Fatal("skewed late transaction should match")
	}
	// Far outside window must not match.
	if MatchesPayment(p, mkTx("c", 10001, "settlement", now.Add(-1*time.Hour)), skew) {
		t.Fatal("transaction far before window must not match")
	}
}

func TestMatchesPaymentUnparseableTimeFallsBack(t *testing.T) {
	now := time.Now()
	p := mkPayment(10001, now, now.Add(5*time.Minute))
	// Zero time = unparseable; falls back to amount + status only.
	if !MatchesPayment(p, mkTx("a", 10001, "settlement", time.Time{}), time.Minute) {
		t.Fatal("unparseable time must fall back to amount+status")
	}
}

func TestReconcileConsumesTransactionOnce(t *testing.T) {
	now := time.Now()
	// Two pending payments with the same amount would both accept the same
	// transaction, but reconcile must assign it to at most one.
	a := mkPayment(10001, now, now.Add(5*time.Minute))
	b := mkPayment(10001, now, now.Add(5*time.Minute))
	b.ID = "q"

	tx := mkTx("t1", 10001, "settlement", now.Add(time.Minute))
	matches := Reconcile([]core.Payment{a, b}, []core.MerchantTransaction{tx}, time.Minute)

	if len(matches) != 1 {
		t.Fatalf("reconcile returned %d matches, want 1", len(matches))
	}
}
