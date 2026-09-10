package shopee

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/utils"
)

// partnerAPIHost is the host of the merchant/switch partner API, which
// authenticates by header only and never receives a cookie.
const partnerAPIHost = "api.partner.shopee.co.id"

// BrowserHeaders is the desktop-browser identity observed in the reference
// capture. Shopee's fraud gateway silently suppresses OTP delivery for
// requests that do not look like the official passport web client.
var BrowserHeaders = map[string]string{
	"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:153.0) Gecko/20100101 Firefox/153.0",
	"Accept":          "application/json, text/plain, */*",
	"Accept-Language": "id,en-US;q=0.9,en;q=0.8",
	"Sec-Fetch-Dest":  "empty",
	"Sec-Fetch-Mode":  "cors",
	"Sec-Fetch-Site":  "same-site",
}

// HTTPClient wraps net/http with Shopee cookie management and JSON envelope
// handling.
type HTTPClient struct {
	client    *http.Client
	cookieJar *CookieJar
	logger    utils.Logger
}

// NewHTTPClient creates a Shopee HTTP client with a fresh cookie jar.
func NewHTTPClient(logger utils.Logger) *HTTPClient {
	if logger == nil {
		logger = utils.NoopLogger
	}
	jar := NewCookieJar()
	client := &http.Client{
		Jar:     jar,
		Timeout: core.DefaultRequestTimeout,
	}
	return &HTTPClient{client: client, cookieJar: jar, logger: logger}
}

// Jar returns the cookie jar.
func (h *HTTPClient) Jar() *CookieJar { return h.cookieJar }

// Request is a generic Shopee HTTP request.
type Request struct {
	URL     string
	Method  string
	Query   map[string]string
	Headers map[string]string
	Body    any
}

// Do performs a request and returns the raw response.
func (h *HTTPClient) Do(ctx context.Context, req Request) (*http.Response, error) {
	u, err := url.Parse(req.URL)
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
	// String bodies (device-risk telemetry) are sent verbatim, matching the
	// TS behaviour. Everything else is JSON-marshalled.
	if req.Body != nil {
		if s, ok := req.Body.(string); ok {
			body = strings.NewReader(s)
		} else {
			b, err := json.Marshal(req.Body)
			if err != nil {
				return nil, err
			}
			body = bytes.NewReader(b)
		}
	}

	method := req.Method
	if method == "" {
		method = http.MethodGet
		if req.Body != nil {
			method = http.MethodPost
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	for k, v := range BrowserHeaders {
		httpReq.Header.Set(k, v)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	// Only default Content-Type when the caller hasn't set it (and the body
	// is not a raw string, which sets its own Content-Type).
	if req.Body != nil {
		if _, ok := req.Body.(string); !ok {
			if _, has := req.Headers["Content-Type"]; !has {
				httpReq.Header.Set("Content-Type", "application/json")
			}
		}
	}

	// The partner API host (`api.partner.shopee.co.id`) authenticates by
	// header only and, in the reference capture, never receives a cookie.
	// Sending the dashboard token cookie there can contradict the fresh
	// merchant token. Use a client without a Jar so no cookies are sent.
	client := h.client
	if u.Hostname() == partnerAPIHost {
		client = &http.Client{
			Transport: h.client.Transport,
			Timeout:   h.client.Timeout,
		}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// JSON performs a request and decodes the JSON response into v. It returns the
// raw body on HTTP errors.
func (h *HTTPClient) JSON(ctx context.Context, req Request, v any) error {
	resp, err := h.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return core.NewHTTPError(resp.StatusCode, "Shopee HTTP error", string(raw))
	}

	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return core.NewAPIError("Shopee returned unreadable JSON", "", err)
	}
	return nil
}

// FollowGet performs a GET and follows redirects while capturing cookies. It
// returns the final response body. Accept matches the TS followGet baseline.
func (h *HTTPClient) FollowGet(ctx context.Context, urlStr string) ([]byte, error) {
	return h.FollowGetWithHeaders(ctx, urlStr, nil, nil)
}

// FollowGetWithHeaders is FollowGet with optional query params and extra headers.
// It manually follows redirects (matching the TS `followGet`), so each hop is
// a fresh request with baseline headers and no Referer auto-set by Go.
func (h *HTTPClient) FollowGetWithHeaders(ctx context.Context, urlStr string, query, headers map[string]string) ([]byte, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	if len(query) > 0 {
		q := u.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	// Client that STOPS at redirects (returns the 3xx), so we can follow
	// manually. The Jar still captures cookies from each response.
	client := &http.Client{
		Jar: h.cookieJar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: core.DefaultRequestTimeout,
	}

	currentURL := u.String()
	for redirect := 0; redirect <= 5; redirect++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range BrowserHeaders {
			req.Header.Set(k, v)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			// Final response (not a redirect).
			defer resp.Body.Close()
			return io.ReadAll(resp.Body)
		}

		location := resp.Header.Get("Location")
		resp.Body.Close() // drain redirect body
		if location == "" {
			return nil, core.NewHTTPError(resp.StatusCode,
				"Shopee redirect did not include a location", nil)
		}

		// Resolve relative URL against the current URL.
		locURL, err := url.Parse(location)
		if err != nil {
			return nil, err
		}
		base, _ := url.Parse(currentURL)
		currentURL = base.ResolveReference(locURL).String()
	}

	return nil, core.NewHTTPError(508, "Shopee redirect limit was exceeded", nil)
}

// ShopeeURL builds a URL from a base and path.
func ShopeeURL(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
