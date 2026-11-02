package gopay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"081234567890", "81234567890"},
		{"+62 812-3456-7890", "6281234567890"},
		{"008123456789", "8123456789"},
		{"81234567890", "81234567890"},
	}

	for _, tc := range cases {
		got := normalizePhone(tc.input)
		if got != tc.want {
			t.Errorf("normalizePhone(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestAuthClientRequestOTPAndVerify(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case epLoginRequest:
			resp := GoIDEnvelope{
				Success: true,
				Data:    json.RawMessage(`{"otp_token":"mock-otp-token-xyz"}`),
			}
			_ = json.NewEncoder(w).Encode(resp)
		case epToken:
			payload := GoIDTokenPayload{
				AccessToken:  "mock-access-token",
				RefreshToken: "mock-refresh-token",
				TokenType:    "Bearer",
				ExpiresIn:    3600,
			}
			_ = json.NewEncoder(w).Encode(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	http := NewHTTPClient(server.URL, nil)
	auth := NewAuthClient(http, "test-client-id")

	// 1. Request OTP
	res, err := auth.RequestOTP(context.Background(), "08123456789", "62")
	if err != nil {
		t.Fatalf("RequestOTP failed: %v", err)
	}
	if res.OTPToken != "mock-otp-token-xyz" {
		t.Errorf("expected OTPToken 'mock-otp-token-xyz', got %q", res.OTPToken)
	}

	// 2. Verify OTP
	tokens, err := auth.VerifyOTP(context.Background(), "1234", res.OTPToken)
	if err != nil {
		t.Fatalf("VerifyOTP failed: %v", err)
	}
	if tokens.AccessToken != "mock-access-token" {
		t.Errorf("expected access token 'mock-access-token', got %q", tokens.AccessToken)
	}
	if tokens.RefreshToken != "mock-refresh-token" {
		t.Errorf("expected refresh token 'mock-refresh-token', got %q", tokens.RefreshToken)
	}

	// 3. Refresh Token
	refreshed, err := auth.Refresh(context.Background(), tokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if refreshed.AccessToken != "mock-access-token" {
		t.Errorf("expected refreshed access token 'mock-access-token', got %q", refreshed.AccessToken)
	}
}

