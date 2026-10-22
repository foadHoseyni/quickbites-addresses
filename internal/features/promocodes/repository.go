package promocodes

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrDuplicateCode is returned when a promo code with the same code exists.
var ErrDuplicateCode = errors.New("promo code already exists")

// Repository stores promo codes in "promo_codes" and per-user usage counters
// in "promo_code_usage" (one small document per promo + user).
type Repository struct {
	codes *mongo.Collection
	usage *mongo.Collection
}

func NewRepository(db *mongo.Database) *Repository {
	return &Repository{
		codes: db.Collection("promo_codes"),
		usage: db.Collection("promo_code_usage"),
	}
}

// EnsureIndexes creates the unique indexes the atomic claims rely on.
func (r *Repository) EnsureIndexes(ctx context.Context) error {
	if _, err := r.codes.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "code", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return err
	}
	_, err := r.usage.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "promo_id", Value: 1}, {Key: "user_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

func (r *Repository) Create(ctx context.Context, p *PromoCode) error {
	now := time.Now()
	p.ID = primitive.NewObjectID()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := r.codes.InsertOne(ctx, p)
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicateCode
	}
	return err
}

// FindByCode returns (nil, nil) when no code matches.
func (r *Repository) FindByCode(ctx context.Context, code string) (*PromoCode, error) {
	var p PromoCode
	err := r.codes.FindOne(ctx, bson.M{"code": code}).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) List(ctx context.Context) ([]PromoCode, error) {
	cur, err := r.codes.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	out := []PromoCode{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetActive switches a code on or off; returns (nil, nil) if it doesn't exist.
func (r *Repository) SetActive(ctx context.Context, code string, active bool) (*PromoCode, error) {
	var p PromoCode
	err := r.codes.FindOneAndUpdate(ctx,
		bson.M{"code": code},
		bson.M{"$set": bson.M{"is_active": active, "updated_at": time.Now()}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UserUses returns how many times a user has redeemed a promo code.
func (r *Repository) UserUses(ctx context.Context, promoID, userID primitive.ObjectID) (int64, error) {
	var doc struct {
		Uses int64 `bson:"uses"`
	}
	err := r.usage.FindOne(ctx, bson.M{"promo_id": promoID, "user_id": userID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil
	}
	return doc.Uses, err
}

// ClaimUser atomically takes one of the user's uses. It returns false when
// the user already reached maxPerUser (0 = unlimited).
//
// How it stays atomic: the filter only matches while uses < max. If the
// counter is full, the filter doesn't match, the upsert tries to insert a
// second document for the same (promo_id, user_id), and the unique index
// rejects it with a duplicate-key error, which we read as "limit reached".
func (r *Repository) ClaimUser(ctx context.Context, promoID, userID primitive.ObjectID, maxPerUser int64) (bool, error) {
	filter := bson.M{"promo_id": promoID, "user_id": userID}
	if maxPerUser > 0 {
		filter["uses"] = bson.M{"$lt": maxPerUser}
	}
	update := bson.M{
		"$inc": bson.M{"uses": 1},
		"$set": bson.M{"updated_at": time.Now()},
	}
	_, err := r.usage.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ReleaseUser gives a user's use back (compensation when checkout fails).
func (r *Repository) ReleaseUser(ctx context.Context, promoID, userID primitive.ObjectID) error {
	_, err := r.usage.UpdateOne(ctx,
		bson.M{"promo_id": promoID, "user_id": userID, "uses": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"uses": -1}, "$set": bson.M{"updated_at": time.Now()}},
	)
	return err
}

// ClaimGlobal atomically takes one use of the code. The conditions are part
// of the update filter, so two concurrent requests can't both take the last
// use: only one of them still matches when the database applies the update.
func (r *Repository) ClaimGlobal(ctx context.Context, promoID primitive.ObjectID, now time.Time) (bool, error) {
	filter := bson.M{
		"_id":       promoID,
		"is_active": true,
		"$and": bson.A{
			bson.M{"$or": bson.A{
				bson.M{"expires_at": bson.M{"$exists": false}},
				bson.M{"expires_at": nil},
				bson.M{"expires_at": bson.M{"$gt": now}},
			}},
			bson.M{"$or": bson.A{
				bson.M{"max_uses": 0},
				bson.M{"$expr": bson.M{"$lt": bson.A{"$current_uses", "$max_uses"}}},
			}},
		},
	}
	res, err := r.codes.UpdateOne(ctx, filter, bson.M{
		"$inc": bson.M{"current_uses": 1},
		"$set": bson.M{"updated_at": now},
	})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// ReleaseGlobal gives one use of the code back.
func (r *Repository) ReleaseGlobal(ctx context.Context, promoID primitive.ObjectID) error {
	_, err := r.codes.UpdateOne(ctx,
		bson.M{"_id": promoID, "current_uses": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"current_uses": -1}, "$set": bson.M{"updated_at": time.Now()}},
	)
	return err
}
