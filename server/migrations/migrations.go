// Package migrations handles database migrations
package migrations

import (
	"github.com/rs/zerolog/log"
)

// RunMigration runs database migrations
// For stub mode, this is a no-op
func RunMigration(configDir *string) {
	log.Info().Msg("Skipping migrations in stub mode")
}
