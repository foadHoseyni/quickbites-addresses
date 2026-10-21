package addresses

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Repository is the only code that talks to the "addresses" collection.
type Repository struct {
	collection *mongo.Collection
}

func NewRepository(db *mongo.Database) *Repository {
	return &Repository{collection: db.Collection("addresses")}
}

// EnsureIndexes creates the indexes the feature relies on.
func (r *Repository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
	})
	return err
}

func (r *Repository) Create(ctx context.Context, a *Address) error {
	now := time.Now()
	a.ID = primitive.NewObjectID()
	a.CreatedAt = now
	a.UpdatedAt = now
	_, err := r.collection.InsertOne(ctx, a)
	return err
}

// FindByUser lists a user's addresses, default first, then newest first.
func (r *Repository) FindByUser(ctx context.Context, userID primitive.ObjectID) ([]Address, error) {
	opts := options.Find().SetSort(bson.D{
		{Key: "is_default", Value: -1},
		{Key: "created_at", Value: -1},
	})
	cur, err := r.collection.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, err
	}
	out := []Address{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FindByIDAndUser returns (nil, nil) when nothing matches, so callers MUST
// check for nil. Filtering on user_id too means another user's address is
// simply "not found".
func (r *Repository) FindByIDAndUser(ctx context.Context, id, userID primitive.ObjectID) (*Address, error) {
	var a Address
	err := r.collection.FindOne(ctx, bson.M{"_id": id, "user_id": userID}).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repository) CountByUser(ctx context.Context, userID primitive.ObjectID) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{"user_id": userID})
}

// UpdateFields applies a $set and returns the document after the update.
func (r *Repository) UpdateFields(ctx context.Context, id primitive.ObjectID, fields bson.M) (*Address, error) {
	set := bson.M{"updated_at": time.Now()}
	for k, v := range fields {
		set[k] = v
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var a Address
	err := r.collection.FindOneAndUpdate(ctx, bson.M{"_id": id}, bson.M{"$set": set}, opts).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *Repository) SetDefault(ctx context.Context, id primitive.ObjectID, isDefault bool) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"is_default": isDefault, "updated_at": time.Now()}},
	)
	return err
}

func (r *Repository) UnsetAllDefaults(ctx context.Context, userID primitive.ObjectID) error {
	_, err := r.collection.UpdateMany(ctx,
		bson.M{"user_id": userID, "is_default": true},
		bson.M{"$set": bson.M{"is_default": false, "updated_at": time.Now()}},
	)
	return err
}

// FindNextAddress returns the user's most recently created address,
// used to promote a new default after the default is deleted.
func (r *Repository) FindNextAddress(ctx context.Context, userID primitive.ObjectID) (*Address, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}})
	var a Address
	err := r.collection.FindOne(ctx, bson.M{"user_id": userID}, opts).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
