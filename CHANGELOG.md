# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `shopee/` provider — OTP login (cookie-JWT), merchant/store selection, dynamic
  QRIS, cursor-based transaction feed, session refresh and persistence.
- `gopay/` provider — OAuth2 bearer auth (GoID), `TokenManager` with
  pre-expiry refresh and 401 auto-retry, merchant/outlet discovery, offset-based
  transaction feed. **In development — untested against the live GoBiz API.**
- `payment/` service — unique-amount allocator, settlement matcher, background
  poller, event callbacks (`OnPaid`, `OnExpired`, `OnError`).
- `qris/` — EMVCo/QRIS parser, static→dynamic amount injection, CRC16.
- `core/` — domain types, ports, and a typed error hierarchy with `IsErrorCode` /
  `AsBaseError` helpers.
- `utils/` — logger, Indonesian phone parsing, ID generator, CRC16.
- CLI test tools under `cmd/` (`login`, `tui`).

### Fixed

- `core.AsBaseError` now resolves embedded `*BaseError` across all error
  subtypes (`AuthError`, `APIError`, `HTTPError`, `ConfigError`,
  `CaptchaRequiredError`), so `core.IsErrorCode` returns the correct code
  instead of always `false`.
- Shopee `Authenticated()` now checks the cookie JWT expiry rather than only
  `session != nil`.
- Shopee `RequirePartnerData` / `RequirePaymentData` now map invalid-token codes
  (`200020`–`200023`) to `AuthError(CodeAuthRequired)` so consumers can detect
  session expiry uniformly.
