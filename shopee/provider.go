package shopee

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/payment"
	"github.com/hirotomasato/paygateme/qris"
	"github.com/hirotomasato/paygateme/utils"
)

// ProviderConfig configures a ShopeeProvider.
type ProviderConfig struct {
	// Restore a previous session instead of logging in again.
	Session *Session
	// Static QRIS payload (whole EMVCo string) bound to the active store.
	StaticQris string
	// Owner metadata for the static QRIS.
	StaticQrisScope *StaticQrisScope
	// Callback to persist the updated session (required for token rotation).
	OnSessionUpdated func(session Session) error
	// Custom payment store. Defaults to in-memory.
	Store core.PaymentStore
	// Device-risk blob captured from a real browser.
	DeviceReport string
	// Polling and payment tuning (milliseconds; 0 = defaults).
	PollInterval  int64
	DefaultExpiry int64
	ClockSkew     int64
	Logger        utils.Logger
}

// Provider is the high-level Shopee Merchant adapter. It owns auth, merchant
// and store discovery, the per-scope payment service, and QRIS binding.
type Provider struct {
	config ProviderConfig
	logger utils.Logger

	http     *HTTPClient
	auth     *AuthClient
	merchant *MerchantClient

	mu      sync.Mutex
	session *Session

	store           core.PaymentStore
	staticQris      string
	staticQrisScope *StaticQrisScope

	services      map[string]*payment.Service
	activeService *payment.Service
	activeScope   *core.PaymentScope
}

// NewProvider constructs a ShopeeProvider.
func NewProvider(config ProviderConfig) *Provider {
	logger := config.Logger
	if logger == nil {
		logger = utils.NoopLogger
	}
	store := config.Store
	if store == nil {
		store = payment.NewInMemoryStore()
	}

	http := NewHTTPClient(logger)
	p := &Provider{
		config:          config,
		logger:          logger,
		http:            http,
		auth:            NewAuthClient(http, APILocale{}, logger),
		store:           store,
		staticQris:      config.StaticQris,
		staticQrisScope: config.StaticQrisScope,
		services:        make(map[string]*payment.Service),
	}

	if config.Session != nil {
		sess := *config.Session
		p.session = &sess
		// Restore the cookie jar so merchant-token reads and partner calls
		// work immediately on a restored session (no fresh login needed).
		p.http.Jar().Restore(sess.Cookies)
	}

	return p
}

// --- Auth ---

// RequestOtp sends an OTP for a phone number.
func (p *Provider) RequestOtp(ctx context.Context, phone string, opts OtpRequestOptions) (*OtpChallenge, error) {
	if opts.DeviceReport == "" {
		opts.DeviceReport = p.config.DeviceReport
	}
	return p.auth.RequestOtp(ctx, phone, opts)
}

// VerifyOtp verifies an OTP and returns merchant-list state.
func (p *Provider) VerifyOtp(ctx context.Context, input VerifyOtpInput) (*OtpVerification, error) {
	return p.auth.VerifyOtp(ctx, input)
}

// LoginWithOtp combines verify and complete login in one call.
func (p *Provider) LoginWithOtp(ctx context.Context, input LoginWithOtpInput) (*LoginOutcome, error) {
	verification, err := p.auth.VerifyOtp(ctx, VerifyOtpInput{
		Challenge: input.Challenge,
		OTP:       input.OTP,
	})
	if err != nil {
		return nil, err
	}

	merchantID := input.MerchantID
	if merchantID == "" {
		if resolved, ok := resolveSingleMerchant(verification.Merchants); ok {
			merchantID = resolved.ID
		} else {
			return &LoginOutcome{
				Status:       LoginMerchantSelectionNeeded,
				Verification: verification,
				Merchants:    verification.Merchants,
			}, nil
		}
	}

	session, err := p.completeLoginInternal(ctx, *verification, merchantID, input.StoreID)
	if err != nil {
		return nil, err
	}
	return &LoginOutcome{Status: LoginComplete, Session: session}, nil
}

// CompleteLogin completes a login with a chosen merchant.
func (p *Provider) CompleteLogin(ctx context.Context, input CompleteLoginInput) (*Session, error) {
	return p.completeLoginInternal(ctx, input.Verification, input.MerchantID, input.StoreID)
}

// completeLoginInternal finishes a login: exchanges the verification for a
// merchant token, discovers the profile and stores, and builds the final
// session with the renewal (switch) credential retained.
func (p *Provider) completeLoginInternal(ctx context.Context, verification OtpVerification, merchantID, requestedStoreID string) (*Session, error) {
	base, err := p.auth.CompleteLogin(ctx, CompleteLoginInput{
		Verification: verification,
		MerchantID:   merchantID,
		StoreID:      requestedStoreID,
	})
	if err != nil {
		return nil, err
	}

	token, err := ReadMerchantCredential(p.http.Jar())
	if err != nil {
		return nil, err
	}
	merchantClient := NewMerchantClient(p.http, token.Token, base.Merchant.ID, APILocale{}, p.logger)

	profile, err := merchantClient.GetProfile(ctx)
	if err != nil {
		return nil, err
	}
	stores, err := merchantClient.ListStores(ctx, 100)
	if err != nil {
		return nil, err
	}
	storeID, err := chooseStoreID(stores, requestedStoreID, profile.StoreID)
	if err != nil {
		return nil, err
	}

	session := &Session{
		Version:   1,
		Cookies:   p.http.Jar().Snapshot(),
		AccountID: base.AccountID,
		Merchant:  base.Merchant,
		Merchants: append([]MerchantSummary(nil), verification.Merchants...),
		Stores:    stores,
		StoreID:   storeID,
		CreatedAt: time.Now().UnixMilli(),
		ExpiresAt: base.ExpiresAt,
		SwitchCredential: &SwitchCredential{
			TocNonce:          verification.TocNonce,
			SPCClientID:       verification.SPCClientID,
			DeviceFingerprint: verification.DeviceFingerprint,
		},
	}
	if profile.MerchantName != "" {
		session.Merchant.Name = profile.MerchantName
	}

	return p.persistSession(*session)
}

// --- Merchant / Store ---

// ListStores returns every store the active merchant owns.
func (p *Provider) ListStores(ctx context.Context) ([]Store, error) {
	p.mu.Lock()
	merchant := p.merchant
	p.mu.Unlock()
	if merchant == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired,
			"login before listing stores", nil)
	}
	return merchant.ListStores(ctx, 100)
}

// SelectStore selects an active store and rebuilds the per-scope service.
func (p *Provider) SelectStore(ctx context.Context, storeID string) (*Session, error) {
	p.mu.Lock()
	session := p.session
	merchant := p.merchant
	p.mu.Unlock()

	if session == nil || merchant == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired,
			"login before selecting a store", nil)
	}

	stores, err := merchant.ListStores(ctx, 100)
	if err != nil {
		return nil, err
	}
	if _, err := chooseStoreID(stores, storeID, ""); err != nil {
		return nil, err
	}

	// Deactivate the current composition before switching scope.
	p.mu.Lock()
	if p.activeService != nil {
		if _, err := p.activeService.Deactivate(nil); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	}
	updated := *session
	updated.StoreID = storeID
	updated.Stores = stores
	p.mu.Unlock()

	return p.persistSession(updated)
}

// SelectMerchant re-mints the merchant token for a different merchant through
// the SSO exchange, without a new OTP.
func (p *Provider) SelectMerchant(ctx context.Context, merchantID string) (*Session, error) {
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()

	if session == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired,
			"login before switching merchant", nil)
	}

	var target *MerchantSummary
	for i := range session.Merchants {
		if session.Merchants[i].ID == merchantID {
			target = &session.Merchants[i]
			break
		}
	}
	if target == nil {
		return nil, core.NewConfigError("Shopee merchant is not accessible",
			map[string]any{"merchantId": merchantID})
	}
	if session.Merchant.ID == merchantID {
		return p.ExportSession(), nil
	}
	if !target.IsActive || target.IsBanned {
		return nil, core.NewConfigError("The selected Shopee merchant is inactive or banned",
			map[string]any{"merchantId": merchantID})
	}
	if session.SwitchCredential == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired,
			"This Shopee session cannot switch merchants; log in again to enable switching", nil)
	}

	p.mu.Lock()
	if p.activeService != nil {
		if _, err := p.activeService.Deactivate(nil); err != nil {
			p.mu.Unlock()
			return nil, err
		}
	}
	p.mu.Unlock()

	base, err := p.mintMerchantToken(ctx, session, merchantID)
	if err != nil {
		return nil, err
	}
	minted, err := ReadMerchantCredential(p.http.Jar())
	if err != nil {
		return nil, err
	}
	merchantClient := NewMerchantClient(p.http, minted.Token, base.Merchant.ID, APILocale{}, p.logger)
	profile, err := merchantClient.GetProfile(ctx)
	if err != nil {
		return nil, err
	}
	stores, err := merchantClient.ListStores(ctx, 100)
	if err != nil {
		return nil, err
	}
	storeID, err := chooseStoreID(stores, "", profile.StoreID)
	if err != nil {
		return nil, err
	}

	updated := *session
	updated.Cookies = p.http.Jar().Snapshot()
	updated.AccountID = base.AccountID
	updated.Merchant = *target
	if profile.MerchantName != "" {
		updated.Merchant.Name = profile.MerchantName
	}
	updated.Stores = stores
	updated.StoreID = storeID
	if minted.ExpiresAt > 0 {
		updated.ExpiresAt = minted.ExpiresAt
	}

	return p.persistSession(updated)
}

// RefreshSession renews the merchant token without a new OTP, for as long as
// the passport account session is still alive.
func (p *Provider) RefreshSession(ctx context.Context) (*Session, error) {
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()

	if session == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired, "no session to refresh", nil)
	}
	if session.SwitchCredential == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired,
			"This Shopee session predates silent renewal; log in again with an OTP", nil)
	}

	p.http.Jar().Restore(session.Cookies)
	alive, err := p.auth.AccountSessionAlive(ctx)
	if err != nil {
		return nil, err
	}
	if !alive {
		return nil, core.NewAuthError(core.CodeAuthRequired,
			"The Shopee account session has expired; log in again with an OTP", nil)
	}

	if _, err := p.mintMerchantToken(ctx, session, session.Merchant.ID); err != nil {
		return nil, err
	}
	minted, err := ReadMerchantCredential(p.http.Jar())
	if err != nil {
		return nil, err
	}

	updated := *session
	updated.Cookies = p.http.Jar().Snapshot()
	if minted.ExpiresAt > 0 {
		updated.ExpiresAt = minted.ExpiresAt
	}

	return p.persistSession(updated)
}

// mintMerchantToken re-runs the login SSO exchange for one of the account's
// merchants, leaving the freshly minted token in the dashboard cookie.
func (p *Provider) mintMerchantToken(ctx context.Context, session *Session, merchantID string) (*Session, error) {
	credential := session.SwitchCredential
	if credential == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired,
			"This Shopee session has no renewal credential; log in again", nil)
	}

	verification := OtpVerification{
		Version:           1,
		TocNonce:          credential.TocNonce,
		TocUserID:         0, // not read by completeLogin
		SPCClientID:       credential.SPCClientID,
		DeviceFingerprint: credential.DeviceFingerprint,
		Cookies:           append([]Cookie(nil), session.Cookies...),
		Merchants:         append([]MerchantSummary(nil), session.Merchants...),
		VerifiedAt:        session.CreatedAt,
	}

	return p.auth.CompleteLogin(ctx, CompleteLoginInput{
		Verification: verification,
		MerchantID:   merchantID,
	})
}

// --- QRIS ---

// SetStaticQris binds a static QRIS payload to the active store.
func (p *Provider) SetStaticQris(payload string) error {
	if !qris.IsValidQRISChecksum(payload) {
		return core.NewBaseError(core.CodeQRISParseError, "QRIS checksum is invalid")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session == nil || p.session.StoreID == "" {
		return core.NewConfigError("select a store before binding QRIS", nil)
	}
	p.staticQris = payload
	p.staticQrisScope = &StaticQrisScope{
		MerchantID: p.session.Merchant.ID,
		StoreID:    p.session.StoreID,
	}
	return nil
}

// StaticQris returns the bound QRIS payload.
func (p *Provider) StaticQris() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.staticQris
}

// StaticQrisScope returns the bound QRIS owner metadata.
func (p *Provider) StaticQrisScope() *StaticQrisScope {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.staticQrisScope == nil {
		return nil
	}
	c := *p.staticQrisScope
	return &c
}

// --- Payments ---

// GetPaymentScope returns the active payment scope.
func (p *Provider) GetPaymentScope() *core.PaymentScope {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paymentScopeLocked()
}

// Payments returns the active store's payment service.
func (p *Provider) Payments() (*payment.Service, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	scope := p.paymentScopeLocked()
	if scope == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired,
			"login and select a store before using payments", nil)
	}

	key := scopeKey(scope)
	if svc, ok := p.services[key]; ok {
		p.activeService = svc
		p.activeScope = scope
		_ = svc.Activate(nil)
		return svc, nil
	}

	svc, err := p.buildServiceLocked(scope)
	if err != nil {
		return nil, err
	}
	p.services[key] = svc
	p.activeService = svc
	p.activeScope = scope
	return svc, nil
}

// CreatePayment creates a payment on the active store's scope.
func (p *Provider) CreatePayment(ctx context.Context, amount int64, reference string) (*core.Payment, error) {
	svc, err := p.Payments()
	if err != nil {
		return nil, err
	}
	created, err := svc.CreatePayment(ctx, payment.CreatePaymentInput{
		Amount:    amount,
		Reference: reference,
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// CancelPayment cancels a pending payment.
func (p *Provider) CancelPayment(ctx context.Context, id string) (*core.Payment, error) {
	svc, err := p.Payments()
	if err != nil {
		return nil, err
	}
	return svc.CancelPayment(ctx, id)
}

// Authenticated reports whether the provider holds a live session. It reads
// the merchant token from the cookie jar and checks its expiry, so an expired
// session is reported as unauthenticated — the caller can trigger a re-login.
func (p *Provider) Authenticated() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session == nil {
		return false
	}
	cred, err := ReadMerchantCredential(p.http.Jar())
	if err != nil {
		return false
	}
	if cred.ExpiresAt > 0 && time.Now().UnixMilli() >= cred.ExpiresAt {
		return false
	}
	if p.session.AccountID != "" && cred.AccountID != "" && cred.AccountID != p.session.AccountID {
		return false
	}
	return true
}

// ExportSession returns the current session.
func (p *Provider) ExportSession() *Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session == nil {
		return nil
	}
	s := *p.session
	return &s
}

// --- Internal ---

func (p *Provider) paymentScopeLocked() *core.PaymentScope {
	if p.session == nil || p.session.StoreID == "" {
		return nil
	}
	return &core.PaymentScope{
		Provider:   ProviderID,
		AccountID:  p.session.Merchant.ID,
		MerchantID: p.session.StoreID,
	}
}

func (p *Provider) buildServiceLocked(scope *core.PaymentScope) (*payment.Service, error) {
	token, err := ReadMerchantCredential(p.http.Jar())
	if err != nil {
		return nil, err
	}

	feed := NewTransactionFeed(p.http, token.Token, p.session.Merchant.ID, p.session.StoreID, APILocale{}, p.logger)

	svc, err := payment.NewService(payment.Options{
		MerchantID:    scope.MerchantID,
		Scope:         scope,
		Store:         p.store,
		Feed:          feed,
		StaticQris:    p.staticQris,
		PollInterval:  durationMs(p.config.PollInterval, 0),
		DefaultExpiry: durationMs(p.config.DefaultExpiry, 0),
		ClockSkew:     durationMs(p.config.ClockSkew, 0),
		Logger:        p.logger,
	})
	if err != nil {
		return nil, err
	}
	return svc, nil
}

func (p *Provider) persistSession(session Session) (*Session, error) {
	// Rebuild the cookie jar and merchant client from the persisted session so
	// subsequent calls operate on the renewed state.
	p.http.Jar().Restore(session.Cookies)
	if token, err := ReadMerchantCredential(p.http.Jar()); err == nil {
		p.merchant = NewMerchantClient(p.http, token.Token, session.Merchant.ID, APILocale{}, p.logger)
	}

	if p.config.OnSessionUpdated != nil {
		if err := p.config.OnSessionUpdated(session); err != nil {
			return nil, err
		}
	}
	p.mu.Lock()
	p.session = &session
	p.mu.Unlock()
	return &session, nil
}

func chooseStoreID(stores []Store, requestedID, profileStoreID string) (string, error) {
	if requestedID != "" {
		for _, s := range stores {
			if s.ID == requestedID {
				return s.ID, nil
			}
		}
		return "", core.NewConfigError("Configured Shopee store is not accessible",
			map[string]any{"storeId": requestedID})
	}
	if profileStoreID != "" {
		for _, s := range stores {
			if s.ID == profileStoreID {
				return s.ID, nil
			}
		}
	}
	if len(stores) == 1 {
		return stores[0].ID, nil
	}
	return "", nil
}

func scopeKey(scope *core.PaymentScope) string {
	return fmt.Sprintf("%s:%s:%s", scope.Provider, scope.AccountID, scope.MerchantID)
}

func durationMs(ms int64, fallback int64) time.Duration {
	if ms <= 0 {
		return time.Duration(fallback) * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}
