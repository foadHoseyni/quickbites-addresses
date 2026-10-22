package promocodes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type testServer struct {
	r     *gin.Engine
	admin string
	user  string
}

func newTestServer() *testServer {
	gin.SetMode(gin.TestMode)
	admin := primitive.NewObjectID()
	svc, _ := newTestService()
	r := gin.New()
	RegisterRoutes(r.Group("/api"), NewHandler(svc), map[primitive.ObjectID]bool{admin: true})
	return &testServer{r: r, admin: admin.Hex(), user: primitive.NewObjectID().Hex()}
}

func (s *testServer) do(method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, req)
	return w
}

const save10 = `{"code":"save10","discountType":"percentage","discountValue":10,"minimumOrderCents":1500}`

func TestAdminCreateAndPermissions(t *testing.T) {
	s := newTestServer()

	if w := s.do(http.MethodPost, "/api/admin/promo-codes", s.admin, save10); w.Code != http.StatusCreated {
		t.Fatalf("admin create: expected 201, got %d %s", w.Code, w.Body.String())
	}
	if w := s.do(http.MethodPost, "/api/admin/promo-codes", s.admin, save10); w.Code != http.StatusConflict {
		t.Fatalf("duplicate: expected 409, got %d", w.Code)
	}
	if w := s.do(http.MethodPost, "/api/admin/promo-codes", s.user, save10); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: expected 403, got %d", w.Code)
	}
	if w := s.do(http.MethodPost, "/api/admin/promo-codes", "", save10); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: expected 401, got %d", w.Code)
	}
}

func TestValidateEndpoint(t *testing.T) {
	s := newTestServer()
	s.do(http.MethodPost, "/api/admin/promo-codes", s.admin, save10)

	w := s.do(http.MethodPost, "/api/promo-codes/validate", s.user, `{"code":"SAVE10","subtotalCents":2598}`)
	var res ValidationResult
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != http.StatusOK || !res.Valid || res.DiscountCents != 260 || res.FinalSubtotalCents != 2338 {
		t.Fatalf("valid code: got %d %+v", w.Code, res)
	}

	// A code that doesn't apply is still 200, with valid=false and a reason.
	w = s.do(http.MethodPost, "/api/promo-codes/validate", s.user, `{"code":"NOPE","subtotalCents":2598}`)
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != http.StatusOK || res.Valid || res.Reason != ReasonNotFound {
		t.Fatalf("unknown code: got %d %+v", w.Code, res)
	}

	if w := s.do(http.MethodPost, "/api/promo-codes/validate", "", `{"code":"SAVE10","subtotalCents":2598}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous validate: expected 401, got %d", w.Code)
	}
	if w := s.do(http.MethodPost, "/api/promo-codes/validate", s.user, `{"code":"SAVE10"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing subtotal: expected 400, got %d", w.Code)
	}
}

func TestDeactivateEndpoint(t *testing.T) {
	s := newTestServer()
	s.do(http.MethodPost, "/api/admin/promo-codes", s.admin, save10)

	if w := s.do(http.MethodPatch, "/api/admin/promo-codes/save10/deactivate", s.admin, ""); w.Code != http.StatusOK {
		t.Fatalf("deactivate: expected 200, got %d", w.Code)
	}
	w := s.do(http.MethodPost, "/api/promo-codes/validate", s.user, `{"code":"SAVE10","subtotalCents":2598}`)
	var res ValidationResult
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Valid || res.Reason != ReasonInactive {
		t.Fatalf("after deactivate: got %+v", res)
	}
	if w := s.do(http.MethodPatch, "/api/admin/promo-codes/MISSING/deactivate", s.admin, ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown code: expected 404, got %d", w.Code)
	}
}
