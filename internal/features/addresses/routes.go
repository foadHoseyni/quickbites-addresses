package addresses

import (
	"github.com/gin-gonic/gin"

	"quickbites/backend/internal/middleware"
)

// RegisterRoutes mounts the address endpoints. Every route requires auth.
func RegisterRoutes(api *gin.RouterGroup, h *Handler) {
	g := api.Group("/addresses", middleware.RequireAuth())
	g.GET("", h.ListAddresses)
	g.POST("", h.AddAddress)
	g.PATCH("/:id", h.UpdateAddress)
	g.DELETE("/:id", h.DeleteAddress)
	g.PATCH("/:id/default", h.SetDefault)
}
