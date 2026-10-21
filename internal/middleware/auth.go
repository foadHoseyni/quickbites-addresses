// Package middleware holds HTTP middleware shared by all features.
//
// NOTE: the real project validates a JWT here. To keep this study project
// self-contained, the "token" is simply the user's ObjectID in hex:
//
//	Authorization: Bearer 65f0c0ffee0000000000abcd
//
// The important part is the contract: handlers get the user ONLY from
// GetCurrentUser, never from the request body.
package middleware

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"quickbites/backend/internal/apperrors"
)

const currentUserKey = "currentUser"

// CurrentUser is what handlers know about the authenticated caller.
type CurrentUser struct {
	ID primitive.ObjectID
}

// RequireAuth authenticates the request and stores the user in the context.
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" {
			AbortWithAppError(c, apperrors.NewUnauthorized("Authentication required"))
			return
		}
		id, err := primitive.ObjectIDFromHex(token)
		if err != nil {
			AbortWithAppError(c, apperrors.NewUnauthorized("Invalid token"))
			return
		}
		c.Set(currentUserKey, &CurrentUser{ID: id})
		c.Next()
	}
}

// GetCurrentUser returns the authenticated user set by RequireAuth.
func GetCurrentUser(c *gin.Context) (*CurrentUser, bool) {
	v, ok := c.Get(currentUserKey)
	if !ok {
		return nil, false
	}
	u, ok := v.(*CurrentUser)
	return u, ok
}

// AbortWithAppError writes an error response and stops the handler chain.
// AppErrors keep their status; anything else becomes a generic 500.
func AbortWithAppError(c *gin.Context, err error) {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		c.AbortWithStatusJSON(appErr.Status, appErr)
		return
	}
	c.AbortWithStatusJSON(500, apperrors.NewServerError("An error occurred"))
}
