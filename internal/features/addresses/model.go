package addresses

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Address is one delivery address belonging to one user.
type Address struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID       primitive.ObjectID `bson:"user_id"       json:"userId"`
	Label        string             `bson:"label"         json:"label"`
	AddressLine1 string             `bson:"address_line1" json:"addressLine1"`
	AddressLine2 string             `bson:"address_line2" json:"addressLine2"`
	City         string             `bson:"city"          json:"city"`
	State        string             `bson:"state"         json:"state"`
	ZipCode      string             `bson:"zip_code"      json:"zipCode"`
	IsDefault    bool               `bson:"is_default"    json:"isDefault"`
	CreatedAt    time.Time          `bson:"created_at"    json:"createdAt"`
	UpdatedAt    time.Time          `bson:"updated_at"    json:"updatedAt"`
}

// AddAddressInput is what the service needs to create an address.
// There is deliberately NO UserID here: the user comes from the session.
type AddAddressInput struct {
	Label        string
	AddressLine1 string
	AddressLine2 string
	City         string
	State        string
	ZipCode      string
}
