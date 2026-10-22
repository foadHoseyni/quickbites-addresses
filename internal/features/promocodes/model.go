// Package promocodes implements promotion codes: admins create them, users
// check them before checkout, and checkout redeems them atomically.
//
// All money amounts are integer minor units (cents), never floats.
package promocodes

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	DiscountTypePercentage = "percentage" // DiscountValue is a whole percent, 1-100
	DiscountTypeFixed      = "fixed"      // DiscountValue is an amount in cents
)

// PromoCode is one promotion code.
type PromoCode struct {
	ID                primitive.ObjectID `bson:"_id,omitempty"        json:"id"`
	Code              string             `bson:"code"                 json:"code"`
	Description       string             `bson:"description"          json:"description"`
	DiscountType      string             `bson:"discount_type"        json:"discountType"`
	DiscountValue     int64              `bson:"discount_value"       json:"discountValue"`
	MinimumOrderCents int64              `bson:"minimum_order_cents"  json:"minimumOrderCents"`
	MaxUses           int64              `bson:"max_uses"             json:"maxUses"`        // 0 = unlimited
	MaxUsesPerUser    int64              `bson:"max_uses_per_user"    json:"maxUsesPerUser"` // 0 = unlimited
	CurrentUses       int64              `bson:"current_uses"         json:"currentUses"`
	IsActive          bool               `bson:"is_active"            json:"isActive"`
	ExpiresAt         *time.Time         `bson:"expires_at,omitempty" json:"expiresAt,omitempty"`
	CreatedAt         time.Time          `bson:"created_at"           json:"createdAt"`
	UpdatedAt         time.Time          `bson:"updated_at"           json:"updatedAt"`
}

// CreatePromoInput is what an admin provides to create a code.
type CreatePromoInput struct {
	Code              string
	Description       string
	DiscountType      string
	DiscountValue     int64
	MinimumOrderCents int64
	MaxUses           int64
	MaxUsesPerUser    int64
	ExpiresAt         *time.Time
}

// ValidationResult is the answer to "does this code apply to my order?".
// A code that doesn't apply is a normal result (Valid=false with a Reason),
// not an error.
type ValidationResult struct {
	Valid              bool   `json:"valid"`
	Code               string `json:"code"`
	Reason             string `json:"reason,omitempty"`
	DiscountType       string `json:"discountType,omitempty"`
	DiscountValue      int64  `json:"discountValue,omitempty"`
	DiscountCents      int64  `json:"discountCents"`
	FinalSubtotalCents int64  `json:"finalSubtotalCents"`
	Message            string `json:"message"`
}

// Redemption is what checkout gets back after a successful Redeem.
type Redemption struct {
	PromoID       primitive.ObjectID
	Code          string
	DiscountCents int64
}
