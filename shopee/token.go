package shopee

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/hirotomasato/paygateme/core"
)

// MerchantCredential is the inner merchant token extracted from Shopee's signed
// dashboard JWT cookie.
type MerchantCredential struct {
	Token      string
	AccountID  string
	BusinessID string
	ExpiresAt  int64 // epoch ms; 0 = unknown
}

// ReadMerchantCredential extracts the inner merchant token from the dashboard
// JWT cookie.
func ReadMerchantCredential(jar *CookieJar) (*MerchantCredential, error) {
	jwt := jar.Get(LiveTokenCookie, PartnerBaseURL)
	if jwt == "" {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"Shopee login did not return a merchant session token", nil)
	}

	payload, err := decodeJWTPayload(jwt)
	if err != nil {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"Shopee returned an unreadable merchant session token", err)
	}

	token, _ := payload["token"].(string)
	if token == "" {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"Shopee returned an unreadable merchant session token", nil)
	}

	accountID := toStringValue(payload["userid"])
	if accountID == "" {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"Shopee returned an unreadable merchant session token", nil)
	}

	businessID := toStringValue(payload["businessId"])

	expiresAt := int64(0)
	if exp, ok := payload["exp"].(float64); ok {
		expiresAt = int64(exp) * 1000
	}

	return &MerchantCredential{
		Token:      token,
		AccountID:  accountID,
		BusinessID: businessID,
		ExpiresAt:  expiresAt,
	}, nil
}

func decodeJWTPayload(jwt string) (map[string]any, error) {
	segments := strings.Split(jwt, ".")
	if len(segments) != 3 || segments[1] == "" {
		return nil, core.NewBaseError(core.CodeAuthFailed, "invalid JWT")
	}

	payloadB64 := segments[1]
	payloadB64 = strings.ReplaceAll(payloadB64, "-", "+")
	payloadB64 = strings.ReplaceAll(payloadB64, "_", "/")
	payloadB64 = strings.TrimRight(payloadB64, "=")

	payloadBytes, err := base64.RawStdEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func toStringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return formatInt(int64(t))
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

func formatInt(n int64) string {
	return json.Number(fmtInt64(n)).String()
}

func fmtInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
