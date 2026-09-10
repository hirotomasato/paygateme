package shopee

import "testing"

func TestParseShopeeAmount(t *testing.T) {
	cases := []struct {
		input string
		want  int64
		ok    bool
	}{
		// Plain digits.
		{"30000", 30000, true},
		{"0", 0, true},
		// Valid Indonesian grouped format.
		{"30.000", 30000, true},
		{"1.250.000", 1250000, true},
		{"3.001", 3001, true},
		// Ambiguous / invalid formats must be rejected.
		{"30.00", 0, false},     // parseFloat would yield 30
		{"30.000.00", 0, false}, // malformed grouping
		{"30,000", 0, false},    // comma separator
		{"-100", 0, false},      // negative
		{"1e5", 0, false},       // scientific notation
		{"30.000.5", 0, false},  // trailing fractional
		{"", 0, false},          // empty
		{" 30.000 ", 30000, true},
		{"Rp30.000", 0, false}, // currency symbol
		{"30 000", 0, false},   // internal whitespace
		{".000", 0, false},     // leading dot
		{"30.", 0, false},      // trailing dot
	}

	for _, c := range cases {
		got, ok := ParseShopeeAmount(c.input)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseShopeeAmount(%q) = (%d, %v), want (%d, %v)",
				c.input, got, ok, c.want, c.ok)
		}
	}
}

func TestParseShopeeAmountLargeValue(t *testing.T) {
	// Values beyond any realistic payment must still parse exactly.
	got, ok := ParseShopeeAmount("9.223.372.036.854.775.807")
	if !ok {
		t.Fatal("expected large grouped value to parse")
	}
	if got != 9223372036854775807 {
		t.Fatalf("got %d, want max int64", got)
	}
}
