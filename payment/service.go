package payment

import (
	"context"
	"sync"
	"time"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/qris"
	"github.com/hirotomasato/paygateme/utils"
)

// CreatePaymentInput is the input to Service.CreatePayment.
type CreatePaymentInput struct {
	// Base amount in whole rupiah.
	Amount int64
	// Optional caller reference (internal order id, etc.).
	Reference string
	// Override the default expiry for this payment. Zero uses the default.
	ExpiresIn time.Duration
	// Opaque metadata.
	Metadata map[string]any
}

// Options configures a payment Service.
type Options struct {
	MerchantID string
	// Provider ownership for isolating records in a shared store. Nil means
	// unscoped mode.
	Scope *core.PaymentScope
	Store core.PaymentStore
	// Provider-normalized feed whose adapter owns pagination and conversion.
	Feed core.TransactionFeed
	// Static QRIS payload used to derive per-order dynamic QRIS strings.
	StaticQris string
	// Allocator for unique amounts. Nil uses the default.
	Allocator *AmountAllocator
	// PollInterval is the delay between background ticks. Zero uses default.
	PollInterval time.Duration
	// DefaultExpiry is how long payments stay pending. Zero uses default.
	DefaultExpiry time.Duration
	// ClockSkew is the matching tolerance. Zero uses default.
	ClockSkew time.Duration
	// PageSize is the suggested feed page size, clamped to [1, 100].
	PageSize int
	Logger   utils.Logger
	// LifecycleToken guards activate/deactivate when set by a provider facade.
	LifecycleToken any
}

// TickResult is the outcome of a single reconciliation pass.
type TickResult struct {
	Paid    []core.Payment
	Expired []core.Payment
}

// Service orchestrates dynamic payments with unique amounts and polls the
// transaction feed to settle or expire them. All state transitions are
// serialized through a single write queue so cancel, settle, and expire can
// never overwrite each other's terminal state.
type Service struct {
	merchantID string
	scope      *core.PaymentScope
	feedScope  core.PaymentScope
	store      core.PaymentStore
	feed       core.TransactionFeed
	staticQris string
	allocator  *AmountAllocator

	pollInterval  time.Duration
	defaultExpiry time.Duration
	clockSkew     time.Duration
	pageSize      int
	logger        utils.Logger
	lifecycleToken any

	// writeMu serializes allocations and state transitions.
	writeMu sync.Mutex

	// stateMu guards the quarantine and consumed-transaction maps. It is a leaf
	// lock: never acquire writeMu while holding stateMu.
	stateMu            sync.Mutex
	recentlyFreed      map[int64]time.Time
	consumedTxIDs      map[string]time.Time

	// Lifecycle state.
	activeMu        sync.Mutex
	active          bool
	activationEpoch int64

	// Background polling.
	stopCh   chan struct{}
	stopOnce sync.Once
	tickWG   sync.WaitGroup

	// Event handlers.
	handlerMu       sync.Mutex
	paidHandlers    []func(core.Payment)
	expiredHandlers []func(core.Payment)
	errorHandlers   []func(error)
}

// NewService constructs a payment Service.
func NewService(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, core.NewConfigError("store is required", nil)
	}
	if opts.Feed == nil {
		return nil, core.NewConfigError("feed is required", nil)
	}
	if opts.MerchantID == "" {
		return nil, core.NewConfigError("merchantId is required", nil)
	}
	if opts.Scope != nil && opts.Scope.MerchantID != opts.MerchantID {
		return nil, core.NewConfigError("scope.merchantId must match merchantId", nil)
	}

	s := &Service{
		merchantID: opts.MerchantID,
		scope:      opts.Scope,
		store:      opts.Store,
		feed:       opts.Feed,
		staticQris: opts.StaticQris,
		allocator:  opts.Allocator,
		pollInterval: opts.PollInterval,
		defaultExpiry: opts.DefaultExpiry,
		clockSkew:     opts.ClockSkew,
		pageSize:      opts.PageSize,
		logger:        opts.Logger,
		lifecycleToken: opts.LifecycleToken,
		recentlyFreed: make(map[int64]time.Time),
		consumedTxIDs: make(map[string]time.Time),
		active:        true,
		stopCh:        make(chan struct{}),
	}

	// Default feed scope: when unscoped, GoPay offset feeds still need a scope
	// shape; the explicit-scope check below decides ownership separately.
	if s.scope != nil {
		s.feedScope = *s.scope
	} else {
		s.feedScope = core.PaymentScope{Provider: "gopay", MerchantID: opts.MerchantID}
	}

	if s.allocator == nil {
		s.allocator = DefaultAmountAllocator()
	}
	if s.pollInterval <= 0 {
		s.pollInterval = core.DefaultPollInterval
	}
	if s.defaultExpiry <= 0 {
		s.defaultExpiry = core.DefaultPaymentExpiry
	}
	if s.clockSkew <= 0 {
		s.clockSkew = core.DefaultClockSkew
	}
	if s.logger == nil {
		s.logger = utils.NoopLogger
	}
	s.pageSize = clampPageSize(s.pageSize)

	return s, nil
}

func clampPageSize(n int) int {
	if n <= 0 {
		return core.DefaultTransactionPageSize
	}
	if n > core.MaxTransactionPageSize {
		return core.MaxTransactionPageSize
	}
	return n
}

// ---- Events ----

// OnPaid registers a handler invoked when a payment settles.
func (s *Service) OnPaid(fn func(core.Payment)) *Service {
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	s.paidHandlers = append(s.paidHandlers, fn)
	return s
}

// OnExpired registers a handler invoked when a payment expires.
func (s *Service) OnExpired(fn func(core.Payment)) *Service {
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	s.expiredHandlers = append(s.expiredHandlers, fn)
	return s
}

// OnError registers a handler invoked on polling or listener failures.
func (s *Service) OnError(fn func(error)) *Service {
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	s.errorHandlers = append(s.errorHandlers, fn)
	return s
}

func (s *Service) emitPaid(p core.Payment) {
	s.handlerMu.Lock()
	handlers := make([]func(core.Payment), len(s.paidHandlers))
	copy(handlers, s.paidHandlers)
	s.handlerMu.Unlock()
	for _, h := range handlers {
		safeCall(func() { h(p) }, func(err error) { s.emitError(err) })
	}
}

func (s *Service) emitExpired(p core.Payment) {
	s.handlerMu.Lock()
	handlers := make([]func(core.Payment), len(s.expiredHandlers))
	copy(handlers, s.expiredHandlers)
	s.handlerMu.Unlock()
	for _, h := range handlers {
		safeCall(func() { h(p) }, func(err error) { s.emitError(err) })
	}
}

func (s *Service) emitError(err error) {
	s.handlerMu.Lock()
	handlers := make([]func(error), len(s.errorHandlers))
	copy(handlers, s.errorHandlers)
	s.handlerMu.Unlock()
	for _, h := range handlers {
		safeCall(func() { h(err) }, nil)
	}
}

// safeCall runs fn, recovering from panics so a throwing listener cannot abort
// the reconciliation tick that already committed its state.
func safeCall(fn func(), onErr func(error)) {
	defer func() {
		if r := recover(); r != nil {
			if onErr != nil {
				onErr(&core.BaseError{
					Code:    core.CodeAPIError,
					Message: "listener panic: " + toString(r),
				})
			}
		}
	}()
	fn()
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case error:
		return t.Error()
	default:
		return "unknown"
	}
}

// ---- Config ----

// SetStaticQris sets (or clears) the static QRIS used for dynamic QR generation.
func (s *Service) SetStaticQris(staticQris string) {
	s.staticQris = staticQris
}

// SetFeed replaces provider transport credentials without discarding replay guards.
func (s *Service) SetFeed(feed core.TransactionFeed) {
	s.feed = feed
}

// HasStaticQris reports whether a static QRIS is configured.
func (s *Service) HasStaticQris() bool { return s.staticQris != "" }

// IsRunning reports whether background reconciliation is scheduled.
func (s *Service) IsRunning() bool {
	select {
	case <-s.stopCh:
		return false
	default:
		return true
	}
}

// IsActive reports whether this service owns its scope's write lifecycle.
func (s *Service) IsActive() bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	return s.active
}

// ---- Lifecycle ----

// Activate re-enables a retained service after its scope becomes current again.
func (s *Service) Activate(token any) error {
	if err := s.assertLifecycleAccess(token); err != nil {
		return err
	}
	s.activeMu.Lock()
	if s.active {
		s.activeMu.Unlock()
		return nil
	}
	s.active = true
	s.activationEpoch++
	s.activeMu.Unlock()
	return nil
}

// Deactivate prevents new allocations and reconciliation, then waits for
// in-flight work to finish. It returns whether polling was running so the
// facade can decide to resume it later.
func (s *Service) Deactivate(token any) (wasRunning bool, err error) {
	if err := s.assertLifecycleAccess(token); err != nil {
		return false, err
	}

	wasRunning = s.IsRunning()

	s.activeMu.Lock()
	if s.active {
		s.active = false
		s.activationEpoch++
	}
	s.activeMu.Unlock()

	s.Stop()
	// Drain any in-flight tick.
	s.tickWG.Wait()
	// Barrier: wait for queued writes to finish.
	s.writeMu.Lock()
	s.writeMu.Unlock()

	return wasRunning, nil
}

func (s *Service) assertLifecycleAccess(token any) error {
	if s.lifecycleToken == nil || s.lifecycleToken == token {
		return nil
	}
	return core.NewConfigError("this PaymentService lifecycle is controlled by its provider", nil)
}

func (s *Service) assertActive(op string) error {
	s.activeMu.Lock()
	active := s.active
	s.activeMu.Unlock()
	if !active {
		return core.NewConfigError("cannot "+op+" with an inactive PaymentService", nil)
	}
	return nil
}

func (s *Service) currentEpoch() int64 {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	return s.activationEpoch
}

func (s *Service) isCurrentActivation(epoch int64) bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	return s.active && s.activationEpoch == epoch
}

// ownsPayment reports whether the payment belongs to this service's scope.
func (s *Service) ownsPayment(p *core.Payment) bool {
	if s.scope == nil {
		return p.Scope == nil
	}
	return p.Scope != nil && core.SameScope(*p.Scope, *s.scope)
}

// amountQuarantine is how long a freed unique amount stays unavailable. Two
// clock skews after the slot is freed, no transaction belonging to the old
// payment can still fall inside a new payment's window.
func (s *Service) amountQuarantine() time.Duration {
	return 2 * s.clockSkew
}

// ---- Store helpers ----

func (s *Service) listActive(ctx context.Context) ([]core.Payment, error) {
	active, err := s.store.ListActive(ctx, nil)
	if err != nil {
		return nil, err
	}

	if s.scope == nil {
		out := make([]core.Payment, 0, len(active))
		for _, p := range active {
			if p.Scope == nil {
				out = append(out, core.CopyPayment(p))
			}
		}
		return out, nil
	}

	// A scoped service must fail fast on ambiguous unscoped records.
	unscoped := 0
	for _, p := range active {
		if p.Scope == nil {
			unscoped++
		}
	}
	if unscoped > 0 {
		return nil, core.NewConfigError(
			"active payments without PaymentScope cannot be reconciled by a scoped service",
			map[string]any{"unscopedActiveCount": unscoped})
	}

	// Filter again defensively, even when the store supports scoped reads.
	out := make([]core.Payment, 0, len(active))
	for _, p := range active {
		if p.Scope != nil && core.SameScope(*p.Scope, *s.scope) {
			out = append(out, core.CopyPayment(p))
		}
	}
	return out, nil
}

// ---- Create ----

// CreatePayment creates a new pending payment with a unique amount and
// (optionally) a dynamic QRIS.
func (s *Service) CreatePayment(ctx context.Context, input CreatePaymentInput) (core.Payment, error) {
	if err := s.assertActive("create payments"); err != nil {
		return core.Payment{}, err
	}
	if input.Amount <= 0 {
		return core.Payment{}, core.NewConfigError("amount must be a positive integer (whole rupiah)", nil)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := s.assertActive("create payments"); err != nil {
		return core.Payment{}, err
	}

	active, err := s.listActive(ctx)
	if err != nil {
		return core.Payment{}, err
	}
	if err := s.assertActive("create payments"); err != nil {
		return core.Payment{}, err
	}

	now := time.Now()
	s.stateMu.Lock()
	for amount, freedAt := range s.recentlyFreed {
		if freedAt.Add(s.amountQuarantine()).Before(now) || freedAt.Add(s.amountQuarantine()).Equal(now) {
			delete(s.recentlyFreed, amount)
		}
	}
	taken := make(map[int64]bool, len(active)+len(s.recentlyFreed))
	for _, p := range active {
		taken[p.UniqueAmount] = true
	}
	for amount := range s.recentlyFreed {
		taken[amount] = true
	}
	s.stateMu.Unlock()

	uniqueOffset, err := s.allocator.Allocate(input.Amount, taken)
	if err != nil {
		return core.Payment{}, err
	}
	uniqueAmount := input.Amount + uniqueOffset
	expiry := input.ExpiresIn
	if expiry <= 0 {
		expiry = s.defaultExpiry
	}

	payment := core.Payment{
		ID:           utils.PaymentID(),
		Scope:        core.CopyPaymentScope(s.scope),
		BaseAmount:   input.Amount,
		UniqueOffset: uniqueOffset,
		UniqueAmount: uniqueAmount,
		Status:       core.PaymentPending,
		CreatedAt:    now,
		ExpiresAt:    now.Add(expiry),
		Reference:    input.Reference,
		Metadata:     input.Metadata,
	}

	if s.staticQris != "" {
		qr, qerr := qris.StaticToDynamicQris(s.staticQris, uniqueAmount)
		if qerr != nil {
			return core.Payment{}, qerr
		}
		payment.QRString = qr
	}

	if err := s.store.Create(ctx, payment); err != nil {
		return core.Payment{}, err
	}

	s.logger.Info("payment created", map[string]any{
		"id":           payment.ID,
		"uniqueAmount": uniqueAmount,
	})
	return core.CopyPayment(payment), nil
}

// ---- Transitions ----

// transitionIfPending moves a payment to a terminal status if and only if it is
// still pending, re-reading it inside the write queue so concurrent transitions
// can never overwrite each other's terminal state. Returns nil when the payment
// was missing, not owned, already terminal, or the activation epoch went stale.
func (s *Service) transitionIfPending(ctx context.Context, id string, status core.PaymentStatus, tx *core.MerchantTransaction, epoch int64) (*core.Payment, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if epoch >= 0 && !s.isCurrentActivation(epoch) {
		return nil, nil
	}

	stored, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if stored == nil || !s.ownsPayment(stored) || stored.Status != core.PaymentPending {
		return nil, nil
	}
	if epoch >= 0 && !s.isCurrentActivation(epoch) {
		return nil, nil
	}

	updated := core.CopyPayment(*stored)
	updated.Status = status
	if tx != nil {
		updated.Transaction = tx
	}

	if err := s.store.Update(ctx, updated); err != nil {
		return nil, err
	}

	// The freed amount enters quarantine, and a settling transaction is
	// remembered as consumed so it can never settle a second payment.
	s.stateMu.Lock()
	s.recentlyFreed[stored.UniqueAmount] = time.Now()
	if tx != nil {
		s.consumedTxIDs[tx.ID] = time.Now()
	}
	s.stateMu.Unlock()

	return &updated, nil
}

// CancelPayment cancels a pending payment, releasing its unique amount slot.
func (s *Service) CancelPayment(ctx context.Context, id string) (*core.Payment, error) {
	if err := s.assertActive("cancel payments"); err != nil {
		return nil, err
	}
	epoch := s.currentEpoch()
	cancelled, err := s.transitionIfPending(ctx, id, core.PaymentCancelled, nil, epoch)
	if err != nil {
		return nil, err
	}
	if cancelled != nil {
		return cancelled, nil
	}
	if !s.isCurrentActivation(epoch) {
		return nil, nil
	}
	return s.GetPayment(ctx, id)
}

// GetPayment returns a payment by id, or nil if it is not owned by this scope.
func (s *Service) GetPayment(ctx context.Context, id string) (*core.Payment, error) {
	p, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil || !s.ownsPayment(p) {
		return nil, nil
	}
	c := core.CopyPayment(*p)
	return &c, nil
}

// ---- Polling ----

// Start begins background polling. Safe to call once; repeated calls are no-ops.
func (s *Service) Start() {
	if err := s.assertActive("start payment polling"); err != nil {
		return
	}
	select {
	case <-s.stopCh:
		return
	default:
	}
	s.logger.Info("payment polling started", map[string]any{"intervalMs": s.pollInterval.Milliseconds()})
	s.tickWG.Add(1)
	go s.pollLoop()
}

func (s *Service) pollLoop() {
	defer s.tickWG.Done()
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if s.IsActive() {
				_, _ = s.Tick(context.Background())
			}
		}
	}
}

// Stop stops background polling. It does not wait for an in-flight tick.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.logger.Info("payment polling stopped", nil)
}

// Tick runs a single reconciliation pass: fetch recent transactions, settle
// matches, then expire what remains stale.
//
// Matching runs BEFORE expiry on purpose: the feed indexes transactions with a
// delay, so a buyer who paid inside the window can surface after expiresAt has
// passed. A payment is only marked expired once expiresAt + clockSkew has
// passed — the exact moment the matcher stops accepting transactions for it.
func (s *Service) Tick(ctx context.Context) (TickResult, error) {
	res := TickResult{}
	if err := s.assertActive("run payment reconciliation"); err != nil {
		return res, err
	}

	epoch := s.currentEpoch()
	s.tickWG.Add(1)
	defer s.tickWG.Done()

	pending, err := s.listActive(ctx)
	if err != nil {
		return res, err
	}
	if !s.isCurrentActivation(epoch) || len(pending) == 0 {
		return res, nil
	}

	// Forget consumed transaction ids the rolling feed window can no longer
	// return; the map stays bounded by settlement volume.
	s.stateMu.Lock()
	now := time.Now()
	for id, seenAt := range s.consumedTxIDs {
		if !seenAt.Add(core.DefaultTransactionLookback).After(now) {
			delete(s.consumedTxIDs, id)
		}
	}
	consumed := make(map[string]bool, len(s.consumedTxIDs))
	for id := range s.consumedTxIDs {
		consumed[id] = true
	}
	s.stateMu.Unlock()

	// A feed failure must not stop stale payments from expiring, so the fetch
	// error is parked and returned after the expiry sweep.
	var feedErr error
	transactions, err := s.fetchRecentTransactions(ctx, pending)
	if err != nil {
		feedErr = err
	}
	if !s.isCurrentActivation(epoch) {
		return res, nil
	}

	// Exclude already-consumed transactions.
	filtered := make([]core.MerchantTransaction, 0, len(transactions))
	for _, tx := range transactions {
		if !consumed[tx.ID] {
			filtered = append(filtered, tx)
		}
	}

	matches := Reconcile(pending, filtered, s.clockSkew)
	settledIDs := make(map[string]bool, len(matches))
	for _, m := range matches {
		settled, err := s.transitionIfPending(ctx, m.Payment.ID, core.PaymentPaid, &m.Transaction, epoch)
		if err != nil {
			feedErr = err
			continue
		}
		if settled == nil {
			continue
		}
		s.logger.Info("payment settled", map[string]any{
			"id":            settled.ID,
			"transactionId": m.Transaction.ID,
		})
		res.Paid = append(res.Paid, *settled)
		settledIDs[settled.ID] = true
		s.emitPaid(*settled)
	}

	// Expire stale payments that were not settled.
	for _, p := range pending {
		if settledIDs[p.ID] {
			continue
		}
		if !p.ExpiresAt.Add(s.clockSkew).After(time.Now()) {
			expired, err := s.transitionIfPending(ctx, p.ID, core.PaymentExpired, nil, epoch)
			if err != nil {
				feedErr = err
				continue
			}
			if expired == nil {
				continue
			}
			s.logger.Info("payment expired", map[string]any{"id": expired.ID})
			res.Expired = append(res.Expired, *expired)
			s.emitExpired(*expired)
		}
	}

	if !s.isCurrentActivation(epoch) {
		return res, nil
	}
	if feedErr != nil {
		s.logger.Error("poll tick failed", map[string]any{"message": feedErr.Error()})
		s.emitError(feedErr)
	}
	return res, feedErr
}

// fetchRecentTransactions fetches every transaction that could possibly settle
// one of the given pending payments. The window starts at the oldest active
// payment (minus clock skew) rather than a fixed 24 hours back, with 24h as an
// upper bound.
func (s *Service) fetchRecentTransactions(ctx context.Context, pending []core.Payment) ([]core.MerchantTransaction, error) {
	now := time.Now()
	oldest := now
	for _, p := range pending {
		if p.CreatedAt.Before(oldest) {
			oldest = p.CreatedAt
		}
	}
	start := now.Add(-core.DefaultTransactionLookback)
	if oldestStart := oldest.Add(-s.clockSkew); oldestStart.After(start) {
		start = oldestStart
	}

	result, err := s.feed.ListRecent(ctx, core.TransactionFeedQuery{
		Scope:     s.feedScope,
		StartTime: start,
		EndTime:   now,
		PageSize:  s.pageSize,
		MaxPages:  core.MaxTransactionPagesPerTick,
	})
	if err != nil {
		return nil, err
	}

	if result.Truncated {
		pages := result.PagesFetched
		if pages == 0 {
			pages = core.MaxTransactionPagesPerTick
		}
		s.logger.Warn(
			"transaction page cap reached; oldest transactions in the window were not scanned this tick",
			map[string]any{
				"provider": s.feedScope.Provider,
				"pages":    pages,
				"pageSize": s.pageSize,
			})
	}

	return result.Transactions, nil
}
