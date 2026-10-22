package promocodes

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"quickbites/backend/internal/apperrors"
)

var fixedNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func newTestService() (*Service, *fakeStore) {
	store := newFakeStore()
	svc := NewService(store)
	svc.now = func() time.Time { return fixedNow }
	return svc, store
}

func mustCreate(t *testing.T, svc *Service, in CreatePromoInput) *PromoCode {
	t.Helper()
	p, err := svc.CreatePromo(context.Background(), in)
	if err != nil {
		t.Fatalf("create promo: %v", err)
	}
	return p
}

func statusOf(err error) int {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Status
	}
	return 0
}

func TestCalculateDiscount(t *testing.T) {
	cases := []struct {
		name     string
		typ      string
		value    int64
		subtotal int64
		want     int64
	}{
		{"10% of 25.98 rounds half up to 2.60", DiscountTypePercentage, 10, 2598, 260},
		{"15% of 10.03 = 1.5045 rounds to 1.50", DiscountTypePercentage, 15, 1003, 150},
		{"fixed 5.00 off", DiscountTypeFixed, 500, 2000, 500},
		{"fixed is capped at the subtotal", DiscountTypeFixed, 5000, 1200, 1200},
		{"100% gives the whole subtotal", DiscountTypePercentage, 100, 999, 999},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CalculateDiscount(&PromoCode{DiscountType: c.typ, DiscountValue: c.value}, c.subtotal)
			if got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestValidateRules(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	user := primitive.NewObjectID()
	past := fixedNow.Add(-time.Hour)
	future := fixedNow.Add(time.Hour)

	mustCreate(t, svc, CreatePromoInput{Code: "SAVE10", DiscountType: DiscountTypePercentage, DiscountValue: 10, MinimumOrderCents: 1500, ExpiresAt: &future})
	mustCreate(t, svc, CreatePromoInput{Code: "OFF", DiscountType: DiscountTypeFixed, DiscountValue: 300})
	svc.SetActive(ctx, "OFF", false)

	// Expired: create while "now" is earlier, then validate at fixedNow.
	svc.now = func() time.Time { return past.Add(-time.Hour) }
	mustCreate(t, svc, CreatePromoInput{Code: "OLD", DiscountType: DiscountTypeFixed, DiscountValue: 100, ExpiresAt: &past})
	svc.now = func() time.Time { return fixedNow }

	cases := []struct {
		code     string
		subtotal int64
		valid    bool
		reason   string
	}{
		{"save10", 2598, true, ""}, // lowercase works
		{"NOPE", 2598, false, ReasonNotFound},
		{"OFF", 2598, false, ReasonInactive},
		{"OLD", 2598, false, ReasonExpired},
		{"SAVE10", 1000, false, "minimum order of 15.00 required for this promo code"},
	}
	for _, c := range cases {
		res, _, err := svc.Validate(ctx, c.code, c.subtotal, user)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.code, err)
		}
		if res.Valid != c.valid || res.Reason != c.reason {
			t.Errorf("%s: got valid=%v reason=%q, want valid=%v reason=%q", c.code, res.Valid, res.Reason, c.valid, c.reason)
		}
	}

	res, _, _ := svc.Validate(ctx, "SAVE10", 2598, user)
	if res.DiscountCents != 260 || res.FinalSubtotalCents != 2338 {
		t.Errorf("discount: got %d / final %d, want 260 / 2338", res.DiscountCents, res.FinalSubtotalCents)
	}
}

func TestCreatePromoValidation(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	bad := []CreatePromoInput{
		{Code: "x", DiscountType: DiscountTypeFixed, DiscountValue: 100},        // too short
		{Code: "PCT", DiscountType: DiscountTypePercentage, DiscountValue: 150}, // > 100%
		{Code: "ZERO", DiscountType: DiscountTypeFixed, DiscountValue: 0},       // no discount
		{Code: "WHAT", DiscountType: "bogo", DiscountValue: 1},                  // unknown type
		{Code: "NEG", DiscountType: DiscountTypeFixed, DiscountValue: 1, MaxUses: -1},
	}
	for _, in := range bad {
		if _, err := svc.CreatePromo(ctx, in); statusOf(err) != http.StatusBadRequest {
			t.Errorf("%+v: expected 400, got %v", in, err)
		}
	}

	mustCreate(t, svc, CreatePromoInput{Code: "dup", DiscountType: DiscountTypeFixed, DiscountValue: 100})
	if _, err := svc.CreatePromo(ctx, CreatePromoInput{Code: "DUP", DiscountType: DiscountTypeFixed, DiscountValue: 100}); statusOf(err) != http.StatusConflict {
		t.Errorf("duplicate code (case-insensitive): expected 409, got %v", err)
	}
}

func TestRedeemRecordsUsageAndBlocksSecondUse(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	user := primitive.NewObjectID()
	mustCreate(t, svc, CreatePromoInput{Code: "ONCE", DiscountType: DiscountTypeFixed, DiscountValue: 200, MaxUsesPerUser: 1})

	r, err := svc.Redeem(ctx, "once", 1000, user)
	if err != nil || r.DiscountCents != 200 {
		t.Fatalf("first redeem: %v %+v", err, r)
	}
	if store.uses("ONCE") != 1 {
		t.Fatalf("current uses should be 1, got %d", store.uses("ONCE"))
	}

	res, _, _ := svc.Validate(ctx, "ONCE", 1000, user)
	if res.Valid || res.Reason != ReasonAlreadyUsed {
		t.Errorf("validate after use: got %+v", res)
	}
	if _, err := svc.Redeem(ctx, "ONCE", 1000, user); statusOf(err) != http.StatusBadRequest {
		t.Errorf("second redeem should be rejected, got %v", err)
	}
}

func TestReleaseGivesUseBack(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	user := primitive.NewObjectID()
	mustCreate(t, svc, CreatePromoInput{Code: "LAST", DiscountType: DiscountTypeFixed, DiscountValue: 100, MaxUses: 1, MaxUsesPerUser: 1})

	r, err := svc.Redeem(ctx, "LAST", 1000, user)
	if err != nil {
		t.Fatal(err)
	}
	// Checkout failed after redeeming: give the use back.
	if err := svc.Release(ctx, r, user); err != nil {
		t.Fatal(err)
	}
	if store.uses("LAST") != 0 {
		t.Fatalf("uses should be back to 0, got %d", store.uses("LAST"))
	}
	if _, err := svc.Redeem(ctx, "LAST", 1000, user); err != nil {
		t.Errorf("redeem after release should work, got %v", err)
	}
}

// The check-then-act race: many users try to take the last uses at once.
// Exactly MaxUses redemptions may succeed.
func TestConcurrentRedeemRespectsGlobalLimit(t *testing.T) {
	svc, store := newTestService()
	mustCreate(t, svc, CreatePromoInput{Code: "FLASH", DiscountType: DiscountTypePercentage, DiscountValue: 20, MaxUses: 5})

	var wg sync.WaitGroup
	var ok int64
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Redeem(context.Background(), "FLASH", 1000, primitive.NewObjectID()); err == nil {
				atomic.AddInt64(&ok, 1)
			}
		}()
	}
	wg.Wait()

	if ok != 5 || store.uses("FLASH") != 5 {
		t.Fatalf("expected exactly 5 redemptions, got %d (stored uses %d)", ok, store.uses("FLASH"))
	}
}

// One user double-clicking checkout must not use a once-per-user code twice.
func TestConcurrentRedeemRespectsPerUserLimit(t *testing.T) {
	svc, store := newTestService()
	user := primitive.NewObjectID()
	mustCreate(t, svc, CreatePromoInput{Code: "WELCOME", DiscountType: DiscountTypeFixed, DiscountValue: 500, MaxUsesPerUser: 1})

	var wg sync.WaitGroup
	var ok int64
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Redeem(context.Background(), "WELCOME", 2000, user); err == nil {
				atomic.AddInt64(&ok, 1)
			}
		}()
	}
	wg.Wait()

	if ok != 1 || store.uses("WELCOME") != 1 {
		t.Fatalf("expected exactly 1 redemption, got %d (stored uses %d)", ok, store.uses("WELCOME"))
	}
}
