package shopee

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// CookieJar is a minimal RFC6265 cookie jar sufficient for Shopee's server-side
// login flow. It implements http.CookieJar so it can be used directly with
// net/http.Client.
type CookieJar struct {
	mu      sync.Mutex
	cookies []*http.Cookie
}

// NewCookieJar creates an empty cookie jar.
func NewCookieJar() *CookieJar {
	return &CookieJar{}
}

// SetCookies implements http.CookieJar. It stores cookies from a Set-Cookie
// header, applying domain/path matching per RFC6265.
func (j *CookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()

	for _, c := range cookies {
		// Normalize domain and path.
		normalized := j.normalizeCookie(c, u)
		if normalized == nil {
			continue
		}
		// Remove any existing cookie with the same name, domain, and path.
		j.removeCookie(normalized)
		j.cookies = append(j.cookies, normalized)
	}
}

// Cookies implements http.CookieJar. Returns cookies matching the URL.
func (j *CookieJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	out := make([]*http.Cookie, 0, len(j.cookies))
	host := canonicalHost(u.Host)

	for _, c := range j.cookies {
		// Skip expired cookies.
		if !c.Expires.IsZero() && c.Expires.Before(now) {
			continue
		}
		// Skip secure cookies on non-HTTPS.
		if c.Secure && u.Scheme != "https" {
			continue
		}
		// Domain match.
		if !domainMatch(host, c.Domain) {
			continue
		}
		// Path match.
		if !pathMatch(u.Path, c.Path) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// Snapshot returns a serializable copy of all cookies.
func (j *CookieJar) Snapshot() []Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()

	out := make([]Cookie, len(j.cookies))
	for i, c := range j.cookies {
		out[i] = cookieFromHTTP(c)
	}
	return out
}

// Restore loads cookies from a serialized snapshot, replacing current contents.
func (j *CookieJar) Restore(snapshot []Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.cookies = make([]*http.Cookie, len(snapshot))
	for i, c := range snapshot {
		j.cookies[i] = cookieToHTTP(c)
	}
}

// Clear removes all cookies.
func (j *CookieJar) Clear() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cookies = nil
}

// Get returns a cookie value by name for the given URL, or empty string.
func (j *CookieJar) Get(name, baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	for _, c := range j.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func (j *CookieJar) normalizeCookie(c *http.Cookie, requestURL *url.URL) *http.Cookie {
	nc := new(http.Cookie)
	*nc = *c

	// Default domain to the request host (hostname, no port).
	if nc.Domain == "" {
		nc.Domain = requestURL.Hostname()
	} else {
		// Go's http.Cookie keeps a leading dot on the Domain attribute; strip it
		// so matching is consistent (mirrors the TS jar).
		nc.Domain = strings.TrimPrefix(strings.ToLower(nc.Domain), ".")
	}
	// Default path.
	if nc.Path == "" {
		nc.Path = defaultPath(requestURL.Path)
	}
	// Reject public suffixes.
	if isPublicSuffix(nc.Domain) {
		return nil
	}
	return nc
}

func (j *CookieJar) removeCookie(target *http.Cookie) {
	for i, c := range j.cookies {
		if c.Name == target.Name && c.Domain == target.Domain && c.Path == target.Path {
			j.cookies = append(j.cookies[:i], j.cookies[i+1:]...)
			return
		}
	}
}

func canonicalHost(host string) string {
	// Strip port for matching.
	if h, _, err := strings.Cut(host, ":"); err {
		host = h
	}
	return strings.ToLower(host)
}

func domainMatch(host, domain string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "."))
	domain = strings.ToLower(strings.TrimPrefix(domain, "."))

	if host == domain {
		return true
	}
	return strings.HasSuffix(host, "."+domain)
}

func pathMatch(requestPath, cookiePath string) bool {
	if cookiePath == "" || cookiePath == "/" {
		return true
	}
	if requestPath == cookiePath {
		return true
	}
	if strings.HasPrefix(requestPath, cookiePath) {
		return len(cookiePath) == 0 || cookiePath[len(cookiePath)-1] == '/' ||
			(len(requestPath) > len(cookiePath) && requestPath[len(cookiePath)] == '/')
	}
	return false
}

func defaultPath(requestPath string) string {
	if !strings.HasPrefix(requestPath, "/") || requestPath == "/" {
		return "/"
	}
	lastSlash := strings.LastIndex(requestPath, "/")
	if lastSlash <= 0 {
		return "/"
	}
	return requestPath[:lastSlash]
}

// isPublicSuffix rejects domains that are public suffixes where cookies cannot
// be set (e.g., "co.id", "com"). This is a minimal list for Shopee's domain
// space.
func isPublicSuffix(domain string) bool {
	domain = strings.ToLower(domain)
	ps := map[string]bool{
		"co.id": true, "or.id": true, "go.id": true, "ac.id": true,
		"net.id": true, "sch.id": true, "web.id": true, "my.id": true,
		"com": true, "org": true, "net": true, "io": true, "dev": true,
	}
	if ps[domain] {
		return true
	}
	// Also reject any label that is just a TLD/suffix.
	parts := strings.Split(domain, ".")
	if len(parts) == 1 {
		return true
	}
	return false
}

// cookieFromHTTP converts an http.Cookie to a serializable Cookie.
func cookieFromHTTP(c *http.Cookie) Cookie {
	exp := int64(0)
	if !c.Expires.IsZero() {
		exp = c.Expires.UnixMilli()
	}
	return Cookie{
		Name:     c.Name,
		Value:    c.Value,
		Domain:   c.Domain,
		Path:     c.Path,
		Secure:   c.Secure,
		HTTPOnly: c.HttpOnly,
		Expires:  exp,
		HostOnly: c.Domain == "" || !strings.HasPrefix(c.Domain, "."),
	}
}

// cookieToHTTP converts a serializable Cookie to an http.Cookie.
func cookieToHTTP(c Cookie) *http.Cookie {
	exp := time.Time{}
	if c.Expires > 0 {
		exp = time.UnixMilli(c.Expires)
	}
	return &http.Cookie{
		Name:     c.Name,
		Value:    c.Value,
		Domain:   c.Domain,
		Path:     c.Path,
		Secure:   c.Secure,
		HttpOnly: c.HTTPOnly,
		Expires:  exp,
	}
}
