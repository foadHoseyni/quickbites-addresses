Delivery Addresses (study project)

A complete, runnable version of the **delivery addresses** feature from the
take-home (Go question 1), it has following layers: **controller → service → repository → MongoDB**.

## The task

The task was to build the endpoints that let a user **set their delivery
addresses** and **set their default address**:

- **Setting an address:** a user can add a new delivery address, edit an
  existing one, list their addresses and delete one they no longer use.
- **Setting the default address:** a user can mark one of their addresses as
  the default. A user with addresses always has exactly one default: the first
  address they add becomes the default automatically, choosing a new default
  removes the flag from the old one, and deleting the default promotes another
  address.

A user can only ever see or change their own addresses.

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

## Caveat: concurrent "set default" requests

Setting the default is two separate writes: unset `is_default` on all of the
user's addresses, then set it on the chosen one. These writes are not atomic,
so if the same user sends two "set default" requests at exactly the same
moment, they can interleave and leave the user with **two defaults**, or with
**none** if a request fails between its two writes. Deleting the default
address has the same two-step shape.

This is extremely rare, since it needs one person to change their default from
two places at once, and the next "set default" call fixes it, so the service
keeps the simple approach. If it needs to be prevented, there are three
options, from simplest to strongest:

1. **Partial unique index** on `user_id` for documents where
   `is_default: true`. MongoDB then rejects a second default for the same user,
   and the losing request returns a conflict or retries.

   ```go
   coll.Indexes().CreateOne(ctx, mongo.IndexModel{
       Keys: bson.D{{Key: "user_id", Value: 1}},
       Options: options.Index().
           SetUnique(true).
           SetPartialFilterExpression(bson.M{"is_default": true}),
   })
   ```

2. **MongoDB transaction** around both writes, so they succeed or fail
   together. This also prevents the "no default" case, but needs a replica set.

3. **Store `default_address_id` on the user document**, so changing the
   default is a single atomic update and two defaults become impossible by
   design.
