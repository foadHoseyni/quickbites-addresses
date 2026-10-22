package promocodes

import (
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"quickbites/backend/internal/middleware"
)

// RegisterRoutes mounts the user endpoint and the admin endpoints.
func RegisterRoutes(api *gin.RouterGroup, h *Handler, admins map[primitive.ObjectID]bool) {
	user := api.Group("/promo-codes", middleware.RequireAuth())
	user.POST("/validate", h.Validate)

	admin := api.Group("/admin/promo-codes", middleware.RequireAuth(), middleware.RequireAdmin(admins))
	admin.POST("", h.Create)
	admin.GET("", h.List)
	admin.PATCH("/:code/activate", h.setActive(true))
	admin.PATCH("/:code/deactivate", h.setActive(false))
}
