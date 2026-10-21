# QuickBites – Delivery Addresses (study project)

A complete, runnable version of the **delivery addresses** feature from the
take-home (Go question 1), it has following layers: **controller → service → repository → MongoDB**.

This is a task for interview. The real project has more features
(auth with JWT, cart, checkout, promo codes).

## Layout

```
cmd/server/main.go                  wiring + HTTP server + graceful shutdown
internal/apperrors/errors.go        errors that carry an HTTP status
internal/middleware/auth.go         RequireAuth, GetCurrentUser, AbortWithAppError
internal/database/mongo.go          Mongo connection
internal/features/addresses/
    model.go                        Address, AddAddressInput (no UserID!)
    repository.go                   all MongoDB access
    service.go                      business rules + Store interface
    controller.go                   HTTP handlers
    routes.go                       route registration
    fake_store_test.go              in-memory Store for tests
    service_test.go                 business-rule tests
    controller_test.go              HTTP tests (201, 401, 404, IDOR, mass assignment)
```

## Run the tests (no database needed)

```bash
go mod tidy
go test ./...
```

## Run the server

```bash
docker compose up -d          # MongoDB on localhost:27017
go run ./cmd/server           # listens on :8080
```

Environment variables: `MONGO_URI` (default `mongodb://localhost:27017`),
`MONGO_DB` (default `quickbites`), `HTTP_ADDR` (default `:8080`).

## Try it

The demo auth treats the bearer token as the user's ObjectID (hex).

```bash
U=65f0c0ffee0000000000abcd

# create (201) - the first address becomes the default
curl -i -X POST localhost:8080/api/addresses -H "Authorization: Bearer $U" \
  -H "Content-Type: application/json" \
  -d '{"label":"Home","addressLine1":"Main St 1","city":"Utrecht","zipCode":"3511AA"}'

# list
curl -s localhost:8080/api/addresses -H "Authorization: Bearer $U"

# partial update
curl -s -X PATCH localhost:8080/api/addresses/<id> -H "Authorization: Bearer $U" \
  -H "Content-Type: application/json" -d '{"city":"Amsterdam"}'

# set default / delete
curl -s -X PATCH  localhost:8080/api/addresses/<id>/default -H "Authorization: Bearer $U"
curl -s -X DELETE localhost:8080/api/addresses/<id>         -H "Authorization: Bearer $U"
```

## Endpoints

| Method | Path                          | Success |
|--------|-------------------------------|---------|
| GET    | /api/addresses                | 200     |
| POST   | /api/addresses                | 201     |
| PATCH  | /api/addresses/:id            | 200     |
| DELETE | /api/addresses/:id            | 200     |
| PATCH  | /api/addresses/:id/default    | 200     |

Another user's address (or a malformed id) always returns **404**, never 403.

## Design note: concurrent "set default" requests

### The problem

Setting a default address is two separate writes:

1. unset `is_default` on all of the user's addresses, then
2. set `is_default` on the chosen address.

These two writes are not atomic. If the same user sends two "set default"
requests for different addresses at the same moment, the writes can
interleave:

```
request A: unset all          request B: unset all
request A: set address 1      request B: set address 2
```
Because setting a default address involves two separate writes (**unset all defaults, then set one**), concurrent requests or failures can leave a user with **two default addresses or none**.

### Possible solutions

1. **MongoDB transaction:** Ensures both writes succeed or fail together, but requires a replica set and adds complexity.
2. **Partial unique index:** Prevents multiple default addresses for the same user at the database level. This is the recommended first improvement if the issue becomes significant.
3. **Store the default on the user document:** Keep a single `default_address_id` field, making the change a single atomic update.
4. **Optimistic concurrency:** Use a version number to detect conflicting updates and retry.
5. **Serialize per user:** Use a lock to prevent simultaneous changes, but this adds infrastructure and lock-handling complexity.

For example, a partial unique index can be created like this:

```go
coll.Indexes().CreateOne(ctx, mongo.IndexModel{
    Keys: bson.D{{Key: "user_id", Value: 1}},
    Options: options.Index().
        SetUnique(true).
        SetPartialFilterExpression(bson.M{"is_default": true}),
})
