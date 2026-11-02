package gopay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockRefresher struct {
	calls atomic.Int32
	delay time.Duration
	fn    func(ctx context.Context, refreshToken string) (*TokenSet, error)
}

func (m *mockRefresher) Refresh(ctx context.Context, refreshToken string) (*TokenSet, error) {
	m.calls.Add(1)
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.fn != nil {
		return m.fn(ctx, refreshToken)
	}
	return &TokenSet{
		AccessToken:  "new-access-token",
		RefreshToken: "new-refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).UnixMilli(),
		TokenType:    "Bearer",
	}, nil
}

func makeJWTWithExp(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payloadData, _ := json.Marshal(map[string]any{"exp": exp})
	payload := base64.RawURLEncoding.EncodeToString(payloadData)
	return fmt.Sprintf("%s.%s.", header, payload)
}

func TestTokenManagerExpiryCalculation(t *testing.T) {
	refresher := &mockRefresher{}
	futureExp := time.Now().Add(2 * time.Hour).Unix()
	jwt := makeJWTWithExp(futureExp)

	initial := TokenSet{
		AccessToken:  jwt,
		RefreshToken: "refresh-123",
	}

	tm := NewTokenManager(refresher, initial, TokenManagerConfig{})
	if tm.IsExpired() {
		t.Errorf("expected token not to be expired")
	}
	if tm.AccessToken() != jwt {
		t.Errorf("expected access token to match initial")
	}
}

func TestTokenManagerForceRefresh(t *testing.T) {
	refresher := &mockRefresher{}
	initial := TokenSet{
		AccessToken:  "old-token",
		RefreshToken: "old-refresh",
	}

	var notified atomic.Bool
	tm := NewTokenManager(refresher, initial, TokenManagerConfig{
		OnTokenRefreshed: func(tokens TokenSet) error {
			notified.Store(true)
			return nil
		},
	})

	newTokens, err := tm.ForceRefresh(context.Background())
	if err != nil {
		t.Fatalf("force refresh failed: %v", err)
	}
	if newTokens.AccessToken != "new-access-token" {
		t.Errorf("expected new access token, got %s", newTokens.AccessToken)
	}
	if tm.AccessToken() != "new-access-token" {
		t.Errorf("expected AccessToken() to update, got %s", tm.AccessToken())
	}
	if !notified.Load() {
		t.Errorf("expected OnTokenRefreshed callback to be called")
	}
	if refresher.calls.Load() != 1 {
		t.Errorf("expected 1 refresh call, got %d", refresher.calls.Load())
	}
}

func TestTokenManagerConcurrentRefreshDeduplication(t *testing.T) {
	refresher := &mockRefresher{delay: 50 * time.Millisecond}
	initial := TokenSet{
		AccessToken:  "old-token",
		RefreshToken: "old-refresh",
	}

	tm := NewTokenManager(refresher, initial, TokenManagerConfig{})

	var wg sync.WaitGroup
	const goroutines = 5
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _ = tm.ForceRefresh(context.Background())
		}()
	}

	wg.Wait()
	if refresher.calls.Load() != 1 {
		t.Errorf("expected single collapsed refresh call, got %d", refresher.calls.Load())
	}
}

