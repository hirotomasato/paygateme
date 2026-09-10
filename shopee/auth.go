package shopee

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/utils"
)

// AuthClient implements Shopee's fetch-only business-account OTP and
// merchant-token exchange flow.
type AuthClient struct {
	http   *HTTPClient
	locale APILocale
	logger utils.Logger
}

// NewAuthClient creates a Shopee auth client.
func NewAuthClient(http *HTTPClient, locale APILocale, logger utils.Logger) *AuthClient {
	if logger == nil {
		logger = utils.NoopLogger
	}
	return &AuthClient{http: http, locale: locale, logger: logger}
}

const passwordRequiredMessage = "This Shopee account is password-protected; supply the password to receive an OTP"

// RequestOtp sends an OTP for a phone number and returns the challenge.
func (a *AuthClient) RequestOtp(ctx context.Context, phoneNumber string, opts OtpRequestOptions) (*OtpChallenge, error) {
	parsed, err := utils.ParseIndonesianMobile(phoneNumber)
	if err != nil {
		return nil, err
	}
	phone := parsed.E164

	a.http.Jar().Clear()
	if err := a.bootstrapAccountSession(ctx); err != nil {
		return nil, err
	}

	fingerprint, err := a.acquireDeviceFingerprint(ctx, opts.DeviceReport)
	if err != nil {
		return nil, err
	}

	// Migration check.
	if _, err := a.accountRequest(ctx, EPCheckPasswordMigration,
		map[string]any{"phone": phone}, fingerprint); err != nil {
		return nil, err
	}
	a.logger.Info("shopee otp: migration check passed", nil)

	// Account existence + password step.
	if err := a.checkAccountExists(ctx, phone, opts.Password, fingerprint); err != nil {
		return nil, err
	}
	a.logger.Info("shopee otp: account existence checked", nil)

	hasPassword, err := a.authenticateByPassword(ctx, phone, opts.Password, fingerprint)
	if err != nil {
		return nil, err
	}
	if hasPassword {
		a.logger.Info("shopee otp: password accepted, OTP second factor required", nil)
	} else {
		a.logger.Info("shopee otp: password step skipped (passwordless account)", nil)
	}

	// OTP settings.
	settingsData, err := a.accountRequest(ctx, EPOtpSettings, map[string]any{
		"operation":                   OTPOperation,
		"phone":                       phone,
		"security_device_fingerprint": fingerprint,
		"support_session":             false,
		"supported_channels":          []int{1, 2, 3, 5},
	}, fingerprint)
	if err != nil {
		return nil, err
	}

	availableChannels := extractChannels(settingsData["available_channel_list"])
	channel := opts.Channel
	if channel == 0 {
		channel = intValue(settingsData["default_channel"], DefaultOTPChannel)
	}
	if len(availableChannels) > 0 && !containsInt(availableChannels, channel) {
		return nil, core.NewConfigError("Requested Shopee OTP channel is unavailable",
			map[string]any{"channel": channel, "availableChannels": availableChannels})
	}

	// Send OTP.
	sendData, err := a.accountRequest(ctx, EPSendOtp, map[string]any{
		"operation":                   OTPOperation,
		"phone":                       phone,
		"security_device_fingerprint": fingerprint,
		"support_session":             false,
		"supported_channels":          []int{1, 2, 3, 5, 4},
		"channel":                     channel,
		"captcha_signature":           "",
	}, fingerprint)
	if err != nil {
		return nil, err
	}

	seed, _ := sendData["seed"].(string)
	if seed != "" {
		a.logger.Info("shopee otp: send accepted with delivery seed", nil)
	} else {
		a.logger.Info("shopee otp: send accepted but no delivery seed returned", nil)
	}

	return &OtpChallenge{
		Version:           1,
		PhoneNumber:       phone,
		Channel:           channel,
		AvailableChannels: availableChannels,
		DeviceFingerprint: fingerprint,
		RiskToken:         fingerprint,
		HasPassword:       hasPassword,
		Cookies:           a.http.Jar().Snapshot(),
		RequestedAt:       time.Now().UnixMilli(),
	}, nil
}

// VerifyOtp verifies an OTP and returns merchant-list state.
func (a *AuthClient) VerifyOtp(ctx context.Context, input VerifyOtpInput) (*OtpVerification, error) {
	challenge := input.Challenge
	if challenge.Version != 1 {
		return nil, core.NewConfigError("Unsupported Shopee OTP challenge version", nil)
	}
	otp := trimSpace(input.OTP)
	if !regexp.MustCompile(`^\d{4,10}$`).MatchString(otp) {
		return nil, core.NewConfigError("Shopee OTP must contain 4 to 10 digits", nil)
	}
	a.http.Jar().Restore(challenge.Cookies)

	riskToken := challenge.RiskToken
	if riskToken == "" {
		riskToken = challenge.DeviceFingerprint
	}

	verified, err := a.accountRequest(ctx, EPVerifyOtp, map[string]any{
		"operation":                   OTPOperation,
		"otp":                         otp,
		"phone":                       FormatPhoneForVerification(challenge.PhoneNumber),
		"security_device_fingerprint": challenge.DeviceFingerprint,
		"support_session":             false,
	}, riskToken)
	if err != nil {
		return nil, err
	}
	otpToken, _ := verified["otp_token"].(string)
	if otpToken == "" {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"Shopee OTP verification returned no token", nil)
	}

	authenticated, err := a.accountRequest(ctx, EPAuthenticateByOtp, map[string]any{
		"otp_token":                   otpToken,
		"security_device_fingerprint": challenge.DeviceFingerprint,
		"is_signup":                   false,
	}, riskToken)
	if err != nil {
		return nil, err
	}

	tocNonce, _ := authenticated["toc_nonce"].(string)
	tocUserID := int64Value(authenticated["toc_account"], "userid")
	if tocNonce == "" || tocUserID == 0 {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"Shopee OTP authentication returned an incomplete account session", nil)
	}

	spcClientID := a.http.Jar().Get(ClientIDCookie, AccountBaseURL)
	if spcClientID == "" {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"Shopee authentication returned no client session id", nil)
	}

	// Account login redirect.
	accountLoginURL, _ := url.Parse(ShopeeURL(PartnerBaseURL, EPAccountLogin))
	q := accountLoginURL.Query()
	q.Set("lang", defaultString(a.locale.Language, DefaultLanguage))
	q.Set("spc_clientid", spcClientID)
	q.Set("state", PartnerState())
	q.Set("toc_nonce", tocNonce)
	accountLoginURL.RawQuery = q.Encode()
	if _, err := a.http.FollowGet(ctx, accountLoginURL.String()); err != nil {
		return nil, err
	}

	// Merchant detect.
	detectResponse := PartnerEnvelope{}
	if err := a.http.JSON(ctx, Request{
		URL:     ShopeeURL(PartnerAPIBaseURL, EPMerchantDetect),
		Method:  "POST",
		Headers: PartnerHeaders(PartnerHeaderOpts{TocNonce: tocNonce, Locale: a.locale}),
		Body:    map[string]any{},
	}, &detectResponse); err != nil {
		return nil, err
	}
	detected, err := RequirePartnerData(detectResponse, EPMerchantDetect)
	if err != nil {
		a.logger.Error("merchantDetect raw response", map[string]any{
			"code":    detectResponse.Code,
			"message": detectResponse.Message,
			"data":    detectResponse.Data,
		})
		return nil, err
	}

	merchants := normalizeMerchants(detected)
	if len(merchants) == 0 {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"The Shopee account has no accessible merchant", nil)
	}

	return &OtpVerification{
		Version:           1,
		TocNonce:          tocNonce,
		TocUserID:         tocUserID,
		SPCClientID:       spcClientID,
		DeviceFingerprint: challenge.DeviceFingerprint,
		Cookies:           a.http.Jar().Snapshot(),
		Merchants:         merchants,
		VerifiedAt:        time.Now().UnixMilli(),
	}, nil
}

// AccountSessionAlive reports whether Shopee still recognises the passport
// account session held in the cookie jar.
func (a *AuthClient) AccountSessionAlive(ctx context.Context) (bool, error) {
	response := AccountEnvelope{}
	if err := a.http.JSON(ctx, Request{
		URL:     ShopeeURL(AccountBaseURL, EPLoginStatus),
		Method:  "POST",
		Headers: AccountHeaders(a.http.Jar(), ""),
		Body:    map[string]any{},
	}, &response); err != nil {
		return false, err
	}
	return response.Error == 0, nil
}

// CompleteLogin exchanges OTP verification for a merchant session.
func (a *AuthClient) CompleteLogin(ctx context.Context, input CompleteLoginInput) (*Session, error) {
	verification := input.Verification
	if verification.Version != 1 {
		return nil, core.NewConfigError("Unsupported Shopee OTP verification version", nil)
	}
	a.http.Jar().Restore(verification.Cookies)

	merchant, err := selectMerchant(verification.Merchants, input.MerchantID)
	if err != nil {
		return nil, err
	}
	if !merchant.IsActive || merchant.IsBanned {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"The selected Shopee merchant is inactive or banned", nil)
	}

	// Token page redirect.
	tokenPageURL, _ := url.Parse(ShopeeURL(AccountBaseURL, EPAccountLoginToken))
	q := tokenPageURL.Query()
	q.Set("lang", defaultString(a.locale.Language, DefaultLanguage))
	q.Set("spc_clientid", verification.SPCClientID)
	q.Set("state", PartnerState())
	q.Set("tob_userid", fmt.Sprintf("%d", merchant.StaffUserID))
	q.Set("next", ShopeeURL(PartnerBaseURL, EPAccountTobAuth))
	q.Set("client_id", AccountClientID)
	q.Set("toc_nonce", verification.TocNonce)
	tokenPageURL.RawQuery = q.Encode()
	if _, err := a.http.FollowGet(ctx, tokenPageURL.String()); err != nil {
		return nil, err
	}

	// Login TOC.
	loginData, err := a.accountRequest(ctx, EPLoginToc, map[string]any{
		"toc_nonce":                   verification.TocNonce,
		"tob_userid":                  merchant.StaffUserID,
		"security_device_fingerprint": verification.DeviceFingerprint,
	}, verification.DeviceFingerprint)
	if err != nil {
		return nil, err
	}
	nonce, _ := loginData["nonce"].(string)
	if nonce == "" {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"Shopee merchant login returned no authorization code", nil)
	}

	// Exchange redirect.
	exchangeURL, _ := url.Parse(ShopeeURL(PartnerBaseURL, EPAccountTobAuth))
	eq := exchangeURL.Query()
	eq.Set("code", nonce)
	eq.Set("lang", defaultString(a.locale.Language, DefaultLanguage))
	eq.Set("spc_clientid", verification.SPCClientID)
	eq.Set("state", PartnerState())
	exchangeURL.RawQuery = eq.Encode()
	if _, err := a.http.FollowGet(ctx, exchangeURL.String()); err != nil {
		return nil, err
	}

	credential, err := ReadMerchantCredential(a.http.Jar())
	if err != nil {
		return nil, err
	}
	if credential.AccountID != "" && credential.AccountID != fmt.Sprintf("%d", merchant.StaffUserID) {
		return nil, core.NewAuthError(core.CodeAuthFailed,
			"Shopee returned a token for a different merchant", nil)
	}

	merchants := make([]MerchantSummary, len(verification.Merchants))
	copy(merchants, verification.Merchants)

	return &Session{
		Version:   1,
		Cookies:   a.http.Jar().Snapshot(),
		AccountID: credential.AccountID,
		Merchant:  merchant,
		Merchants: merchants,
		Stores:    []Store{},
		StoreID:   input.StoreID,
		CreatedAt: time.Now().UnixMilli(),
		ExpiresAt: credential.ExpiresAt,
	}, nil
}

// ---- Internal helpers ----

func (a *AuthClient) bootstrapAccountSession(ctx context.Context) error {
	lang := defaultString(a.locale.Language, DefaultLanguage)
	// The login page must be requested with a text/html Accept so the gateway
	// issues the anonymous SPC_* session cookies the browser collects first.
	_, err := a.http.FollowGetWithHeaders(ctx, AccountBaseURL+"/login", map[string]string{
		"lang": lang,
	}, map[string]string{"Accept": "text/html"})
	return err
}

func (a *AuthClient) acquireDeviceFingerprint(ctx context.Context, deviceReport string) (string, error) {
	headers := map[string]string{
		"Origin":  AccountBaseURL,
		"Referer": AccountBaseURL + "/",
	}

	var body any = map[string]any{}
	if deviceReport != "" {
		headers["Content-Type"] = "text/plain;charset=UTF-8"
		headers["szdet"] = fmt.Sprintf("%d", time.Now().UnixMilli())
		body = deviceReport
	} else {
		headers["Content-Type"] = "application/json"
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			RiskToken string `json:"riskToken"`
		} `json:"data"`
	}
	if err := a.http.JSON(ctx, Request{
		URL:     DeviceFingerprintURL,
		Method:  "POST",
		Headers: headers,
		Body:    body,
	}, &resp); err != nil {
		return "", err
	}
	if resp.Code != 0 || resp.Data.RiskToken == "" {
		return "", core.NewHTTPError(resp.Code,
			"Shopee device-risk service returned no risk token", nil)
	}
	return resp.Data.RiskToken, nil
}

func (a *AuthClient) accountRequest(ctx context.Context, path string, body map[string]any, riskToken string) (map[string]any, error) {
	response := AccountEnvelope{}
	if err := a.http.JSON(ctx, Request{
		URL:     ShopeeURL(AccountBaseURL, path),
		Method:  "POST",
		Headers: AccountHeaders(a.http.Jar(), riskToken),
		Body:    body,
	}, &response); err != nil {
		return nil, err
	}
	return RequireAccountData(response, path)
}

func (a *AuthClient) checkAccountExists(ctx context.Context, phone, password, riskToken string) error {
	// This endpoint may return non-zero codes (e.g. 48401004) but the flow
	// proceeds anyway; only transport errors abort.
	hashed := ""
	if password != "" {
		hashed = HashShopeePassword(password)
	}
	_ = a.http.JSON(ctx, Request{
		URL:     ShopeeURL(AccountBaseURL, EPCheckAccountByPassword),
		Method:  "POST",
		Headers: AccountHeaders(a.http.Jar(), riskToken),
		Body: map[string]any{
			"phone":    phone,
			"password": hashed,
		},
	}, &AccountEnvelope{})
	return nil
}

func (a *AuthClient) authenticateByPassword(ctx context.Context, phone, password, riskToken string) (bool, error) {
	hashed := ""
	if password != "" {
		hashed = HashShopeePassword(password)
	}
	response := AccountEnvelope{}
	if err := a.http.JSON(ctx, Request{
		URL:     ShopeeURL(AccountBaseURL, EPAuthenticateByPassword),
		Method:  "POST",
		Headers: AccountHeaders(a.http.Jar(), riskToken),
		Body: map[string]any{
			"phone":                       phone,
			"password":                    hashed,
			"security_device_fingerprint": riskToken,
		},
	}, &response); err != nil {
		return false, err
	}

	if response.Error == AuthErrorNeedOTP {
		if password == "" {
			return false, core.NewConfigError(passwordRequiredMessage, nil)
		}
		return true, nil
	}
	if response.Error == 0 {
		return false, nil
	}
	if hasPasswordInData(response.Data) {
		return false, core.NewConfigError(passwordRequiredMessage, nil)
	}
	return false, core.NewAuthError(core.CodeAuthFailed,
		fmt.Sprintf("Shopee rejected the password before sending the OTP (error %d)", response.Error), nil)
}

func hasPasswordInData(data map[string]any) bool {
	if data == nil {
		return false
	}
	tocAccount, ok := data["toc_account"].(map[string]any)
	if !ok {
		return false
	}
	hasPassword, ok := tocAccount["has_password"].(bool)
	return ok && hasPassword
}

// ---- Merchant helpers ----

func normalizeMerchants(detected map[string]any) []MerchantSummary {
	selectMerchant, _ := detected["selectMerchant"].(map[string]any)
	if selectMerchant == nil {
		return nil
	}
	merchantList, _ := selectMerchant["merchantList"].([]any)
	if merchantList == nil {
		return nil
	}

	out := make([]MerchantSummary, 0, len(merchantList))
	for _, raw := range merchantList {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		merchantID := int64Value(m, "merchantId")
		staffUserID := int64Value(m, "staffTobUid")
		if merchantID == 0 || staffUserID == 0 {
			continue
		}
		out = append(out, MerchantSummary{
			ID:                 fmt.Sprintf("%d", merchantID),
			Name:               stringValue(m["merchantName"]),
			Status:             int(intValue(m["merchantStatus"], 0)),
			StaffUserID:        staffUserID,
			StaffRole:          int(intValue(m["staffRole"], 0)),
			StaffStatus:        int(intValue(m["staffStatus"], 0)),
			IsActive:           boolValue(m["isActive"]),
			IsBanned:           boolValue(m["isBanned"]),
			IsCurrentLoginUser: boolValue(m["isCurrentLoginUser"]),
		})
	}
	return out
}

func selectMerchant(merchants []MerchantSummary, requestedID string) (MerchantSummary, error) {
	if requestedID != "" {
		for _, m := range merchants {
			if m.ID == requestedID {
				return m, nil
			}
		}
		available := make([]map[string]string, 0, len(merchants))
		for _, m := range merchants {
			available = append(available, map[string]string{"id": m.ID, "name": m.Name})
		}
		return MerchantSummary{}, core.NewConfigError(
			"Configured Shopee merchantId is not accessible",
			map[string]any{"merchantId": requestedID, "availableMerchants": available})
	}

	if resolved, ok := resolveSingleMerchant(merchants); ok {
		return resolved, nil
	}
	usable := UsableMerchants(merchants)
	available := make([]map[string]string, 0, len(usable))
	for _, m := range usable {
		available = append(available, map[string]string{"id": m.ID, "name": m.Name})
	}
	return MerchantSummary{}, core.NewConfigError(
		"merchantId is required when multiple Shopee merchants are accessible",
		map[string]any{"availableMerchants": available})
}

// UsableMerchants returns merchants a login can actually select.
func UsableMerchants(merchants []MerchantSummary) []MerchantSummary {
	out := make([]MerchantSummary, 0, len(merchants))
	for _, m := range merchants {
		if m.IsActive && !m.IsBanned {
			out = append(out, m)
		}
	}
	return out
}

func resolveSingleMerchant(merchants []MerchantSummary) (MerchantSummary, bool) {
	usable := UsableMerchants(merchants)
	current := make([]MerchantSummary, 0, len(usable))
	for _, m := range usable {
		if m.IsCurrentLoginUser {
			current = append(current, m)
		}
	}
	if len(current) == 1 {
		return current[0], true
	}
	if len(usable) == 1 {
		return usable[0], true
	}
	return MerchantSummary{}, false
}

// ---- JSON value helpers ----

func extractChannels(v any) []int {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]int, 0, len(list))
	for _, item := range list {
		if f, ok := item.(float64); ok {
			out = append(out, int(f))
		}
	}
	return out
}

func intValue(v any, fallback int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	default:
		return fallback
	}
}

func int64Value(v any, key string) int64 {
	m, ok := v.(map[string]any)
	if !ok {
		return 0
	}
	switch t := m[key].(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	default:
		return 0
	}
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolValue(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func containsInt(list []int, target int) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
