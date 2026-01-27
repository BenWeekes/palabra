// Package graph provides GraphQL resolvers
package graph

import (
	"github.com/samyak-jain/agora_backend/pkg/models"
	"github.com/samyak-jain/agora_backend/utils"
)

// Resolver is the root resolver
type Resolver struct {
	DB     *models.Database
	Logger *utils.Logger
}
