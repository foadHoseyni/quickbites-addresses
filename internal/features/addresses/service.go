package addresses

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"quickbites/backend/internal/apperrors"
)

// Store is what the service needs from storage. *Repository implements it
// against MongoDB; tests use an in-memory fake. Depending on an interface
// keeps the business rules testable without a database.
type Store interface {
	Create(ctx context.Context, a *Address) error
	FindByUser(ctx context.Context, userID primitive.ObjectID) ([]Address, error)
	FindByIDAndUser(ctx context.Context, id, userID primitive.ObjectID) (*Address, error)
	CountByUser(ctx context.Context, userID primitive.ObjectID) (int64, error)
	UpdateFields(ctx context.Context, id primitive.ObjectID, fields bson.M) (*Address, error)
	Delete(ctx context.Context, id primitive.ObjectID) error
	SetDefault(ctx context.Context, id primitive.ObjectID, isDefault bool) error
	UnsetAllDefaults(ctx context.Context, userID primitive.ObjectID) error
	FindNextAddress(ctx context.Context, userID primitive.ObjectID) (*Address, error)
}

// Compile-time check that the Mongo repository satisfies Store.
var _ Store = (*Repository)(nil)

// Service holds the business rules:
//   - a user only ever touches their own addresses
//   - a user with addresses always has exactly one default
type Service struct {
	repo Store
}

func NewService(repo Store) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListAddresses(ctx context.Context, userID primitive.ObjectID) ([]Address, error) {
	list, err := s.repo.FindByUser(ctx, userID)
	if err != nil {
		return nil, apperrors.NewServerError("Failed to fetch addresses")
	}
	return list, nil
}

// AddAddress creates an address. FIX: the user's first address becomes the
// default automatically (this is what the unused CountByUser was for).
func (s *Service) AddAddress(ctx context.Context, userID primitive.ObjectID, in AddAddressInput) (*Address, error) {
	count, err := s.repo.CountByUser(ctx, userID)
	if err != nil {
		return nil, apperrors.NewServerError("Failed to create address")
	}

	addr := &Address{
		UserID:       userID,
		Label:        in.Label,
		AddressLine1: in.AddressLine1,
		AddressLine2: in.AddressLine2,
		City:         in.City,
		State:        in.State,
		ZipCode:      in.ZipCode,
		IsDefault:    count == 0,
	}
	if err := s.repo.Create(ctx, addr); err != nil {
		return nil, apperrors.NewServerError("Failed to create address")
	}
	return addr, nil
}

// UpdateAddressPartial. FIX: the original only looked the address up and
// returned it; it never called UpdateFields, so edits never persisted.
func (s *Service) UpdateAddressPartial(ctx context.Context,
	addrID, userID primitive.ObjectID, updates bson.M) (*Address, error) {

	existing, err := s.repo.FindByIDAndUser(ctx, addrID, userID)
	if err != nil {
		return nil, apperrors.NewServerError("Failed to fetch address")
	}
	if existing == nil {
		return nil, apperrors.NewNotFound("Address not found")
	}
	if len(updates) == 0 {
		return existing, nil
	}

	updated, err := s.repo.UpdateFields(ctx, addrID, updates)
	if err != nil {
		return nil, apperrors.NewServerError("Failed to update address")
	}
	if updated == nil {
		return nil, apperrors.NewNotFound("Address not found")
	}
	return updated, nil
}

// DeleteAddress. FIX: the original never called Delete. If the default is
// deleted, the most recent remaining address is promoted to default.
func (s *Service) DeleteAddress(ctx context.Context, addrID, userID primitive.ObjectID) error {
	existing, err := s.repo.FindByIDAndUser(ctx, addrID, userID)
	if err != nil {
		return apperrors.NewServerError("Failed to fetch address")
	}
	if existing == nil {
		return apperrors.NewNotFound("Address not found")
	}

	if err := s.repo.Delete(ctx, addrID); err != nil {
		return apperrors.NewServerError("Failed to delete address")
	}

	if existing.IsDefault {
		next, err := s.repo.FindNextAddress(ctx, userID)
		if err != nil {
			return apperrors.NewServerError("Failed to update default address")
		}
		if next != nil {
			if err := s.repo.SetDefault(ctx, next.ID, true); err != nil {
				return apperrors.NewServerError("Failed to update default address")
			}
		}
	}
	return nil
}

// SetDefault. FIX: the original never wrote anything. Unset all of the
// user's defaults first, then set the chosen one.
// (Two writes = not atomic; see the prep guide for the unique-index fix.)
func (s *Service) SetDefault(ctx context.Context, addrID, userID primitive.ObjectID) (*Address, error) {
	addr, err := s.repo.FindByIDAndUser(ctx, addrID, userID)
	if err != nil {
		return nil, apperrors.NewServerError("Failed to fetch address")
	}
	if addr == nil {
		return nil, apperrors.NewNotFound("Address not found")
	}

	if err := s.repo.UnsetAllDefaults(ctx, userID); err != nil {
		return nil, apperrors.NewServerError("Failed to update default address")
	}
	if err := s.repo.SetDefault(ctx, addrID, true); err != nil {
		return nil, apperrors.NewServerError("Failed to set default address")
	}
	addr.IsDefault = true
	return addr, nil
}
