# Security

This project is an **unofficial, community reverse-engineered SDK**. It
authenticates against provider APIs using the same flows as the official
web dashboards — but it is **not** an official integration. By using it you
accept the associated risks, including potential account restrictions.

## Handling credentials & sessions

- **Never commit session files.** Session files contain cookies or OAuth2
  tokens that grant access to your merchant account. This repository's
  `.gitignore` already excludes `session.json` / `*.session.json`.
- **Persist sessions with `OnSessionUpdated` / `OnTokenRefreshed`** so tokens
  are rotated and stored atomically, never logged.
- **Do not log tokens.** The default logger does not redact tokens — pass a
  logger that scrubs `Authorization` / `Cookie` headers in production.

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
