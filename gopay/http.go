package gopay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/utils"
)

// tokenManager is the interface the HTTP client depends on for 401 retry.
// Defined here to avoid a circular dependency with the token package.
type tokenManager interface {
	ForceRefresh(ctx context.Context) (*TokenSet, error)
	AccessToken() string
}

// HTTPClient wraps net/http.Client with GoPay-specific behaviour: Bearer auth,
// baseline headers, JSON marshalling, and 401 auto-retry via a TokenManager.
type HTTPClient struct {
	client       *http.Client
	baseURL      string
	headers      map[string]string
	logger       utils.Logger
	tokenManager tokenManager
}

// NewHTTPClient creates a client for the given base URL.
func NewHTTPClient(baseURL string, logger utils.Logger) *HTTPClient {
	if logger == nil {
		logger = utils.NoopLogger
	}
	return &HTTPClient{
		client: &http.Client{
			Timeout: core.DefaultRequestTimeout,
		},
		baseURL: baseURL,
		headers: DefaultStaticHeaders(),
		logger:  logger,
	}
}

// SetDefaultHeader sets a header sent on every request.
func (c *HTTPClient) SetDefaultHeader(key, value string) {
	c.headers[key] = value
}

// SetTokenManager attaches a TokenManager for 401 auto-retry and Bearer token.
func (c *HTTPClient) SetTokenManager(tm tokenManager) {
	c.tokenManager = tm
	c.SetDefaultHeader("Authorization", "Bearer "+tm.AccessToken())
}

// RequestJSON is a convenience that sends a JSON request and decodes a JSON
// response. It handles 401 auto-retry when a TokenManager is attached.
func (c *HTTPClient) RequestJSON(ctx context.Context, req HTTPRequest, v any) error {
	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		// 401 → try one forced refresh then retry exactly once.
		if resp.StatusCode == http.StatusUnauthorized && c.tokenManager != nil && !req.SkipAuthRetry {
			if _, err := c.tokenManager.ForceRefresh(ctx); err != nil {
				return core.NewAuthError(core.CodeAuthRequired,
					"token refresh failed on 401", err)
			}
			c.SetDefaultHeader("Authorization", "Bearer "+c.tokenManager.AccessToken())
			// Retry once with the new token.
			resp2, err2 := c.Do(ctx, req)
			if err2 != nil {
				return err2
			}
			defer resp2.Body.Close()
			raw2, _ := io.ReadAll(resp2.Body)
			if resp2.StatusCode >= 400 {
				return core.NewHTTPError(resp2.StatusCode,
					fmt.Sprintf("GoPay HTTP error (retry) %d", resp2.StatusCode), string(raw2))
			}
			raw = raw2
			goto decode
		}
		return core.NewHTTPError(resp.StatusCode,
			fmt.Sprintf("GoPay HTTP error %d", resp.StatusCode), string(raw))
	}

decode:
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return core.NewAPIError("GoPay returned unreadable JSON", "", err)
	}
	return nil
}

// HTTPRequest is a single HTTP call.
type HTTPRequest struct {
	Method  string
	Path    string
	Query   map[string]string
	Headers map[string]string
	Body    any
	// SkipAuthRetry prevents 401 auto-retry (auth endpoints).
	SkipAuthRetry bool
}

// Do sends a request and returns the raw response.
func (c *HTTPClient) Do(ctx context.Context, req HTTPRequest) (*http.Response, error) {
	u, err := url.Parse(c.baseURL + req.Path)
	if err != nil {
		return nil, err
	}
	if len(req.Query) > 0 {
		q := u.Query()
		for k, v := range req.Query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	var body io.Reader
	if req.Body != nil {
		var raw []byte
		switch v := req.Body.(type) {
		case string:
			raw = []byte(v)
		default:
			raw, err = json.Marshal(v)
			if err != nil {
				return nil, err
			}
		}
		body = bytes.NewReader(raw)
	}

	method := req.Method
	if method == "" {
		method = http.MethodGet
		if body != nil {
			method = http.MethodPost
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}

	// Baseline headers + defaults.
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	if _, ok := httpReq.Header["Content-Type"]; !ok && body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	return c.client.Do(httpReq)
}

// GoIDTokenPayload is the raw token response from GoID.
type GoIDTokenPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// GoIDEnvelope is the GoID API response envelope.
type GoIDEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Errors  []GoIDError     `json:"errors"`
}

// GoIDError is an error entry in the GoID envelope.
type GoIDError struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	MessageTitle string `json:"message_title"`
}

// IsEmpty reports whether the query has no time range (zero times).
func (q TransactionQuery) IsEmpty() bool {
	return q.StartTime.IsZero() && q.EndTime.IsZero()
}

// buildQueryString converts a TransactionQuery to URL query params.
func buildQueryString(q TransactionQuery, merchantID string) map[string]string {
	params := map[string]string{
		"from":         fmt.Sprintf("%d", q.From),
		"size":         fmt.Sprintf("%d", q.Size),
		"start_time":   q.StartTime.UTC().Format(time.RFC3339),
		"end_time":     q.EndTime.UTC().Format(time.RFC3339),
		"merchant_ids": merchantID,
	}
	if len(q.Statuses) > 0 {
		params["statuses"] = strings.Join(q.Statuses, ",")
	}
	if len(q.PaymentTypes) > 0 {
		params["payment_types"] = strings.Join(q.PaymentTypes, ",")
	}
	return params
}
