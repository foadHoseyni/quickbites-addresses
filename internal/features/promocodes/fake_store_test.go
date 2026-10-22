package promocodes

import (
	"context"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// fakeStore is an in-memory Store. A single mutex makes every method
// atomic, which mirrors what the conditional MongoDB updates guarantee.
type fakeStore struct {
	mu    sync.Mutex
	codes map[string]*PromoCode
	usage map[[2]primitive.ObjectID]int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{codes: map[string]*PromoCode{}, usage: map[[2]primitive.ObjectID]int64{}}
}

func (f *fakeStore) byID(id primitive.ObjectID) *PromoCode {
	for _, p := range f.codes {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (f *fakeStore) Create(_ context.Context, p *PromoCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.codes[p.Code]; exists {
		return ErrDuplicateCode
	}
	p.ID = primitive.NewObjectID()
	cp := *p
	f.codes[p.Code] = &cp
	return nil
}

func (f *fakeStore) FindByCode(_ context.Context, code string) (*PromoCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.codes[code]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (f *fakeStore) List(_ context.Context) ([]PromoCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []PromoCode{}
	for _, p := range f.codes {
		out = append(out, *p)
	}
	return out, nil
}

func (f *fakeStore) SetActive(_ context.Context, code string, active bool) (*PromoCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.codes[code]
	if !ok {
		return nil, nil
	}
	p.IsActive = active
	cp := *p
	return &cp, nil
}

func (f *fakeStore) UserUses(_ context.Context, promoID, userID primitive.ObjectID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usage[[2]primitive.ObjectID{promoID, userID}], nil
}

func (f *fakeStore) ClaimUser(_ context.Context, promoID, userID primitive.ObjectID, max int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]primitive.ObjectID{promoID, userID}
	if max > 0 && f.usage[key] >= max {
		return false, nil
	}
	f.usage[key]++
	return true, nil
}

func (f *fakeStore) ReleaseUser(_ context.Context, promoID, userID primitive.ObjectID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]primitive.ObjectID{promoID, userID}
	if f.usage[key] > 0 {
		f.usage[key]--
	}
	return nil
}

func (f *fakeStore) ClaimGlobal(_ context.Context, promoID primitive.ObjectID, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.byID(promoID)
	if p == nil || !p.IsActive {
		return false, nil
	}
	if p.ExpiresAt != nil && !now.Before(*p.ExpiresAt) {
		return false, nil
	}
	if p.MaxUses > 0 && p.CurrentUses >= p.MaxUses {
		return false, nil
	}
	p.CurrentUses++
	return true, nil
}

func (f *fakeStore) ReleaseGlobal(_ context.Context, promoID primitive.ObjectID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p := f.byID(promoID); p != nil && p.CurrentUses > 0 {
		p.CurrentUses--
	}
	return nil
}

func (f *fakeStore) uses(code string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.codes[code].CurrentUses
}
