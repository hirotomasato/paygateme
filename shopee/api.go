package shopee

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/utils"
)

// ---- Envelope Types ----

// AccountEnvelope is the Shopee account API response envelope.
type AccountEnvelope struct {
	Error int            `json:"error"`
	Msg   string         `json:"msg,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

// PartnerEnvelope is the Shopee partner API response envelope.
type PartnerEnvelope struct {
	Code    int            `json:"code"`
	Message string         `json:"message,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

// PaymentEnvelope is the ShopeePay API response envelope.
type PaymentEnvelope struct {
	Code  int            `json:"code"`
	Msg   string         `json:"msg,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
	Error int            `json:"error,omitempty"`
}

// APILocale configures language and timezone for Shopee API calls.
type APILocale struct {
	Language string
	Timezone string
}

// ---- Envelope Helpers ----

// RequireAccountData extracts the data field from an account envelope, or
// returns an error.
func RequireAccountData(env AccountEnvelope, endpoint string) (map[string]any, error) {
	if env.Error != 0 {
		code := fmt.Sprintf("%d", env.Error)
		if env.Error == AuthErrorNeedOTP {
			return env.Data, nil // NeedOTP is expected
		}
		msg := env.Msg
		if msg == "" {
			msg = fmt.Sprintf("Shopee returned error %d", env.Error)
		}
		return nil, core.NewAPIError(msg, code, nil)
	}
	if env.Data == nil {
		return nil, core.NewAPIError("Shopee returned empty data", "", nil)
	}
	return env.Data, nil
}

// RequirePartnerData extracts the data field from a partner envelope.
func RequirePartnerData(env PartnerEnvelope, endpoint string) (map[string]any, error) {
	if env.Code != 0 {
		code := fmt.Sprintf("%d", env.Code)
		msg := env.Message
		if msg == "" {
			msg = fmt.Sprintf("Shopee partner API returned error %d", env.Code)
		}
		if InvalidTokenCodes[code] {
			return nil, core.NewAuthError(core.CodeAuthRequired,
				"Shopee rejected the merchant token; the session is no longer valid", nil)
		}
		return nil, core.NewAPIError(msg, code, nil)
	}
	if env.Data == nil {
		return nil, core.NewAPIError("Shopee partner API returned empty data", "", nil)
	}
	return env.Data, nil
}

// RequirePaymentData extracts the data field from a payment envelope.
func RequirePaymentData(env PaymentEnvelope, endpoint string) (map[string]any, error) {
	if env.Error != 0 || env.Code != 0 {
		code := fmt.Sprintf("%d", env.Error)
		if env.Code != 0 {
			code = fmt.Sprintf("%d", env.Code)
		}
		msg := env.Msg
		if msg == "" {
			msg = fmt.Sprintf("ShopeePay API returned error %s", code)
		}
		if InvalidTokenCodes[code] {
			return nil, core.NewAuthError(core.CodeAuthRequired,
				"Shopee rejected the merchant token; the session is no longer valid", nil)
		}
		return nil, core.NewAPIError(msg, code, nil)
	}
	if env.Data == nil {
		return nil, core.NewAPIError("ShopeePay API returned empty data", "", nil)
	}
	return env.Data, nil
}

// ---- Phone Formatting ----

// FormatPhoneForVerification renders an e164 phone number the way the passport
// client formats it for verify_otp, e.g. "(+62) 897 7110 640".
func FormatPhoneForVerification(e164 string) string {
	subscriber := strings.TrimPrefix(e164, "62")
	groups := []string{}
	if len(subscriber) >= 3 {
		groups = append(groups, subscriber[:3])
	}
	if len(subscriber) > 3 {
		group2 := subscriber[3:]
		if len(group2) > 4 {
			groups = append(groups, group2[:4])
			groups = append(groups, group2[4:])
		} else {
			groups = append(groups, group2)
		}
	}
	return "(+62) " + strings.Join(groups, " ")
}

// ---- Partner State ----

// PartnerState builds the partner login state URL parameter, mirroring the
// passport client's `URL.searchParams` behavior: inner URLs are URL-encoded
// exactly once.
func PartnerState() string {
	loginAuthURL := ShopeeURL(PartnerBaseURL, EPPartnerLoginAuth)
	u, _ := url.Parse(PartnerBaseURL + "/")
	q := u.Query()
	q.Set("business_next", loginAuthURL)
	q.Set("business_state", PartnerBaseURL)
	q.Set("business_client_id", BusinessClientID)
	u.RawQuery = q.Encode()
	return u.String()
}

// ---- Account Headers ----

// LoginReferer builds the Referer header value the passport SPA sends on every
// account authentication call. It mirrors shopeeLoginReferer() exactly.
func LoginReferer(businessNext string) string {
	u, _ := url.Parse(ShopeeURL(AccountBaseURL, "/authenticate/login/"))
	q := u.Query()
	q.Set("lang", DefaultLanguage)
	q.Set("should_hide_back", "true")

	stateURL, _ := url.Parse(PartnerBaseURL + "/")
	sq := stateURL.Query()
	sq.Set("business_next", businessNext)
	sq.Set("business_state", PartnerBaseURL)
	sq.Set("business_client_id", BusinessClientID)
	stateURL.RawQuery = sq.Encode()

	q.Set("state", stateURL.String())
	q.Set("client_id", AccountClientID)
	q.Set("next", ShopeeURL(PartnerBaseURL, EPAccountLogin))
	u.RawQuery = q.Encode()
	return u.String()
}

// AccountHeaders builds the headers for Shopee account API requests.
func AccountHeaders(jar *CookieJar, riskToken string) map[string]string {
	csrfToken := jar.Get("csrftoken", AccountBaseURL)
	referer := LoginReferer(ShopeeURL(PartnerBaseURL, EPPartnerLoginAuth))

	h := map[string]string{
		"Origin":         AccountBaseURL,
		"Referer":        referer,
		"X-App-Type":     "2",
		"Sec-Fetch-Dest": "empty",
		"Sec-Fetch-Mode": "cors",
		"Sec-Fetch-Site": "same-origin",
		"Priority":       "u=0",
	}

	if riskToken != "" {
		h["af-ac-enc-sz-token"] = riskToken
		h["x-sz-sdk-version"] = SZSDKVersion
	}
	if csrfToken != "" {
		h["X-CSRFToken"] = csrfToken
	}
	return h
}

// PartnerHeaderOpts is the optional set of partner API headers.
type PartnerHeaderOpts struct {
	Token    string
	TocNonce string
	Locale   APILocale
}

// PartnerHeaders builds the headers for Shopee partner API requests. The
// names mirror the official web client's X-Merchant-* scheme exactly; the
// mer-detect service in particular rejects requests that arrive without the
// request id and tracing baggage.
func PartnerHeaders(opts PartnerHeaderOpts) map[string]string {
	lang := opts.Locale.Language
	if lang == "" {
		lang = DefaultLanguage
	}
	tz := opts.Locale.Timezone
	if tz == "" {
		tz = DefaultTimezone
	}
	h := map[string]string{
		"Origin":                  PartnerBaseURL,
		"Referer":                 PartnerBaseURL + "/",
		"X-Merchant-ToB-Clientid": "undefined",
		"X-Merchant-Login-From":   PartnerLoginFrom,
		"X-Merchant-From":         PartnerLoginFrom,
		"X-Merchant-Language":     lang,
		"X-Merchant-Timezone":     tz,
		"X-Merchant-RequestId":    utils.UUID(),
		"shopee-baggage":          "PFB=undefined",
		"X-Merchant-Token":        opts.Token,
	}
	if opts.TocNonce != "" {
		h["X-Merchant-ToC-Nonce"] = opts.TocNonce
	}
	return h
}

// PaymentHeaders builds the headers for ShopeePay API requests.
func PaymentHeaders() map[string]string {
	return map[string]string{
		"Origin":         PartnerBaseURL,
		"Referer":        PartnerBaseURL + "/",
		"X-Timestamp-Ms": fmt.Sprintf("%d", time.Now().UnixMilli()),
		"X-Token":        "",
	}
}

// PaymentMetadata builds the payment request metadata body.
func PaymentMetadata(token string, locale APILocale) map[string]any {
	lang := locale.Language
	if lang == "" {
		lang = DefaultLanguage
	}
	tz := locale.Timezone
	if tz == "" {
		tz = DefaultTimezone
	}
	return map[string]any{
		"token":    token,
		"language": lang,
		"timezone": tz,
	}
}
