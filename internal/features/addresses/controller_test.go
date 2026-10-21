package addresses

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// newTestServer wires the real handler and routes to the in-memory store.
func newTestServer() (*gin.Engine, *fakeStore) {
	gin.SetMode(gin.TestMode)
	store := newFakeStore()
	r := gin.New()
	RegisterRoutes(r.Group("/api"), NewHandler(NewService(store)))
	return r, store
}

func do(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const validBody = `{"label":"Home","addressLine1":"Main St 1","city":"Utrecht","zipCode":"3511AA"}`

func TestCreateReturns201(t *testing.T) {
	r, _ := newTestServer()
	user := primitive.NewObjectID().Hex()

	w := do(r, http.MethodPost, "/api/addresses", user, validBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateRequiresAuth(t *testing.T) {
	r, _ := newTestServer()
	w := do(r, http.MethodPost, "/api/addresses", "", validBody)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// The IDOR fix: a userId in the body is ignored; the session decides.
func TestCreateIgnoresUserIDInBody(t *testing.T) {
	r, _ := newTestServer()
	me := primitive.NewObjectID()
	victim := primitive.NewObjectID()
	body := `{"userId":"` + victim.Hex() + `","label":"Home","addressLine1":"Main St 1","city":"Utrecht","zipCode":"3511AA"}`

	w := do(r, http.MethodPost, "/api/addresses", me.Hex(), body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	var got Address
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.UserID != me {
		t.Fatalf("address was created for %s, expected the authenticated user %s", got.UserID.Hex(), me.Hex())
	}
}

func TestCreateThenListPersists(t *testing.T) {
	r, _ := newTestServer()
	user := primitive.NewObjectID().Hex()
	do(r, http.MethodPost, "/api/addresses", user, validBody)

	w := do(r, http.MethodGet, "/api/addresses", user, "")
	var list []Address
	json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != http.StatusOK || len(list) != 1 || !list[0].IsDefault {
		t.Fatalf("expected one default address, got %d items (status %d)", len(list), w.Code)
	}
}

func TestDeleteOtherUsersAddressIs404(t *testing.T) {
	r, _ := newTestServer()
	owner := primitive.NewObjectID().Hex()
	other := primitive.NewObjectID().Hex()

	created := do(r, http.MethodPost, "/api/addresses", owner, validBody)
	var addr Address
	json.Unmarshal(created.Body.Bytes(), &addr)

	w := do(r, http.MethodDelete, "/api/addresses/"+addr.ID.Hex(), other, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateCannotChangeOwnerOrDefault(t *testing.T) {
	r, store := newTestServer()
	owner := primitive.NewObjectID()
	created := do(r, http.MethodPost, "/api/addresses", owner.Hex(), validBody)
	var addr Address
	json.Unmarshal(created.Body.Bytes(), &addr)

	// Try mass assignment: only whitelisted fields are applied.
	body := `{"city":"Amsterdam","userId":"` + primitive.NewObjectID().Hex() + `","isDefault":false}`
	w := do(r, http.MethodPatch, "/api/addresses/"+addr.ID.Hex(), owner.Hex(), body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	got, _ := store.FindByIDAndUser(context.Background(), addr.ID, owner)
	if got == nil || got.City != "Amsterdam" || !got.IsDefault {
		t.Fatalf("expected city updated, owner and default unchanged; got %+v", got)
	}
}
