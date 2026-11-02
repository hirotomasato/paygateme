package gopay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/payment"
	"github.com/hirotomasato/paygateme/qris"
	"github.com/hirotomasato/paygateme/utils"
)

// Provider is the high-level GoPay/GoBiz Merchant adapter. It manages
// authentication, OAuth2 token rotation, merchant and outlet discovery,
// dynamic QRIS conversion, and per-scope payment reconciliation.
type Provider struct {
	config ProviderConfig
	logger utils.Logger

	http           *HTTPClient
	auth           *AuthClient
	merchantClient *MerchantClient
	feed           *TransactionFeed
	tokenManager   *TokenManager

	mu            sync.Mutex
	session       *SessionState
	deviceID      string
	staticQRIS    string
	store         core.PaymentStore
	services      map[string]*payment.Service
	activeService *payment.Service
	activeScope   *core.PaymentScope
}

// NewProvider constructs a GoPay Provider with the given configuration.
func NewProvider(config ProviderConfig) *Provider {
	logger := config.Logger
	if logger == nil {
		logger = utils.NoopLogger
	}
	store := config.Store
	if store == nil {
		store = payment.NewInMemoryStore()
	}

	deviceID := config.DeviceID
	if deviceID == "" && config.Session != nil {
		deviceID = config.Session.DeviceID
	}
	if deviceID == "" {
		deviceID = generateDeviceID()
	}

	httpClient := NewHTTPClient(GoBizAPIBaseURL, logger)
	httpClient.SetDefaultHeader("X-Uniqueid", deviceID)
	if config.AppVersion != "" {
		httpClient.SetDefaultHeader("x-appVersion", config.AppVersion)
	}

	auth := NewAuthClient(httpClient, config.ClientID)
	merchantClient := NewMerchantClient(httpClient)
	feed := NewTransactionFeed(httpClient)

	p := &Provider{
		config:         config,
		logger:         logger,
		http:           httpClient,
		auth:           auth,
		merchantClient: merchantClient,
		feed:           feed,
		store:          store,
		deviceID:       deviceID,
		staticQRIS:     config.StaticQRIS,
		services:       make(map[string]*payment.Service),
	}

	if config.Session != nil && config.Session.Tokens.AccessToken != "" {
		_ = p.initSession(*config.Session)
	}

	return p
}

// --- Authentication ---

// RequestOTP sends an OTP challenge for a phone number.
func (p *Provider) RequestOTP(ctx context.Context, phoneNumber string, countryCode ...string) (*LoginRequestResult, error) {
	cc := "62"
	if len(countryCode) > 0 && countryCode[0] != "" {
		cc = countryCode[0]
	}
	return p.auth.RequestOTP(ctx, phoneNumber, cc)
}

// VerifyOTP exchanges an OTP and OTP token for a session, and automatically
// populates merchant context and static QRIS if available.
func (p *Provider) VerifyOTP(ctx context.Context, otp, otpToken string) (*SessionState, error) {
	tokens, err := p.auth.VerifyOTP(ctx, otp, otpToken)
	if err != nil {
		return nil, err
	}

	session := SessionState{
		Tokens:          *tokens,
		DeviceID:        p.deviceID,
		LastRefreshedAt: time.Now().UnixMilli(),
		StaticQRIS:      p.staticQRIS,
	}

	p.mu.Lock()
	if err := p.initSession(session); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	p.mu.Unlock()

	// Discover merchant profile and static QRIS automatically.
	if merchants, err := p.merchantClient.SearchMerchants(ctx, 10); err == nil && len(merchants) > 0 {
		m := merchants[0]
		session.Merchant = &m
		if len(m.Outlets) > 0 {
			session.OutletID = m.Outlets[0].PopID
			if session.StaticQRIS == "" && m.Outlets[0].QRString != "" {
				session.StaticQRIS = m.Outlets[0].QRString
				p.mu.Lock()
				p.staticQRIS = m.Outlets[0].QRString
				p.mu.Unlock()
			}
		} else if session.StaticQRIS == "" && m.QRString != "" {
			session.StaticQRIS = m.QRString
			p.mu.Lock()
			p.staticQRIS = m.QRString
			p.mu.Unlock()
		}
	}

	return p.persistSession(session)
}

// Authenticated reports whether the provider holds a live, unexpired token.
func (p *Provider) Authenticated() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session == nil || p.tokenManager == nil {
		return false
	}
	if p.tokenManager.AccessToken() == "" {
		return false
	}
	return !p.tokenManager.IsExpired()
}

// ExportSession returns a copy of the current session state.
func (p *Provider) ExportSession() *SessionState {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session == nil {
		return nil
	}
	s := *p.session
	return &s
}

// RestoreSession restores an existing session into the provider.
func (p *Provider) RestoreSession(session SessionState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.initSession(session)
}

// RefreshSession forces a token refresh via TokenManager.
func (p *Provider) RefreshSession(ctx context.Context) (*TokenSet, error) {
	p.mu.Lock()
	tm := p.tokenManager
	p.mu.Unlock()
	if tm == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired, "no active session to refresh", nil)
	}
	return tm.ForceRefresh(ctx)
}

// --- Merchant & Outlets ---

// ListMerchants enumerates all merchants the account has access to.
func (p *Provider) ListMerchants(ctx context.Context) ([]StoredMerchant, error) {
	p.mu.Lock()
	isAuth := p.session != nil && p.tokenManager != nil
	p.mu.Unlock()
	if !isAuth {
		return nil, core.NewAuthError(core.CodeAuthRequired, "login before listing merchants", nil)
	}
	return p.merchantClient.SearchMerchants(ctx, 100)
}

// GetMerchantProfile fetches the profile for the currently selected merchant.
func (p *Provider) GetMerchantProfile(ctx context.Context) (*MerchantProfile, error) {
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()
	if session == nil || session.Merchant == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired, "login and select a merchant first", nil)
	}
	return p.merchantClient.GetMerchant(ctx, session.Merchant.ID)
}

// SelectMerchant selects an active merchant and binds its primary outlet QRIS if available.
func (p *Provider) SelectMerchant(ctx context.Context, merchantID string) (*StoredMerchant, error) {
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()
	if session == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired, "login before selecting a merchant", nil)
	}

	profile, err := p.merchantClient.GetMerchant(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	stored := StoredMerchant{
		ID:           profile.ID,
		MerchantName: profile.MerchantName,
		OutletName:   profile.OutletName,
		Phone:        profile.Phone,
		Email:        profile.Email,
		Outlets:      profile.Outlets,
		Raw:          profile.Raw,
	}
	for _, o := range profile.Outlets {
		if o.QRString != "" {
			stored.QRString = o.QRString
			break
		}
	}

	p.mu.Lock()
	if p.activeService != nil {
		_, _ = p.activeService.Deactivate(nil)
	}
	p.session.Merchant = &stored
	if len(stored.Outlets) > 0 {
		p.session.OutletID = stored.Outlets[0].PopID
	}
	if p.staticQRIS == "" && stored.QRString != "" {
		p.staticQRIS = stored.QRString
		p.session.StaticQRIS = stored.QRString
	}
	sessCopy := *p.session
	p.mu.Unlock()

	_, _ = p.persistSession(sessCopy)
	return &stored, nil
}

// SelectOutlet selects an active outlet/POP on the current merchant and binds its static QRIS.
func (p *Provider) SelectOutlet(ctx context.Context, popID string) (*MerchantOutlet, error) {
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()
	if session == nil || session.Merchant == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired, "select a merchant before selecting an outlet", nil)
	}

	for _, outlet := range session.Merchant.Outlets {
		if outlet.PopID == popID {
			p.mu.Lock()
			if p.activeService != nil {
				_, _ = p.activeService.Deactivate(nil)
			}
			p.session.OutletID = popID
			if outlet.QRString != "" {
				p.staticQRIS = outlet.QRString
				p.session.StaticQRIS = outlet.QRString
			}
			sessCopy := *p.session
			p.mu.Unlock()
			_, _ = p.persistSession(sessCopy)
			return &outlet, nil
		}
	}

	return nil, core.NewConfigError("outlet not found on active merchant", map[string]any{"popId": popID})
}

// --- QRIS ---

// SetStaticQRIS binds a static QRIS payload to the provider.
func (p *Provider) SetStaticQRIS(payload string) error {
	if !qris.IsValidQRISChecksum(payload) {
		return core.NewBaseError(core.CodeQRISParseError, "QRIS checksum is invalid")
	}
	p.mu.Lock()
	p.staticQRIS = payload
	if p.session != nil {
		p.session.StaticQRIS = payload
		sessCopy := *p.session
		p.mu.Unlock()
		if p.config.OnSessionUpdated != nil {
			_ = p.config.OnSessionUpdated(sessCopy)
		}
	} else {
		p.mu.Unlock()
	}
	return nil
}

// StaticQRIS returns the currently bound static QRIS payload.
func (p *Provider) StaticQRIS() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.staticQRIS
}

// --- Payments ---

// GetPaymentScope returns the active payment scope.
func (p *Provider) GetPaymentScope() *core.PaymentScope {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paymentScopeLocked()
}

// Payments returns the active merchant/outlet's payment service.
func (p *Provider) Payments() (*payment.Service, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	scope := p.paymentScopeLocked()
	if scope == nil {
		return nil, core.NewAuthError(core.CodeAuthRequired,
			"login and select a merchant before using payments", nil)
	}

	key := fmt.Sprintf("%s:%s:%s", scope.Provider, scope.AccountID, scope.MerchantID)
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

// CreatePayment creates a payment intent on the active scope.
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

// CancelPayment cancels an active pending payment by ID.
func (p *Provider) CancelPayment(ctx context.Context, id string) (*core.Payment, error) {
	svc, err := p.Payments()
	if err != nil {
		return nil, err
	}
	return svc.CancelPayment(ctx, id)
}

// --- Internal helpers ---

func (p *Provider) initSession(session SessionState) error {
	p.session = &session
	if session.DeviceID != "" {
		p.deviceID = session.DeviceID
		p.http.SetDefaultHeader("X-Uniqueid", p.deviceID)
	}
	if session.StaticQRIS != "" && p.staticQRIS == "" {
		p.staticQRIS = session.StaticQRIS
	}

	refreshBuffer := time.Duration(p.config.RefreshBeforeExpiryMs) * time.Millisecond
	if refreshBuffer <= 0 {
		refreshBuffer = DefaultRefreshBeforeExpiryMS * time.Millisecond
	}

	tm := NewTokenManager(p.auth, session.Tokens, TokenManagerConfig{
		RefreshBeforeExpiry: refreshBuffer,
		Logger:              p.logger,
		OnTokenRefreshed: func(tokens TokenSet) error {
			p.mu.Lock()
			if p.session != nil {
				p.session.Tokens = tokens
				p.session.LastRefreshedAt = time.Now().UnixMilli()
				sessCopy := *p.session
				p.mu.Unlock()
				if p.config.OnTokenRefreshed != nil {
					_ = p.config.OnTokenRefreshed(sessCopy)
				}
				if p.config.OnSessionUpdated != nil {
					_ = p.config.OnSessionUpdated(sessCopy)
				}
			} else {
				p.mu.Unlock()
			}
			return nil
		},
	})
	p.tokenManager = tm
	p.http.SetTokenManager(tm)
	return nil
}

func (p *Provider) persistSession(session SessionState) (*SessionState, error) {
	p.mu.Lock()
	p.session = &session
	p.mu.Unlock()

	if p.config.OnSessionUpdated != nil {
		if err := p.config.OnSessionUpdated(session); err != nil {
			return nil, err
		}
	}
	return &session, nil
}

func (p *Provider) paymentScopeLocked() *core.PaymentScope {
	if p.session == nil || p.session.Merchant == nil {
		return nil
	}
	merchantID := p.session.Merchant.ID
	outletID := p.session.OutletID
	if outletID == "" {
		outletID = merchantID
	}
	return &core.PaymentScope{
		Provider:   ProviderID,
		AccountID:  merchantID,
		MerchantID: outletID,
	}
}

func (p *Provider) buildServiceLocked(scope *core.PaymentScope) (*payment.Service, error) {
	pollInterval := core.DefaultPollInterval
	if p.config.PollInterval > 0 {
		pollInterval = time.Duration(p.config.PollInterval) * time.Millisecond
	}
	defaultExpiry := core.DefaultPaymentExpiry
	if p.config.DefaultExpiry > 0 {
		defaultExpiry = time.Duration(p.config.DefaultExpiry) * time.Millisecond
	}
	clockSkew := core.DefaultClockSkew
	if p.config.ClockSkew > 0 {
		clockSkew = time.Duration(p.config.ClockSkew) * time.Millisecond
	}

	svc, err := payment.NewService(payment.Options{
		MerchantID:    scope.MerchantID,
		Scope:         scope,
		Store:         p.store,
		Feed:          p.feed,
		StaticQris:    p.staticQRIS,
		PollInterval:  pollInterval,
		DefaultExpiry: defaultExpiry,
		ClockSkew:     clockSkew,
		Logger:        p.logger,
	})
	if err != nil {
		return nil, err
	}
	return svc, nil
}

func generateDeviceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
