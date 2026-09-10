package shopee

import (
	"net/http"
	"net/url"
	"testing"
)

func TestCookieJarDomainAttributeLeadingDot(t *testing.T) {
	jar := NewCookieJar()
	u, _ := url.Parse("https://partner.shopee.co.id/path")

	// A cookie with Domain=.shopee.co.id must match partner.shopee.co.id.
	jar.SetCookies(u, []*http.Cookie{{
		Name:   "SPC_TEST",
		Value:  "abc",
		Domain: ".shopee.co.id",
		Path:   "/",
	}})

	got := jar.Get("SPC_TEST", "https://partner.shopee.co.id/some/other/path")
	if got != "abc" {
		t.Fatalf("domain cookie did not match subdomain: got %q", got)
	}

	// And it must match a different subdomain too.
	got = jar.Get("SPC_TEST", "https://shopeepay.shopee.co.id/")
	if got != "abc" {
		t.Fatalf("domain cookie did not match sibling subdomain: got %q", got)
	}
}

func TestCookieJarHostOnlyDoesNotLeak(t *testing.T) {
	jar := NewCookieJar()
	u, _ := url.Parse("https://partner.shopee.co.id/")

	// A host-only cookie (no Domain) must NOT match a sibling subdomain.
	jar.SetCookies(u, []*http.Cookie{{
		Name:  "CSRF",
		Value: "secret",
		Path:  "/",
	}})

	if got := jar.Get("CSRF", "https://partner.shopee.co.id/"); got != "secret" {
		t.Fatalf("host-only cookie did not match its own host: %q", got)
	}
	if got := jar.Get("CSRF", "https://shopeepay.shopee.co.id/"); got != "" {
		t.Fatalf("host-only cookie leaked to sibling subdomain: %q", got)
	}
}

func TestCookieJarSnapshotRestore(t *testing.T) {
	jar := NewCookieJar()
	u, _ := url.Parse("https://partner.shopee.co.id/")
	jar.SetCookies(u, []*http.Cookie{{
		Name:   "SPC_A",
		Value:  "1",
		Domain: ".shopee.co.id",
		Path:   "/",
	}})

	snapshot := jar.Snapshot()
	jar2 := NewCookieJar()
	jar2.Restore(snapshot)

	if got := jar2.Get("SPC_A", "https://partner.shopee.co.id/"); got != "1" {
		t.Fatalf("restored cookie lost value: %q", got)
	}
}
