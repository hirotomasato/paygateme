# Contributing

Thanks for wanting to help! This project is a Go SDK for Indonesian merchant
payments (Shopee & GoPay QRIS). Here's how to get started.

## Setup

```bash
git clone https://github.com/hirotomasato/paygateme.git
cd paygateme
go mod download
```

## Running tests

```bash
go test ./...          # all packages
go test -v ./shopee/   # single package with verbose output
```

## Code style

- `gofmt` is the law. Run `gofmt -w .` before committing.
- `go vet ./...` must pass.
- Prefer `for i := range n` over `for i := 0; i < n; i++` (Go 1.22+).
- Errors are typed — every public error is a `*core.BaseError` subtype. Use
  `core.NewAuthError`, `core.NewAPIError`, etc. instead of `fmt.Errorf`.

## Adding a provider

1. Create a new top-level package (`gopay/`, `dana/`, etc.).
2. Implement the interfaces in `core/` (`TransactionFeed`, `PaymentStore`).
3. Write a `Provider` facade with `Authenticated()`, `Payments()`, and
   `ExportSession()`.
4. Port the provider's auth, merchant, and feed from the TypeScript reference
   in `merchantid/`.
5. Add tests.

## Commit messages

Keep it simple: `payment: fix allocator overflow guard` or `shopee: map
invalid-token codes to AuthError`.

## License

By contributing, you agree that your contributions will be licensed under the
MIT License.