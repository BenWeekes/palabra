// Package middleware provides HTTP middleware
package middleware

import (
	"net/http"

	"github.com/samyak-jain/agora_backend/pkg/models"
	"github.com/samyak-jain/agora_backend/utils"
)

// AuthHandler returns a middleware that handles authentication
// For stub mode, this is a pass-through middleware
func AuthHandler(db *models.Database, logger *utils.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Pass-through for stub mode - no auth required
			next.ServeHTTP(w, r)
		})
	}
}
