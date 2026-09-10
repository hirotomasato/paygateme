package shopee

import (
	"testing"
)

// Regression: JSON numbers arrive as float64; rendering them with %v yields
// scientific notation ("1.7190191e+07") which silently fails the feed scope
// check and leaves every payment pending forever.
func TestJSONNumberToString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{float64(17190191), "17190191"},
		{float64(22420988), "22420988"},
		{float64(3), "3"},
		{"17190191", "17190191"},
		{nil, "<nil>"},
	}
	for _, c := range cases {
		if got := jsonNumberToString(c.in); got != c.want {
			t.Errorf("jsonNumberToString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeTransactionAcceptsFloatIDRow(t *testing.T) {
	raw := map[string]any{
		"transactionId": "TX-1",
		"amount":        "10.001",
		"createTime":    float64(1788934058),
		"merchantId":    float64(17190191),
		"storeId":       float64(22420988),
		"status":        float64(3),
		"service":       float64(1),
	}
	tx := normalizeTransaction(raw, "17190191", "22420988")
	if tx == nil {
		t.Fatal("row with float64 ids must normalize, got nil")
	}
	if tx.Status != "completed" {
		t.Fatalf("status = %q, want completed", tx.Status)
	}
	if tx.Amount != 10001 {
		t.Fatalf("amount = %d, want 10001", tx.Amount)
	}
}
