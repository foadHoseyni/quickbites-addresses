# QuickBites Backend

A Go service (Gin + MongoDB) for a food-ordering app. It currently provides
two features:

- **Delivery addresses**: a logged-in user manages their addresses and picks
  the default one used at checkout.
- **Promo codes**: admins create promotion codes, users check whether a code
  applies to their order, and checkout redeems codes safely under concurrency.

Every feature is layered as **controller → service → repository → MongoDB**:

- **Controller** (`controller.go`) handles HTTP: parses the request, takes the
  user from the authenticated session, calls the service and writes the response.
- **Service** (`service.go`) holds the business rules.
- **Repository** (`repository.go`) is the only code that talks to MongoDB.

Services depend on a small `Store` interface rather than on MongoDB directly,
so the business rules are tested against an in-memory store.

All endpoints require authentication (`Authorization: Bearer <token>`). The
user is always taken from the session, never from the request body.

---

## Delivery addresses

### API

| Method | Path                         | Purpose | Success |
|--------|------------------------------|---------|---------|
| GET    | `/api/addresses`             | List the user's addresses, default first, then newest first. | 200 |
| POST   | `/api/addresses`             | Add a new address. The user's first address automatically becomes the default. | 201 |
| PATCH  | `/api/addresses/:id`         | Partially update an address (label, address lines, city, state, zip code). Ownership and default status cannot be changed here. | 200 |
| DELETE | `/api/addresses/:id`         | Delete an address. If it was the default, the most recently added remaining address becomes the new default. | 200 |
| PATCH  | `/api/addresses/:id/default` | Make this address the user's default; all other addresses of the user stop being default. | 200 |

Errors: **401** unauthenticated, **400** invalid body, **404** when the address
does not exist **or belongs to another user**. Both cases deliberately look the
same, so the API never reveals whether someone else's address exists.

Create body (`label`, `addressLine1`, `city` and `zipCode` are required):

```json
{
  "label": "Home",
  "addressLine1": "Main St 1",
  "addressLine2": "",
  "city": "Utrecht",
  "state": "Utrecht",
  "zipCode": "3511AA"
}
```

Update body: any subset of the same fields, for example `{"city": "Amsterdam"}`.
Fields that are not sent stay unchanged.

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

---

## Promo codes

### What a promo code is

A code gives either a **percentage** discount (1–100%) or a **fixed** amount
off. A code can have a minimum order amount, an expiry time, a global usage
limit and a per-user usage limit (0 means unlimited). Codes are
**case-insensitive**: they are stored and looked up in upper case.

**All money is in integer cents** (`subtotalCents`, `discountCents`, ...). Floats
can't represent most decimal amounts exactly, so amounts are never stored or
calculated as floats. Percentage discounts round half up to the nearest cent,
and a discount is never larger than the subtotal.

### API

| Method | Path | Who | Purpose | Success |
|--------|------|-----|---------|---------|
| POST  | `/api/promo-codes/validate` | any user | Check whether a code applies to an order and preview the discount. Does **not** use the code. | 200 |
| POST  | `/api/admin/promo-codes` | admin | Create a promo code. | 201 |
| GET   | `/api/admin/promo-codes` | admin | List all promo codes with their usage. | 200 |
| PATCH | `/api/admin/promo-codes/:code/deactivate` | admin | Switch a code off; it stops applying immediately. | 200 |
| PATCH | `/api/admin/promo-codes/:code/activate` | admin | Switch a code back on. | 200 |

Admins are configured with the `ADMIN_USER_IDS` environment variable (a
comma-separated list of user ids). Non-admins get **403**.

**Validate** request and response:

```json
{ "code": "save10", "subtotalCents": 2598 }
```

```json
{
  "valid": true,
  "code": "SAVE10",
  "discountType": "percentage",
  "discountValue": 10,
  "discountCents": 260,
  "finalSubtotalCents": 2338,
  "message": "Promo code applied: 10% off"
}
```

A code that doesn't apply is a normal answer, not an error: the endpoint still
returns **200** with `"valid": false` and a `reason`. Error status codes are
reserved for real problems (401, 400 for a malformed request, 500). The checks
run in this order, and the first failing one gives the reason:

1. `promo code not found`
2. `the promotion code is inactive`
3. `the promotion code has expired`
4. `minimum order of X.XX required for this promo code`
5. `the promotion code has reached its usage limit`
6. `you have already used this promotion code`

Inactive is checked before expired on purpose: "this code was switched off" is
the more fundamental answer.

**Create** request (admin):

```json
{
  "code": "SAVE10",
  "description": "10% off orders from 15.00",
  "discountType": "percentage",
  "discountValue": 10,
  "minimumOrderCents": 1500,
  "maxUses": 1000,
  "maxUsesPerUser": 1,
  "expiresAt": "2026-12-31T23:59:59Z"
}
```

For `"discountType": "fixed"`, `discountValue` is the amount off in cents.
Invalid input returns **400**; an existing code (case-insensitive) returns **409**.

### Redeeming at checkout

Checkout does not call the validate endpoint; it calls the service directly
and must follow this contract:

```go
redemption, err := promoService.Redeem(ctx, code, subtotalCents, userID)
if err != nil {
    return err // 400 with the reason the code doesn't apply
}
order, err := orders.Create(ctx, ..., redemption.DiscountCents)
if err != nil {
    _ = promoService.Release(ctx, redemption, userID) // give the use back
    return err
}
```

`Redeem` re-validates the code at the moment of purchase (the price is always
decided on the server, never taken from an earlier validate call) and then
**uses the code up atomically**.

### Design caveat: concurrent redemptions

Checking a limit and then recording a use is a *check-then-act* race: with one
use left, two checkouts can both pass and the code ends at 11 of 10 (or a
once-per-user code is used twice by a double-click). To prevent this, the
database enforces the limits at write time:

- **Global limit:** a single `UpdateOne` increments `current_uses` only if its
  filter still holds (`is_active`, not expired, `current_uses < max_uses`).
  The losing request gets `MatchedCount == 0` and is rejected.
- **Per-user limit:** a conditional upsert on `promo_code_usage`, with a
  **unique index** on `(promo_id, user_id)`, increments `uses` only while
  below the limit; otherwise the duplicate-key error means "already used".

If the global claim fails, the per-user claim is released again, and
`Release` does the same when checkout fails after `Redeem`. A transaction would
also work but needs a replica set; single-document conditional updates are
atomic and enough here.

**Trade-off:** a crash between `Redeem` and `Release` leaves a use counted
without an order, which errs on the safe side (never *more* uses than allowed).
---

## Project layout

```
cmd/server/main.go                    wiring, HTTP server, graceful shutdown
internal/apperrors/errors.go          errors that carry an HTTP status
internal/middleware/auth.go           RequireAuth, GetCurrentUser, AbortWithAppError
internal/middleware/admin.go          RequireAdmin, ParseAdminIDs
internal/database/mongo.go            MongoDB connection
internal/features/addresses/          delivery addresses (model, repository, service, controller, routes, tests)
internal/features/promocodes/         promo codes (model, repository, service, controller, routes, tests)
```

## Running

```bash
go mod tidy
go test -race ./...           # runs without a database

docker compose up -d          # MongoDB on localhost:27017
ADMIN_USER_IDS=65f0c0ffee0000000000aaaa go run ./cmd/server   # listens on :8080
```

Environment variables: `MONGO_URI` (default `mongodb://localhost:27017`),
`MONGO_DB` (default `quickbites`), `HTTP_ADDR` (default `:8080`),
`ADMIN_USER_IDS` (comma-separated admin user ids).

Authentication is simplified: the bearer token is the user's ObjectID in hex.
In production the middleware validates a JWT instead; handlers only depend on
`middleware.GetCurrentUser`, so nothing else changes.

### Examples

```bash
U=65f0c0ffee0000000000abcd      # a normal user
A=65f0c0ffee0000000000aaaa      # an admin (listed in ADMIN_USER_IDS)

# addresses
curl -i -X POST localhost:8080/api/addresses -H "Authorization: Bearer $U" \
  -H "Content-Type: application/json" \
  -d '{"label":"Home","addressLine1":"Main St 1","city":"Utrecht","zipCode":"3511AA"}'
curl -s localhost:8080/api/addresses -H "Authorization: Bearer $U"
curl -s -X PATCH localhost:8080/api/addresses/<id>/default -H "Authorization: Bearer $U"

# promo codes
curl -i -X POST localhost:8080/api/admin/promo-codes -H "Authorization: Bearer $A" \
  -H "Content-Type: application/json" \
  -d '{"code":"SAVE10","discountType":"percentage","discountValue":10,"minimumOrderCents":1500,"maxUsesPerUser":1}'
curl -s -X POST localhost:8080/api/promo-codes/validate -H "Authorization: Bearer $U" \
  -H "Content-Type: application/json" -d '{"code":"save10","subtotalCents":2598}'
curl -s -X PATCH localhost:8080/api/admin/promo-codes/SAVE10/deactivate -H "Authorization: Bearer $A"
```
