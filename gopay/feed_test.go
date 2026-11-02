package gopay

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hirotomasato/paygateme/core"
)

func TestToWholeRupiah(t *testing.T) {
	cases := []struct {
		minor float64
		want  int64
	}{
		{minor: 100000, want: 1000},
		{minor: 300100, want: 3001},
		{minor: 0, want: 0},
		{minor: math.NaN(), want: 0},
	}

	for _, tc := range cases {
		got := toWholeRupiah(tc.minor)
		if got != tc.want {
			t.Errorf("toWholeRupiah(%v) = %d, want %d", tc.minor, got, tc.want)
		}
	}
}

func TestNormalizeTransaction(t *testing.T) {
	raw := rawTransaction{
		ID:                "gopay-tx-123",
		OrderID:           "order-abc",
		MerchantID:        "m-456",
		TransactionStatus: "settlement",
		PaymentType:       "qris",
		GrossAmount:       500000, // Rp 5.000 in minor units
		RealGrossAmount:   500000,
		Currency:          "IDR",
		TransactionTime:   "2026-11-02T10:00:00Z",
		SettlementTime:    "2026-11-02T10:01:00Z",
		TransactionSource: "web",
	}

	tx := normalizeTransaction(raw)
	if tx.GrossAmount != 5000 {
		t.Fatalf("expected gross amount 5000, got %d", tx.GrossAmount)
	}

	coreTx := tx.ToCoreTransaction()
	if coreTx.ID != "gopay-tx-123" {
		t.Errorf("expected ID gopay-tx-123, got %s", coreTx.ID)
	}
	if coreTx.Amount != 5000 {
		t.Errorf("expected Amount 5000, got %d", coreTx.Amount)
	}
	if coreTx.Status != "settlement" {
		t.Errorf("expected Status settlement, got %s", coreTx.Status)
	}
	if coreTx.Time.IsZero() {
		t.Errorf("expected valid transaction time, got zero")
	}
}

func TestTransactionFeedListRecent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != epTransactions {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("merchant_ids") != "m-789" {
			t.Errorf("unexpected merchant_ids: %s", r.URL.Query().Get("merchant_ids"))
		}

		resp := map[string]any{
			"from":  0,
			"size":  10,
			"total": 1,
			"transactions": []map[string]any{
				{
					"id":                 "tx-001",
					"order_id":           "ord-001",
					"merchant_id":        "m-789",
					"transaction_status": "settlement",
					"payment_type":       "qris",
					"gross_amount":       150100.0,
					"currency":           "IDR",
					"transaction_time":   "2026-11-02T10:00:00Z",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, nil)
	feed := NewTransactionFeed(client)

	query := core.TransactionFeedQuery{
		Scope: core.PaymentScope{
			Provider:   ProviderID,
			AccountID:  "acc-123",
			MerchantID: "m-789",
		},
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		PageSize:  10,
	}

	res, err := feed.ListRecent(context.Background(), query)
	if err != nil {
		t.Fatalf("feed.ListRecent failed: %v", err)
	}
	if len(res.Transactions) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(res.Transactions))
	}
	if res.Transactions[0].Amount != 1501 {
		t.Errorf("expected amount 1501, got %d", res.Transactions[0].Amount)
	}
}

