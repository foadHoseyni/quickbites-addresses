package promocodes

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"quickbites/backend/internal/apperrors"
)

// Reasons returned to the user when a code doesn't apply.
const (
	ReasonNotFound     = "promo code not found"
	ReasonInactive     = "the promotion code is inactive"
	ReasonExpired      = "the promotion code has expired"
	ReasonLimitReached = "the promotion code has reached its usage limit"
	ReasonAlreadyUsed  = "you have already used this promotion code"
)

// Store is what the service needs from storage. *Repository implements it
// against MongoDB; tests use an in-memory fake.
type Store interface {
	Create(ctx context.Context, p *PromoCode) error
	FindByCode(ctx context.Context, code string) (*PromoCode, error)
	List(ctx context.Context) ([]PromoCode, error)
	SetActive(ctx context.Context, code string, active bool) (*PromoCode, error)
	UserUses(ctx context.Context, promoID, userID primitive.ObjectID) (int64, error)
	ClaimUser(ctx context.Context, promoID, userID primitive.ObjectID, maxPerUser int64) (bool, error)
	ReleaseUser(ctx context.Context, promoID, userID primitive.ObjectID) error
	ClaimGlobal(ctx context.Context, promoID primitive.ObjectID, now time.Time) (bool, error)
	ReleaseGlobal(ctx context.Context, promoID primitive.ObjectID) error
}

var _ Store = (*Repository)(nil)

// Service holds the promo code rules. The clock is injectable so expiry
// can be tested precisely.
type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

var codePattern = regexp.MustCompile(`^[A-Z0-9_]{3,32}$`)

// NormalizeCode makes codes case-insensitive: they are stored and looked up
// in upper case.
func NormalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// ---------- Admin ----------

func (s *Service) CreatePromo(ctx context.Context, in CreatePromoInput) (*PromoCode, error) {
	code := NormalizeCode(in.Code)
	switch {
	case !codePattern.MatchString(code):
		return nil, apperrors.NewBadRequest("code must be 3-32 characters: letters, digits or underscore")
	case in.DiscountType == DiscountTypePercentage && (in.DiscountValue < 1 || in.DiscountValue > 100):
		return nil, apperrors.NewBadRequest("percentage discount must be between 1 and 100")
	case in.DiscountType == DiscountTypeFixed && in.DiscountValue < 1:
		return nil, apperrors.NewBadRequest("fixed discount must be at least 1 cent")
	case in.DiscountType != DiscountTypePercentage && in.DiscountType != DiscountTypeFixed:
		return nil, apperrors.NewBadRequest("discountType must be 'percentage' or 'fixed'")
	case in.MinimumOrderCents < 0 || in.MaxUses < 0 || in.MaxUsesPerUser < 0:
		return nil, apperrors.NewBadRequest("minimum order and usage limits cannot be negative")
	case in.ExpiresAt != nil && !in.ExpiresAt.After(s.now()):
		return nil, apperrors.NewBadRequest("expiresAt must be in the future")
	}

	p := &PromoCode{
		Code:              code,
		Description:       in.Description,
		DiscountType:      in.DiscountType,
		DiscountValue:     in.DiscountValue,
		MinimumOrderCents: in.MinimumOrderCents,
		MaxUses:           in.MaxUses,
		MaxUsesPerUser:    in.MaxUsesPerUser,
		IsActive:          true,
		ExpiresAt:         in.ExpiresAt,
	}
	if err := s.store.Create(ctx, p); err != nil {
		if errors.Is(err, ErrDuplicateCode) {
			return nil, apperrors.NewConflict("a promo code with this code already exists")
		}
		return nil, apperrors.NewServerError("Failed to create promo code")
	}
	return p, nil
}

func (s *Service) ListPromos(ctx context.Context) ([]PromoCode, error) {
	list, err := s.store.List(ctx)
	if err != nil {
		return nil, apperrors.NewServerError("Failed to list promo codes")
	}
	return list, nil
}

func (s *Service) SetActive(ctx context.Context, code string, active bool) (*PromoCode, error) {
	p, err := s.store.SetActive(ctx, NormalizeCode(code), active)
	if err != nil {
		return nil, apperrors.NewServerError("Failed to update promo code")
	}
	if p == nil {
		return nil, apperrors.NewNotFound("Promo code not found")
	}
	return p, nil
}

// ---------- Validation ----------

// Validate checks whether a code applies to an order of subtotalCents for
// this user, and how much discount it gives. It does not use the code up.
//
// A code that doesn't apply returns Valid=false with a reason and a nil
// error; a Go error is only returned for real failures (e.g. database down).
// The order of checks decides which reason the user sees.
func (s *Service) Validate(ctx context.Context, code string, subtotalCents int64,
	userID primitive.ObjectID) (ValidationResult, *PromoCode, error) {

	normalized := NormalizeCode(code)
	var promo *PromoCode

	fail := func(reason string) (ValidationResult, *PromoCode, error) {
		return ValidationResult{
			Valid:              false,
			Code:               normalized,
			Reason:             reason,
			Message:            reason,
			FinalSubtotalCents: subtotalCents,
		}, promo, nil
	}

	if subtotalCents < 0 {
		return ValidationResult{}, nil, apperrors.NewBadRequest("subtotal cannot be negative")
	}

	promo, err := s.store.FindByCode(ctx, normalized)
	if err != nil {
		return ValidationResult{}, nil, err
	}
	if promo == nil {
		return fail(ReasonNotFound)
	}
	if !promo.IsActive {
		return fail(ReasonInactive)
	}
	if promo.ExpiresAt != nil && !s.now().Before(*promo.ExpiresAt) {
		return fail(ReasonExpired)
	}
	if subtotalCents < promo.MinimumOrderCents {
		return fail(fmt.Sprintf("minimum order of %s required for this promo code",
			formatCents(promo.MinimumOrderCents)))
	}
	if promo.MaxUses > 0 && promo.CurrentUses >= promo.MaxUses {
		return fail(ReasonLimitReached)
	}
	if promo.MaxUsesPerUser > 0 {
		uses, err := s.store.UserUses(ctx, promo.ID, userID)
		if err != nil {
			return ValidationResult{}, nil, err
		}
		if uses >= promo.MaxUsesPerUser {
			return fail(ReasonAlreadyUsed)
		}
	}

	discount := CalculateDiscount(promo, subtotalCents)
	return ValidationResult{
		Valid:              true,
		Code:               promo.Code,
		DiscountType:       promo.DiscountType,
		DiscountValue:      promo.DiscountValue,
		DiscountCents:      discount,
		FinalSubtotalCents: subtotalCents - discount,
		Message:            successMessage(promo),
	}, promo, nil
}

// CalculateDiscount returns the discount in cents, never more than the
// subtotal. Percentages round half up to the nearest cent.
func CalculateDiscount(p *PromoCode, subtotalCents int64) int64 {
	var d int64
	switch p.DiscountType {
	case DiscountTypePercentage:
		d = (subtotalCents*p.DiscountValue + 50) / 100
	case DiscountTypeFixed:
		d = p.DiscountValue
	}
	if d > subtotalCents {
		d = subtotalCents
	}
	if d < 0 {
		d = 0
	}
	return d
}

// ---------- Redemption (used by checkout) ----------

// Redeem validates the code and atomically uses it up for this user.
// Checkout calls it before creating the order; if creating the order then
// fails, checkout must call Release to give the use back.
//
// Validate alone is a check-then-act race: two requests could both see
// "9 of 10 uses" and both proceed. Redeem closes that gap with conditional
// updates in the database (ClaimUser, ClaimGlobal), so the limits hold even
// under concurrent checkouts.
func (s *Service) Redeem(ctx context.Context, code string, subtotalCents int64,
	userID primitive.ObjectID) (*Redemption, error) {

	res, promo, err := s.Validate(ctx, code, subtotalCents, userID)
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			return nil, appErr
		}
		return nil, apperrors.NewServerError("Failed to validate promo code")
	}
	if !res.Valid {
		return nil, apperrors.NewBadRequest(res.Reason)
	}

	ok, err := s.store.ClaimUser(ctx, promo.ID, userID, promo.MaxUsesPerUser)
	if err != nil {
		return nil, apperrors.NewServerError("Failed to redeem promo code")
	}
	if !ok {
		return nil, apperrors.NewBadRequest(ReasonAlreadyUsed)
	}

	ok, err = s.store.ClaimGlobal(ctx, promo.ID, s.now())
	if err != nil || !ok {
		// Compensate: give the user's use back before reporting the failure.
		_ = s.store.ReleaseUser(ctx, promo.ID, userID)
		if err != nil {
			return nil, apperrors.NewServerError("Failed to redeem promo code")
		}
		return nil, apperrors.NewBadRequest(ReasonLimitReached)
	}

	return &Redemption{PromoID: promo.ID, Code: promo.Code, DiscountCents: res.DiscountCents}, nil
}

// Release undoes a Redeem, e.g. when creating the order failed afterwards.
func (s *Service) Release(ctx context.Context, r *Redemption, userID primitive.ObjectID) error {
	if r == nil {
		return nil
	}
	errGlobal := s.store.ReleaseGlobal(ctx, r.PromoID)
	errUser := s.store.ReleaseUser(ctx, r.PromoID, userID)
	return errors.Join(errGlobal, errUser)
}

// ---------- helpers ----------

func successMessage(p *PromoCode) string {
	if p.DiscountType == DiscountTypePercentage {
		return fmt.Sprintf("Promo code applied: %d%% off", p.DiscountValue)
	}
	return fmt.Sprintf("Promo code applied: %s off", formatCents(p.DiscountValue))
}

func formatCents(c int64) string {
	return fmt.Sprintf("%d.%02d", c/100, c%100)
}
