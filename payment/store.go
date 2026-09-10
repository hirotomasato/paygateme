package payment

import (
	"context"
	"sync"

	"github.com/hirotomasato/paygateme/core"
)

// InMemoryStore is the default in-memory PaymentStore. It is safe for
// concurrent use and suitable for single-process use and tests. Multi-process
// deployments must provide a durable store implementing core.PaymentStore.
type InMemoryStore struct {
	mu       sync.RWMutex
	payments map[string]core.Payment
}

// NewInMemoryStore creates an empty in-memory store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{payments: make(map[string]core.Payment)}
}

func (s *InMemoryStore) Create(_ context.Context, payment core.Payment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payments[payment.ID] = core.CopyPayment(payment)
	return nil
}

func (s *InMemoryStore) Update(_ context.Context, payment core.Payment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payments[payment.ID] = core.CopyPayment(payment)
	return nil
}

func (s *InMemoryStore) Get(_ context.Context, id string) (*core.Payment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.payments[id]
	if !ok {
		return nil, nil
	}
	c := core.CopyPayment(p)
	return &c, nil
}

func (s *InMemoryStore) ListActive(_ context.Context, scope *core.PaymentScope) ([]core.Payment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]core.Payment, 0)
	for _, p := range s.payments {
		if p.Status != core.PaymentPending {
			continue
		}
		if scope != nil {
			if p.Scope == nil || !core.SameScope(*p.Scope, *scope) {
				continue
			}
		}
		out = append(out, core.CopyPayment(p))
	}
	return out, nil
}
