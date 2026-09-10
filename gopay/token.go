package gopay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/utils"
)

// TokenManagerConfig configures a TokenManager.
type TokenManagerConfig struct {
	// Callback invoked after a successful refresh, before the rotated tokens
	// become the active in-memory session. Must succeed for adoption to proceed.
	OnTokenRefreshed func(tokens TokenSet) error
	// Refresh tokens this many milliseconds before expiry. Default: 5 minutes.
	RefreshBeforeExpiry time.Duration
	Logger              utils.Logger
}

// TokenManager manages Bearer token lifecycle: proactive pre-expiry refresh,
// 401-forced refresh, and concurrent-call deduplication.
type TokenManager struct {
	refresher TokenRefresher

	mu         sync.Mutex
	tokens     TokenSet
	expiresAt  time.Time
	refreshing *refreshResult

	buffer time.Duration
	config TokenManagerConfig
	logger utils.Logger
}

// refreshResult collapses concurrent callers onto a single in-flight refresh.
type refreshResult struct {
	done   chan struct{}
	tokens TokenSet
	err    error
}

// NewTokenManager creates a TokenManager with an initial token set.
func NewTokenManager(refresher TokenRefresher, initial TokenSet, config TokenManagerConfig) *TokenManager {
	if config.RefreshBeforeExpiry <= 0 {
		config.RefreshBeforeExpiry = DefaultRefreshBeforeExpiryMS * time.Millisecond
	}
	if config.Logger == nil {
		config.Logger = utils.NoopLogger
	}
	m := &TokenManager{
		refresher: refresher,
		tokens:    initial,
		buffer:    config.RefreshBeforeExpiry,
		config:    config,
		logger:    config.Logger,
	}
	m.expiresAt = m.calculateExpiry(initial)
	return m
}

// AccessToken returns the current access token.
func (m *TokenManager) AccessToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tokens.AccessToken
}

// GetValidAccessToken returns a valid access token, refreshing if needed.
func (m *TokenManager) GetValidAccessToken(ctx context.Context) (string, error) {
	if !m.needsRefresh() {
		return m.AccessToken(), nil
	}
	tokens, err := m.refreshOnce(ctx)
	if err != nil {
		return "", err
	}
	return tokens.AccessToken, nil
}

// ForceRefresh unconditionally refreshes and returns the new access token.
// This is what a 401 handler must call — the local expiry estimate is ignored
// because a server-side 401 contradicts it.
func (m *TokenManager) ForceRefresh(ctx context.Context) (*TokenSet, error) {
	tokens, err := m.refreshOnce(ctx)
	if err != nil {
		return nil, err
	}
	return &tokens, nil
}

// needsRefresh reports whether the token is within the refresh buffer.
func (m *TokenManager) needsRefresh() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return time.Until(m.expiresAt) < m.buffer
}

// refreshOnce collapses concurrent callers onto a single in-flight refresh.
func (m *TokenManager) refreshOnce(ctx context.Context) (TokenSet, error) {
	m.mu.Lock()
	if m.refreshing != nil {
		r := m.refreshing
		m.mu.Unlock()
		<-r.done
		return r.tokens, r.err
	}
	r := &refreshResult{done: make(chan struct{})}
	m.refreshing = r
	m.mu.Unlock()

	tokens, err := m.performRefresh(ctx)

	m.mu.Lock()
	r.tokens = tokens
	r.err = err
	close(r.done)
	m.refreshing = nil
	m.mu.Unlock()
	return tokens, err
}

// performRefresh performs the actual refresh. The persistence callback must
// succeed before the rotated token set becomes active.
func (m *TokenManager) performRefresh(ctx context.Context) (TokenSet, error) {
	m.logger.Debug("TokenManager: refreshing access token",
		map[string]any{"expiresAt": m.expiresAt.UTC().Format(time.RFC3339)})

	m.mu.Lock()
	refreshToken := m.tokens.RefreshToken
	m.mu.Unlock()

	refreshed, err := m.refresher.Refresh(ctx, refreshToken)
	if err != nil {
		m.logger.Error("TokenManager: token refresh failed",
			map[string]any{"error": err.Error()})
		if isRejectedRefresh(err) {
			return TokenSet{}, core.NewAuthError(core.CodeAuthRequired,
				"refresh token expired or invalid; login again", err)
		}
		return TokenSet{}, err
	}

	// Adopt a rotated refresh token when supplied; fall back to the current.
	candidate := TokenSet{
		AccessToken:  refreshed.AccessToken,
		RefreshToken: refreshed.RefreshToken,
		TokenType:    refreshed.TokenType,
	}
	if candidate.RefreshToken == "" {
		candidate.RefreshToken = refreshToken
	}
	candidate.ExpiresAt = m.calculateExpiry(candidate).UnixMilli()

	if m.config.OnTokenRefreshed != nil {
		if err := m.config.OnTokenRefreshed(candidate); err != nil {
			m.logger.Error("TokenManager: refreshed token persistence failed",
				map[string]any{"error": err.Error()})
			return TokenSet{}, err
		}
	}

	m.mu.Lock()
	m.tokens = candidate
	m.expiresAt = time.UnixMilli(candidate.ExpiresAt)
	m.mu.Unlock()

	m.logger.Info("TokenManager: token refreshed successfully",
		map[string]any{"newExpiresAt": m.expiresAt.UTC().Format(time.RFC3339)})
	return candidate, nil
}

// isRejectedRefresh reports whether the error means the refresh token was
// rejected (re-login required) rather than a transient failure.
func isRejectedRefresh(err error) bool {
	var authErr *core.AuthError
	if errors.As(err, &authErr) {
		return true
	}
	var httpErr *core.HTTPError
	if errors.As(err, &httpErr) && (httpErr.Status == 400 || httpErr.Status == 401) {
		return true
	}
	return false
}

// calculateExpiry resolves the token expiry with three strategies:
//  1. expiresAt from the auth response;
//  2. the JWT `exp` claim;
//  3. fallback of 30 minutes.
func (m *TokenManager) calculateExpiry(tokens TokenSet) time.Time {
	if tokens.ExpiresAt > 0 {
		return time.UnixMilli(tokens.ExpiresAt)
	}

	if exp, ok := parseJWTExp(tokens.AccessToken); ok {
		return time.Unix(exp, 0)
	}

	fallback := time.Now().Add(30 * time.Minute)
	m.logger.Warn("TokenManager: using fallback expiry (30 min)",
		map[string]any{"expiresAt": fallback.UTC().Format(time.RFC3339)})
	return fallback
}

// GetTokens returns the current token set with a populated expiry.
func (m *TokenManager) GetTokens() TokenSet {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tokens
	t.ExpiresAt = m.expiresAt.UnixMilli()
	return t
}

// parseJWTExp extracts the `exp` claim (seconds) from a JWT access token.
func parseJWTExp(accessToken string) (int64, bool) {
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return 0, false
	}
	seg := parts[1]
	seg = strings.ReplaceAll(seg, "-", "+")
	seg = strings.ReplaceAll(seg, "_", "/")
	// Pad to a multiple of 4.
	if pad := len(seg) % 4; pad != 0 {
		seg += strings.Repeat("=", 4-pad)
	}
	decoded, err := base64.StdEncoding.DecodeString(seg)
	if err != nil {
		return 0, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return 0, false
	}
	if claims.Exp == 0 {
		return 0, false
	}
	return claims.Exp, true
}
