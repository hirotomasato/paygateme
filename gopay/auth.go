package gopay

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/hirotomasato/paygateme/core"
)

// AuthClient wraps the GoID authentication endpoints used by the merchant
// dashboard: OTP request, OTP verification (token exchange), and token
// refresh. It implements TokenRefresher.
type AuthClient struct {
	http     *HTTPClient
	clientID string
}

// NewAuthClient creates an auth client.
func NewAuthClient(http *HTTPClient, clientID string) *AuthClient {
	if clientID == "" {
		clientID = DefaultGoIDClientID
	}
	return &AuthClient{http: http, clientID: clientID}
}

// RequestOTP sends an OTP challenge for a phone number.
func (a *AuthClient) RequestOTP(ctx context.Context, phoneNumber, countryCode string) (*LoginRequestResult, error) {
	body := map[string]any{
		"client_id":    a.clientID,
		"phone_number": normalizePhone(phoneNumber),
		"country_code": countryCode,
	}

	var raw GoIDEnvelope
	if err := a.http.RequestJSON(ctx, HTTPRequest{
		Method:        "POST",
		Path:          epLoginRequest,
		Headers:       map[string]string{"Authorization": "Bearer"},
		SkipAuthRetry: true,
		Body:          body,
	}, &raw); err != nil {
		return nil, err
	}

	if err := assertSuccess(raw, "failed to request OTP"); err != nil {
		return nil, err
	}

	var data map[string]any
	_ = json.Unmarshal(raw.Data, &data)
	otpToken := ""
	if data != nil {
		if v, ok := data["otp_token"].(string); ok {
			otpToken = v
		} else if v, ok := data["token"].(string); ok {
			otpToken = v
		}
	}

	rawJSON, _ := json.Marshal(raw)
	return &LoginRequestResult{OTPToken: otpToken, Raw: rawJSON}, nil
}

// VerifyOTP exchanges an OTP for an access/refresh token pair.
//
// The GoID token endpoint expects the OTP challenge fields nested under a
// `data` object and identified by the `otp_token` returned from RequestOTP.
func (a *AuthClient) VerifyOTP(ctx context.Context, otp, otpToken string) (*TokenSet, error) {
	if otpToken == "" {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"otpToken is required; pass the value returned by RequestOTP", nil)
	}

	body := map[string]any{
		"client_id": a.clientID,
		"data": map[string]any{
			"otp":       otp,
			"otp_token": otpToken,
		},
		"grant_type": "otp",
	}

	var raw GoIDTokenPayload
	if err := a.http.RequestJSON(ctx, HTTPRequest{
		Method:        "POST",
		Path:          epToken,
		Headers:       map[string]string{"Authorization": "Bearer"},
		SkipAuthRetry: true,
		Body:          body,
	}, &raw); err != nil {
		return nil, err
	}

	return toTokenSet(&raw)
}

// Refresh obtains a fresh access token from a refresh token.
//
// The refresh token goes nested under `data`, exactly like the OTP fields.
// A flat `refresh_token` at the top level is rejected with 401. The Bearer
// header is deliberately empty — auth endpoints must never re-enter the
// 401 auto-retry loop.
func (a *AuthClient) Refresh(ctx context.Context, refreshToken string) (*TokenSet, error) {
	body := map[string]any{
		"client_id": a.clientID,
		"data": map[string]any{
			"refresh_token": refreshToken,
		},
		"grant_type": "refresh_token",
	}

	var raw GoIDTokenPayload
	if err := a.http.RequestJSON(ctx, HTTPRequest{
		Method:        "POST",
		Path:          epToken,
		Headers:       map[string]string{"Authorization": "Bearer"},
		SkipAuthRetry: true,
		Body:          body,
	}, &raw); err != nil {
		return nil, err
	}

	tokens, err := toTokenSet(&raw)
	if err != nil {
		return nil, err
	}
	// The refresh endpoint may omit refresh_token; preserve the prior one.
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = refreshToken
	}
	return tokens, nil
}

// toTokenSet converts a raw token payload to a TokenSet.
func toTokenSet(payload *GoIDTokenPayload) (*TokenSet, error) {
	if payload == nil || payload.AccessToken == "" {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"token response missing access token", nil)
	}

	refreshToken := payload.RefreshToken

	var expiresAt int64
	if payload.ExpiresIn > 0 {
		expiresAt = time.Now().UnixMilli() + payload.ExpiresIn*1000
	}

	tokenType := payload.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}

	return &TokenSet{
		AccessToken:  payload.AccessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		ExpiresAt:    expiresAt,
	}, nil
}

// assertSuccess checks the GoID envelope for success.
func assertSuccess(raw GoIDEnvelope, context string) error {
	if raw.Success {
		return nil
	}
	detail := fmt.Sprintf("%v", raw)
	if len(raw.Errors) > 0 {
		e := raw.Errors[0]
		detail = e.Message
		if detail == "" {
			detail = e.MessageTitle
		}
		if detail == "" {
			detail = e.Code
		}
	}
	return core.NewAPIError(fmt.Sprintf("%s: %s", context, detail), "", nil)
}

var phoneCleanRE = regexp.MustCompile(`[^0-9]`)

// normalizePhone strips non-digit characters and leading zeros.
func normalizePhone(phone string) string {
	return regexp.MustCompile(`^0+`).ReplaceAllString(phoneCleanRE.ReplaceAllString(phone, ""), "")
}
