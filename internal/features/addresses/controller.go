package addresses

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"quickbites/backend/internal/apperrors"
	"quickbites/backend/internal/middleware"
)

// Handler is the HTTP layer: parse the request, get the authenticated user,
// call the service, write the response. No business rules live here.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// addAddressBody. FIX: there is no UserID field any more. The original read
// the user ID from the body, which failed (the frontend never sent it) and
// was an IDOR hole (anyone could create addresses for another user).
type addAddressBody struct {
	Label        string `json:"label"        binding:"required"`
	AddressLine1 string `json:"addressLine1" binding:"required"`
	AddressLine2 string `json:"addressLine2"`
	City         string `json:"city"         binding:"required"`
	State        string `json:"state"`
	ZipCode      string `json:"zipCode"      binding:"required"`
}

// updateAddressBody uses pointers so "field not sent" differs from "set to
// empty". Only these fields can be edited, which also prevents mass
// assignment of user_id or is_default through this endpoint.
type updateAddressBody struct {
	Label        *string `json:"label"`
	AddressLine1 *string `json:"addressLine1"`
	AddressLine2 *string `json:"addressLine2"`
	City         *string `json:"city"`
	State        *string `json:"state"`
	ZipCode      *string `json:"zipCode"`
}

func (b updateAddressBody) toUpdates() bson.M {
	u := bson.M{}
	set := func(key string, v *string) {
		if v != nil {
			u[key] = *v
		}
	}
	set("label", b.Label)
	set("address_line1", b.AddressLine1)
	set("address_line2", b.AddressLine2)
	set("city", b.City)
	set("state", b.State)
	set("zip_code", b.ZipCode)
	return u
}

// GET /api/addresses
func (h *Handler) ListAddresses(c *gin.Context) {
	user, ok := middleware.GetCurrentUser(c)
	if !ok {
		middleware.AbortWithAppError(c, apperrors.NewUnauthorized("Authentication required"))
		return
	}
	list, err := h.svc.ListAddresses(c.Request.Context(), user.ID)
	if err != nil {
		middleware.AbortWithAppError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

// POST /api/addresses
func (h *Handler) AddAddress(c *gin.Context) {
	// FIX: identity comes from the session, never from the request body.
	user, ok := middleware.GetCurrentUser(c)
	if !ok {
		middleware.AbortWithAppError(c, apperrors.NewUnauthorized("Authentication required"))
		return
	}

	var body addAddressBody
	if err := c.ShouldBindJSON(&body); err != nil {
		middleware.AbortWithAppError(c, apperrors.NewBadRequest("Invalid address data"))
		return
	}

	addr, err := h.svc.AddAddress(c.Request.Context(), user.ID, AddAddressInput{
		Label:        body.Label,
		AddressLine1: body.AddressLine1,
		AddressLine2: body.AddressLine2,
		City:         body.City,
		State:        body.State,
		ZipCode:      body.ZipCode,
	})
	if err != nil {
		middleware.AbortWithAppError(c, err)
		return
	}
	// FIX: creating a resource returns 201 Created, not 200 OK.
	c.JSON(http.StatusCreated, addr)
}

// PATCH /api/addresses/:id
func (h *Handler) UpdateAddress(c *gin.Context) {
	user, ok := middleware.GetCurrentUser(c)
	if !ok {
		middleware.AbortWithAppError(c, apperrors.NewUnauthorized("Authentication required"))
		return
	}
	addrID, ok := parseID(c)
	if !ok {
		return
	}

	var body updateAddressBody
	if err := c.ShouldBindJSON(&body); err != nil {
		middleware.AbortWithAppError(c, apperrors.NewBadRequest("Invalid address data"))
		return
	}

	addr, err := h.svc.UpdateAddressPartial(c.Request.Context(), addrID, user.ID, body.toUpdates())
	if err != nil {
		middleware.AbortWithAppError(c, err)
		return
	}
	c.JSON(http.StatusOK, addr)
}

// DELETE /api/addresses/:id
func (h *Handler) DeleteAddress(c *gin.Context) {
	user, ok := middleware.GetCurrentUser(c)
	if !ok {
		middleware.AbortWithAppError(c, apperrors.NewUnauthorized("Authentication required"))
		return
	}
	addrID, ok := parseID(c)
	if !ok {
		return
	}

	if err := h.svc.DeleteAddress(c.Request.Context(), addrID, user.ID); err != nil {
		middleware.AbortWithAppError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Address deleted"})
}

// PATCH /api/addresses/:id/default
func (h *Handler) SetDefault(c *gin.Context) {
	user, ok := middleware.GetCurrentUser(c)
	if !ok {
		middleware.AbortWithAppError(c, apperrors.NewUnauthorized("Authentication required"))
		return
	}
	addrID, ok := parseID(c)
	if !ok {
		return
	}

	addr, err := h.svc.SetDefault(c.Request.Context(), addrID, user.ID)
	if err != nil {
		middleware.AbortWithAppError(c, err)
		return
	}
	c.JSON(http.StatusOK, addr)
}

// parseID converts the :id path parameter to an ObjectID. A malformed ID is
// treated as "not found" rather than a server error.
func parseID(c *gin.Context) (primitive.ObjectID, bool) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		middleware.AbortWithAppError(c, apperrors.NewNotFound("Address not found"))
		return primitive.NilObjectID, false
	}
	return id, true
}
