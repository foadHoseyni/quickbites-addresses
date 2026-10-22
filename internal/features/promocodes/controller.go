package promocodes

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"quickbites/backend/internal/apperrors"
	"quickbites/backend/internal/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

type validateBody struct {
	Code          string `json:"code"          binding:"required"`
	SubtotalCents *int64 `json:"subtotalCents" binding:"required"`
}

type createBody struct {
	Code              string     `json:"code"          binding:"required"`
	Description       string     `json:"description"`
	DiscountType      string     `json:"discountType"  binding:"required"`
	DiscountValue     int64      `json:"discountValue" binding:"required"`
	MinimumOrderCents int64      `json:"minimumOrderCents"`
	MaxUses           int64      `json:"maxUses"`
	MaxUsesPerUser    int64      `json:"maxUsesPerUser"`
	ExpiresAt         *time.Time `json:"expiresAt"`
}

// POST /api/promo-codes/validate
// Always 200 for a well-formed request: whether the code applies is in the
// body (valid + reason). Only real failures produce error status codes.
func (h *Handler) Validate(c *gin.Context) {
	user, ok := middleware.GetCurrentUser(c)
	if !ok {
		middleware.AbortWithAppError(c, apperrors.NewUnauthorized("Authentication required"))
		return
	}
	var body validateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		middleware.AbortWithAppError(c, apperrors.NewBadRequest("code and subtotalCents are required"))
		return
	}

	res, _, err := h.svc.Validate(c.Request.Context(), body.Code, *body.SubtotalCents, user.ID)
	if err != nil {
		if _, isApp := err.(*apperrors.AppError); isApp {
			middleware.AbortWithAppError(c, err)
			return
		}
		middleware.AbortWithAppError(c, apperrors.NewServerError("Failed to validate promo code"))
		return
	}
	c.JSON(http.StatusOK, res)
}

// POST /api/admin/promo-codes
func (h *Handler) Create(c *gin.Context) {
	var body createBody
	if err := c.ShouldBindJSON(&body); err != nil {
		middleware.AbortWithAppError(c, apperrors.NewBadRequest("Invalid promo code data"))
		return
	}
	p, err := h.svc.CreatePromo(c.Request.Context(), CreatePromoInput{
		Code:              body.Code,
		Description:       body.Description,
		DiscountType:      body.DiscountType,
		DiscountValue:     body.DiscountValue,
		MinimumOrderCents: body.MinimumOrderCents,
		MaxUses:           body.MaxUses,
		MaxUsesPerUser:    body.MaxUsesPerUser,
		ExpiresAt:         body.ExpiresAt,
	})
	if err != nil {
		middleware.AbortWithAppError(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

// GET /api/admin/promo-codes
func (h *Handler) List(c *gin.Context) {
	list, err := h.svc.ListPromos(c.Request.Context())
	if err != nil {
		middleware.AbortWithAppError(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

// PATCH /api/admin/promo-codes/:code/activate and /deactivate
func (h *Handler) setActive(active bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := h.svc.SetActive(c.Request.Context(), c.Param("code"), active)
		if err != nil {
			middleware.AbortWithAppError(c, err)
			return
		}
		c.JSON(http.StatusOK, p)
	}
}
