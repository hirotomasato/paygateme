# Security

This project is an **unofficial, community reverse-engineered SDK**. It
authenticates against provider APIs using the same flows as the official
web dashboards — but it is **not** an official integration. By using it you
accept the associated risks, including potential account restrictions.

## Handling credentials & sessions

- **Never commit session files.** Session files contain cookies or OAuth2
  tokens that grant access to your merchant account. This repository's
  `.gitignore` already excludes `session.json` / `*.session.json`.
- **Encrypt sessions at rest.** Use the `utils.Encrypt` / `utils.Decrypt` helpers
  or pass the `-passphrase` flag to CLI tools so credentials are saved as
  AES-256-GCM encrypted blobs rather than plaintext JSON.
- **Persist sessions with `OnSessionUpdated` / `OnTokenRefreshed`** so tokens
  are rotated and stored atomically, never logged.
- **Do not log tokens.** The default logger does not redact tokens — pass a
  logger that scrubs `Authorization` / `Cookie` headers in production.

## Payment Matching Security Guarantees

- **Fail-Closed Settlement**: `MatchesPayment` strictly validates transaction statuses
  against an explicit success whitelist (`settlement`, `capture`, `paid`, `completed`, `success`).
  Empty, unrecognized, or pending statuses are never accepted.
- **Strict Timestamp Enforcement**: Transactions missing a valid timestamp (`Time.IsZero()`)
  are rejected to prevent stale lookback replays from settling new orders.
- **Single-Process Allocation Scope**: In-memory amount uniqueness (`AmountAllocator`)
  and quarantines operate per single process. Distributed multi-instance deployments
  must coordinate amount allocation and deduplication through an external store.

## Reporting a vulnerability

Please **do not open a public issue** for security-sensitive findings. Instead,
report them privately to the maintainers. We will acknowledge within 7 days
and work with you on a coordinated fix and disclosure.

## Scope

| In scope | Out of scope |
|----------|--------------|
| Token leakage in logs | Provider ToS violations |
| Session persistence flaws | Social engineering |
| Error paths that expose secrets | Third-party library issues |
