# PayGateMe

```
 ▄▄▄▄▄▄                           ▄▄▄▄                                 ▄▄▄  ▄▄▄
 ██▀▀▀▀█▄                       ██▀▀▀▀█              ██                ███  ███
 ██    ██   ▄█████▄  ▀██  ███  ██         ▄█████▄  ███████    ▄████▄   ████████
 ██████▀    ▀ ▄▄▄██   ██▄ ██   ██  ▄▄▄▄   ▀ ▄▄▄██    ██      ██▄▄▄▄██  ██ ██ ██
 ██        ▄██▀▀▀██    ████▀   ██  ▀▀██  ▄██▀▀▀██    ██      ██▀▀▀▀▀▀  ██ ▀▀ ██
 ██        ██▄▄▄███     ███     ██▄▄▄██  ██▄▄▄███    ██▄▄▄   ▀██▄▄▄▄█  ██    ██
 ▀▀         ▀▀▀▀ ▀▀     ██        ▀▀▀▀    ▀▀▀▀ ▀▀     ▀▀▀▀     ▀▀▀▀▀   ▀▀    ▀▀
```

**Go library for Shopee & GoPay merchant payments** — OTP login, dynamic QRIS, live transaction feed.

> ⚠️ **Unofficial SDK** — this is a community reverse-engineered library. It is not affiliated with, endorsed by, or connected to Shopee, GoPay, Gojek, or any of their affiliates. Use at your own risk.

## 📁 Project structure

```
.
├── cmd/
│   ├── login/           OTP login CLI
│   └── tui/             terminal payment app (test harness)
├── core/                domain types, errors, payment scope — zero deps
├── shopee/              Shopee provider (cookie-JWT auth)
├── gopay/               GoPay provider (Bearer-OAuth2) — 🚧 in dev
├── payment/             allocator, matcher, service
├── qris/                EMVCo/QRIS parser, CRC16
├── utils/               logger, phone, ID, CRC16
├── README.md
├── CHANGELOG.md
├── CONTRIBUTING.md
├── SECURITY.md
├── CODE_OF_CONDUCT.md
├── LICENSE
├── go.mod
└── go.sum
```

| Package | Purpose | Deps |
|---------|---------|------|
| `core/` | Domain types, error hierarchy, payment scope | none |
| `shopee/` | Shopee provider: cookie‑JWT auth, merchant, feed, QRIS | `core`, `payment`, `qris`, `utils` |
| `gopay/` | GoPay provider — **🚧 in development** | `core`, `payment`, `qris`, `utils` |
| `payment/` | Unique‑amount allocator, settlement matcher, service orchestrator | `core`, `qris`, `utils` |
| `qris/` | EMVCo/QRIS parser, static→dynamic injection, CRC16 | `core`, `utils` |
| `utils/` | Logger, phone, ID, CRC16 helpers | none |

---

## Install

```bash
go get github.com/hirotomasato/paygateme
```

…

## Providers at a glance

| | Shopee | GoPay |
|---|---|---|
| **Auth model** | Cookie JWT + OTP (passport SSO) | Bearer OAuth2 + GoID OTP |
| **Token refresh** | `RefreshSession()` — re‑mint via SSO exchange | `TokenManager` — pre‑expiry + 401 auto‑retry |
| **Session expiry** | `Authenticated()` checks cookie JWT `exp` | `Authenticated()` checks `TokenManager` liveness |
| **QRIS** | Static EMVCo → dynamic via tag‑54 injection | Static from outlet profile |
| **Feed** | `POST` cursor‑based pagination | `GET` offset‑based pagination, minor‑unit ÷100 |
| **Merchant** | Store list per merchant | `searchMerchants` + outlet QRIS |
| **HTTP** | Custom `CookieJar`, partner‑API cookieless | `net/http` + Bearer header, 401 retry interceptor |

---

## Quick start — Shopee

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/payment"
	"github.com/hirotomasato/paygateme/shopee"
)

func main() {
	ctx := context.Background()

	// 1. Build provider — no session yet.
	provider := shopee.NewProvider(shopee.ProviderConfig{
		DeviceReport: shopee.DeviceRiskBlob,
	})

	// 2. Request OTP.
	challenge, _ := provider.RequestOtp(ctx, "081234567890", shopee.OtpRequestOptions{})
	fmt.Printf("OTP sent — channel %d\n", challenge.Channel)

	// 3. Enter OTP → login.
	fmt.Print("OTP: ")
	var otp string
	fmt.Scanln(&otp)
	outcome, _ := provider.LoginWithOtp(ctx, shopee.LoginWithOtpInput{
		Challenge: *challenge, OTP: otp,
	})
	fmt.Println("Logged in:", outcome.Session.Merchant.Name)

	// 4. Bind static QRIS.
	provider.SetStaticQris("00020101021126610016ID.CO.SHOPEE.WWW...")

	// 5. Create payment.
	svc, _ := provider.Payments()
	p, _ := svc.CreatePayment(ctx, payment.CreatePaymentInput{
		Amount: 1500, Reference: "order-001",
	})
	fmt.Println("Payment:", p.ID, "QR:", p.QRString[:60])
	fmt.Println("Unique amount:", p.UniqueAmount) // 1501

	// 6. Realtime settlement.
	svc.OnPaid(func(p core.Payment) {
		fmt.Printf("✓ LUNAS %s — %s\n", p.ID, rupiah(p.UniqueAmount))
	})
	svc.Start()
	defer svc.Stop()
	time.Sleep(5 * time.Minute)

	// 7. Persist session.
	session := provider.ExportSession()
	out, _ := json.MarshalIndent(session, "", "  ")
	os.WriteFile("session.json", out, 0o600)
}

func rupiah(n int64) string { return fmt.Sprintf("Rp %d", n) }
```

## Quick start — GoPay

> 🚧 **GoPay provider is still in active development.** The types and API surface are in place but have not been tested against the live GoBiz API. See the [Shopee quick start](#quick-start--shopee) for a production‑ready provider.

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hirotomasato/paygateme/gopay"
	"github.com/hirotomasato/paygateme/payment"
)

func main() {
	ctx := context.Background()
	provider := gopay.NewProvider(gopay.ProviderConfig{
		OnTokenRefreshed: func(s gopay.SessionState) error {
			out, _ := json.MarshalIndent(s, "", "  ")
			return os.WriteFile("gopay-session.json", out, 0o600)
		},
	})

	// 1. Request OTP.
	res, _ := provider.RequestOTP(ctx, "081234567890", "62")
	fmt.Println("OTP sent, token:", res.OTPToken)

	// 2. Verify OTP.
	tokens, _ := provider.VerifyOTP(ctx, "123456", res.OTPToken)
	fmt.Println("Access token:", tokens.AccessToken[:20]+"...")

	// 3. Resolve merchant + QRIS from outlet profile.
	profile, _ := provider.GetMerchantProfile(ctx)
	fmt.Println("Merchant:", profile.MerchantName)
	fmt.Println("QRIS:", provider.StaticQRIS())

	// 4. Create payment.
	svc, _ := provider.Payments()
	p, _ := svc.CreatePayment(ctx, payment.CreatePaymentInput{
		Amount: 5000, Reference: "gopay-order-001",
	})
	fmt.Println("Payment:", p.ID, "Unique:", p.UniqueAmount)

	svc.OnPaid(func(p payment.Payment) {
		fmt.Printf("✓ LUNAS %s\n", p.ID)
	})
	svc.Start()
	defer svc.Stop()
	time.Sleep(5 * time.Minute)

	// 5. Persist session.
	session := provider.ExportSession()
	out, _ := json.MarshalIndent(session, "", "  ")
	os.WriteFile("gopay-session.json", out, 0o600)
}
```

## Restore previous session (no OTP)

### Shopee

```go
var session shopee.Session
json.Unmarshal(os.ReadFile("session.json"), &session)
provider := shopee.NewProvider(shopee.ProviderConfig{
	Session: &session,
	OnSessionUpdated: func(s shopee.Session) error {
		out, _ := json.MarshalIndent(s, "", "  ")
		return os.WriteFile("session.json", out, 0o600)
	},
})
if !provider.Authenticated() { /* re-login */ }
```

### GoPay

```go
var session gopay.SessionState
json.Unmarshal(os.ReadFile("gopay-session.json"), &session)
provider := gopay.NewProvider(gopay.ProviderConfig{
	Session: &session,
	OnTokenRefreshed: func(s gopay.SessionState) error {
		out, _ := json.MarshalIndent(s, "", "  ")
		return os.WriteFile("gopay-session.json", out, 0o600)
	},
})
if !provider.Authenticated() { /* re-login */ }
```

---

## Realtime feed polling

```go
svc.OnPaid(func(p core.Payment) {
	// Payment settled — buyer's transaction matched.
	fmt.Println("LUNAS", p.ID, p.Transaction.Amount)
})
svc.OnExpired(func(p core.Payment) {
	fmt.Println("EXPIRED", p.ID)
})
svc.OnError(func(err error) {
	if core.IsErrorCode(err, core.CodeAuthRequired) {
		fmt.Println("SESSION EXPIRED — re-login required")
	}
})
svc.Start() // begins background polling
defer svc.Stop()
```

Or manual tick:

```go
res, err := svc.Tick(ctx)
fmt.Println("paid:", len(res.Paid), "expired:", len(res.Expired))
```

---

## Session expiry — unified contract

All providers surface expiry through the same error code. The embedding project only needs **one check**:

```go
if !provider.Authenticated() || core.IsErrorCode(err, core.CodeAuthRequired) {
	// Re-login with OTP.
}
```

| Mechanism | Provider | Signal | Detect |
|-----------|----------|--------|--------|
| Token expired (JWT) | Shopee | `Authenticated()` → `false` | `if !provider.Authenticated()` |
| Token expired (OAuth2) | GoPay | `TokenManager` refresh fails | `core.IsErrorCode(err, core.CodeAuthRequired)` |
| Token rejected by API (200020‑23) | Shopee | `*core.AuthError` | `core.IsErrorCode(err, core.CodeAuthRequired)` |
| Account session expired | Shopee | `RefreshSession()` error | `core.IsErrorCode(err, core.CodeAuthRequired)` |
| Bearer token rejected | GoPay | `TokenManager` 401 → forced refresh | `core.IsErrorCode(err, core.CodeAuthRequired)` |

---

## Cancel a payment

```go
cancelled, err := svc.CancelPayment(ctx, payment.ID)
// cancelled.Status == core.PaymentCancelled
```

---

## Error handling

Every error is a `*core.BaseError` subtype. Branch with `core.IsErrorCode`:

```go
if core.IsErrorCode(err, core.CodeAuthRequired) {
	// re-login
} else if core.IsErrorCode(err, core.CodeAPIError) {
	// transient API error — retry
} else if core.IsErrorCode(err, core.CodeHTTPError) {
	// network failure
}
```

Extract code and message:

```go
var be *core.BaseError
if core.AsBaseError(err, &be) {
	fmt.Println("code:", be.Code, "message:", be.Message)
}
```

| Error type | Detect with | Meaning |
|------------|-------------|---------|
| `*core.AuthError` | `IsErrorCode(err, CodeAuthRequired)` or `CodeAuthFailed` | Login / session expired |
| `*core.APIError` | `IsErrorCode(err, CodeAPIError)` | Provider API returned an error |
| `*core.HTTPError` | `IsErrorCode(err, CodeHTTPError)` | HTTP transport failure |
| `*core.ConfigError` | `IsErrorCode(err, CodeConfigInvalid)` | Misconfiguration |
| `*core.CaptchaRequiredError` | `IsErrorCode(err, CodeCaptchaRequired)` | CAPTCHA challenge (Shopee) |

---

## 🖥 Demo

```console
$ paygateme-login
phone (e.g. 0812xxxx or +62...): 08xxxxxxxxxx
password (empty if passwordless):
OTP requested (channel 3, hasPassword=false). Check your phone.
OTP code: 123456
login OK: merchant Merchant Name (id=xxxxxxxx)
session saved to ~/.paygateme/session.json

$ paygateme-tui -restore ~/.paygateme/session.json \
  -save ~/.paygateme/session.json \
  -qris-file /tmp/qris.txt

  ╭─ Merchant Name · ⠋ LIVE · SESI 0 LUNAS ───────────────────╮
  │    █▀▀▀▀▀█  ▀▄▄▀ ▀█▄▀▀   █▄█ █▄▀   █▀▀▀▀▀█    Rp 1.500  │
  │    █ ███ █ ▀▄▄  ▄▀▀▄▀  ▀█▀ █ █▄▀▀    █ ███ █              │
  │    █ ▀▀▀ █  ▄██ █  ▀▀████    ▀█▀▄▀▀█   █ ▀▀▀ █  ⠋ LIVE   │
  │    ...                                           04:55     │
  ╰──────────────────────────────────────────────────────────╯

  [n] order baru   [c] batalkan   [0-9] nominal   [q] keluar   ↑↓ scroll
```

---

## Testing from the CLI

The `cmd/` directory ships small CLI tools you can run to exercise the providers end‑to‑end.

### Shopee

```bash
# OTP login (interactive — asks phone + OTP)
go run ./cmd/login -save ~/.paygateme/session.json

# Quick smoke test: restore session, create a payment via the terminal app
go run ./cmd/tui -restore ~/.paygateme/session.json \
  -save ~/.paygateme/session.json \
  -qris-file /tmp/qris.txt
```

### GoPay

```bash
# (Coming soon — provider is still in development)
```

---

## Package layout

```
core/        Domain types, errors, payment scope — zero deps
shopee/      Shopee provider: auth, merchant, feed, QRIS binding
gopay/       GoPay provider: auth, merchant, feed, token lifecycle
payment/     Payment service: allocator, matcher, store
qris/        EMVCo/QRIS parser, dynamic payload builder, CRC16
utils/       Logger, phone, ID, CRC16 helpers
cmd/         CLI test tools (login, terminal)
```

---

## License

MIT