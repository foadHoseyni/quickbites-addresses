package addresses

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"quickbites/backend/internal/apperrors"
)

func sampleInput(label string) AddAddressInput {
	return AddAddressInput{Label: label, AddressLine1: "Main St 1", City: "Utrecht", ZipCode: "3511AA"}
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *AppError, got %v", err)
	}
	return appErr.Status
}

func TestFirstAddressBecomesDefault(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	user := primitive.NewObjectID()

	first, _ := svc.AddAddress(context.Background(), user, sampleInput("Home"))
	second, _ := svc.AddAddress(context.Background(), user, sampleInput("Work"))

	if !first.IsDefault {
		t.Error("first address should be the default")
	}
	if second.IsDefault {
		t.Error("second address should not be the default")
	}
}

func TestUpdatePersists(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	user := primitive.NewObjectID()
	addr, _ := svc.AddAddress(context.Background(), user, sampleInput("Home"))

	_, err := svc.UpdateAddressPartial(context.Background(), addr.ID, user, bson.M{"city": "Amsterdam"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.FindByIDAndUser(context.Background(), addr.ID, user)
	if got.City != "Amsterdam" {
		t.Errorf("update did not persist: city = %q", got.City)
	}
}

func TestDeleteDefaultPromotesAnother(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	user := primitive.NewObjectID()
	home, _ := svc.AddAddress(context.Background(), user, sampleInput("Home")) // default
	work, _ := svc.AddAddress(context.Background(), user, sampleInput("Work"))

	if err := svc.DeleteAddress(context.Background(), home.ID, user); err != nil {
		t.Fatal(err)
	}
	if gone, _ := store.FindByIDAndUser(context.Background(), home.ID, user); gone != nil {
		t.Error("delete did not persist")
	}
	remaining, _ := store.FindByIDAndUser(context.Background(), work.ID, user)
	if !remaining.IsDefault {
		t.Error("remaining address should have been promoted to default")
	}
}

func TestSetDefaultKeepsExactlyOne(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	user := primitive.NewObjectID()
	svc.AddAddress(context.Background(), user, sampleInput("Home"))
	work, _ := svc.AddAddress(context.Background(), user, sampleInput("Work"))

	if _, err := svc.SetDefault(context.Background(), work.ID, user); err != nil {
		t.Fatal(err)
	}
	if n := store.defaults(user); n != 1 {
		t.Errorf("expected exactly 1 default, got %d", n)
	}
	got, _ := store.FindByIDAndUser(context.Background(), work.ID, user)
	if !got.IsDefault {
		t.Error("chosen address should be the default")
	}
}

// Another user's address must look exactly like a missing one: 404.
func TestOtherUsersAddressIsNotFound(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	owner := primitive.NewObjectID()
	attacker := primitive.NewObjectID()
	addr, _ := svc.AddAddress(context.Background(), owner, sampleInput("Home"))

	_, err := svc.UpdateAddressPartial(context.Background(), addr.ID, attacker, bson.M{"city": "X"})
	if statusOf(t, err) != http.StatusNotFound {
		t.Error("update of another user's address should be 404")
	}
	if statusOf(t, svc.DeleteAddress(context.Background(), addr.ID, attacker)) != http.StatusNotFound {
		t.Error("delete of another user's address should be 404")
	}
	_, err = svc.SetDefault(context.Background(), addr.ID, attacker)
	if statusOf(t, err) != http.StatusNotFound {
		t.Error("set-default of another user's address should be 404")
	}
	if still, _ := store.FindByIDAndUser(context.Background(), addr.ID, owner); still == nil || still.City != "Utrecht" {
		t.Error("owner's address must be untouched")
	}
}
