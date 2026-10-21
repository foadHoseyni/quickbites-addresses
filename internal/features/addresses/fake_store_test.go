package addresses

import (
	"context"
	"sort"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// fakeStore is an in-memory Store used by the tests. It mimics the Mongo
// repository's behaviour, including returning (nil, nil) for "not found".
type fakeStore struct {
	mu    sync.Mutex
	items map[primitive.ObjectID]*Address
	clock time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{items: map[primitive.ObjectID]*Address{}, clock: time.Unix(1_700_000_000, 0)}
}

// tick gives every created address a distinct, increasing timestamp.
func (f *fakeStore) tick() time.Time {
	f.clock = f.clock.Add(time.Second)
	return f.clock
}

func (f *fakeStore) Create(_ context.Context, a *Address) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a.ID = primitive.NewObjectID()
	a.CreatedAt = f.tick()
	a.UpdatedAt = a.CreatedAt
	cp := *a
	f.items[a.ID] = &cp
	return nil
}

func (f *fakeStore) FindByUser(_ context.Context, userID primitive.ObjectID) ([]Address, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []Address{}
	for _, a := range f.items {
		if a.UserID == userID {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeStore) FindByIDAndUser(_ context.Context, id, userID primitive.ObjectID) (*Address, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.items[id]
	if !ok || a.UserID != userID {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (f *fakeStore) CountByUser(_ context.Context, userID primitive.ObjectID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, a := range f.items {
		if a.UserID == userID {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) UpdateFields(_ context.Context, id primitive.ObjectID, fields bson.M) (*Address, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.items[id]
	if !ok {
		return nil, nil
	}
	for k, v := range fields {
		s, _ := v.(string)
		switch k {
		case "label":
			a.Label = s
		case "address_line1":
			a.AddressLine1 = s
		case "address_line2":
			a.AddressLine2 = s
		case "city":
			a.City = s
		case "state":
			a.State = s
		case "zip_code":
			a.ZipCode = s
		}
	}
	cp := *a
	return &cp, nil
}

func (f *fakeStore) Delete(_ context.Context, id primitive.ObjectID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items, id)
	return nil
}

func (f *fakeStore) SetDefault(_ context.Context, id primitive.ObjectID, isDefault bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.items[id]; ok {
		a.IsDefault = isDefault
	}
	return nil
}

func (f *fakeStore) UnsetAllDefaults(_ context.Context, userID primitive.ObjectID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.items {
		if a.UserID == userID {
			a.IsDefault = false
		}
	}
	return nil
}

func (f *fakeStore) FindNextAddress(ctx context.Context, userID primitive.ObjectID) (*Address, error) {
	list, _ := f.FindByUser(ctx, userID)
	if len(list) == 0 {
		return nil, nil
	}
	return &list[0], nil
}

// defaults returns how many default addresses a user has (should be 0 or 1).
func (f *fakeStore) defaults(userID primitive.ObjectID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.items {
		if a.UserID == userID && a.IsDefault {
			n++
		}
	}
	return n
}
