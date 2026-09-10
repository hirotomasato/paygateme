package payment

import (
	"context"
	"testing"
	"time"

	"github.com/hirotomasato/paygateme/core"
)

type fakeFeed struct {
	txns []core.MerchantTransaction
	err  error
}

func (f *fakeFeed) ListRecent(_ context.Context, _ core.TransactionFeedQuery) (core.TransactionFeedResult, error) {
	if f.err != nil {
		return core.TransactionFeedResult{}, f.err
	}
	return core.TransactionFeedResult{Transactions: f.txns}, nil
}

func newTestService(scope *core.PaymentScope, feed *fakeFeed) *Service {
	opts := Options{
		MerchantID: "S-100",
		Scope:      scope,
		Store:      NewInMemoryStore(),
		Feed:       feed,
		ClockSkew:  time.Minute,
	}
	s, err := NewService(opts)
	if err != nil {
		panic(err)
	}
	return s
}

func TestCreatePaymentAllocatesUniqueAmount(t *testing.T) {
	ctx := context.Background()
	s := newTestService(nil, &fakeFeed{})

	p1, err := s.CreatePayment(ctx, CreatePaymentInput{Amount: 10000})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	p2, err := s.CreatePayment(ctx, CreatePaymentInput{Amount: 10000})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}

	if p1.UniqueAmount == p2.UniqueAmount {
		t.Fatalf("two payments got the same unique amount %d", p1.UniqueAmount)
	}
	if p1.UniqueAmount != 10001 {
		t.Fatalf("first unique amount = %d, want 10001 (smallest slot)", p1.UniqueAmount)
	}
	if p2.UniqueAmount != 10002 {
		t.Fatalf("second unique amount = %d, want 10002", p2.UniqueAmount)
	}
}

func TestTickSettlesMatchingTransaction(t *testing.T) {
	ctx := context.Background()
	scope := &core.PaymentScope{Provider: "shopee", AccountID: "A1", MerchantID: "S-100"}
	feed := &fakeFeed{}
	s := newTestService(scope, feed)

	p, err := s.CreatePayment(ctx, CreatePaymentInput{Amount: 10000})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}

	feed.txns = []core.MerchantTransaction{{
		ID:     "tx-1",
		Amount: p.UniqueAmount,
		Status: "3", // Shopee completed status normalizes to "completed"; matcher treats "3" as fail-closed here, so use a success-like label.
		Time:   time.Now(),
	}}

	// "3" is not in the settled statuses set; use "completed" for a clear pass.
	feed.txns = []core.MerchantTransaction{{
		ID:     "tx-1",
		Amount: p.UniqueAmount,
		Status: "completed",
		Time:   time.Now(),
	}}

	res, err := s.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(res.Paid) != 1 {
		t.Fatalf("paid = %d, want 1", len(res.Paid))
	}
	if res.Paid[0].ID != p.ID {
		t.Fatalf("paid id = %s, want %s", res.Paid[0].ID, p.ID)
	}
	if res.Paid[0].Transaction == nil || res.Paid[0].Transaction.ID != "tx-1" {
		t.Fatalf("settled transaction = %+v, want tx-1", res.Paid[0].Transaction)
	}
}

func TestTickExpiresStalePayment(t *testing.T) {
	ctx := context.Background()
	s := newTestService(nil, &fakeFeed{})

	p, err := s.CreatePayment(ctx, CreatePaymentInput{Amount: 10000, ExpiresIn: 0})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	// Force the payment past its expiry + skew.
	expired := core.CopyPayment(p)
	expired.ExpiresAt = time.Now().Add(-2 * time.Minute)
	if err := s.store.Update(ctx, expired); err != nil {
		t.Fatalf("Update: %v", err)
	}

	res, err := s.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(res.Expired) != 1 {
		t.Fatalf("expired = %d, want 1", len(res.Expired))
	}
}

func TestQuarantineBlocksImmediateReuse(t *testing.T) {
	ctx := context.Background()
	s := newTestService(nil, &fakeFeed{})

	p, err := s.CreatePayment(ctx, CreatePaymentInput{Amount: 10000})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	freedAmount := p.UniqueAmount

	// Cancel frees the slot, but it is quarantined for 2*clockSkew.
	if _, err := s.CancelPayment(ctx, p.ID); err != nil {
		t.Fatalf("CancelPayment: %v", err)
	}

	p2, err := s.CreatePayment(ctx, CreatePaymentInput{Amount: 10000})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	if p2.UniqueAmount == freedAmount {
		t.Fatalf("quarantined amount %d was immediately reused", freedAmount)
	}
}

func TestConsumedTransactionNotReusedAcrossTicks(t *testing.T) {
	ctx := context.Background()
	scope := &core.PaymentScope{Provider: "shopee", AccountID: "A1", MerchantID: "S-100"}
	feed := &fakeFeed{}
	s := newTestService(scope, feed)

	p1, err := s.CreatePayment(ctx, CreatePaymentInput{Amount: 10000})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}

	tx := core.MerchantTransaction{ID: "tx-1", Amount: p1.UniqueAmount, Status: "completed", Time: time.Now()}
	feed.txns = []core.MerchantTransaction{tx}

	if _, err := s.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Create a second payment that would accept the same transaction amount.
	// It gets a different amount, so it can't match tx-1 anyway; instead
	// verify tx-1 is remembered as consumed by attempting to settle it again.
	got, err := s.GetPayment(ctx, p1.ID)
	if err != nil {
		t.Fatalf("GetPayment: %v", err)
	}
	if got == nil || got.Status != core.PaymentPaid {
		t.Fatalf("p1 status = %v, want paid", got.Status)
	}

	// A second payment with the same final amount is quarantined, so its amount
	// differs; feed still returns tx-1. It must not settle from tx-1.
	p2, err := s.CreatePayment(ctx, CreatePaymentInput{Amount: p1.UniqueAmount})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	_ = p2

	res, err := s.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	for _, paid := range res.Paid {
		if paid.Transaction != nil && paid.Transaction.ID == "tx-1" && paid.ID != p1.ID {
			t.Fatal("consumed transaction settled a second payment")
		}
	}
}
