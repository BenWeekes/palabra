// Package models provides database models
package models

import (
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/jmoiron/sqlx" // sql driver
)

// Database wraps the database connection
type Database struct {
	*sqlx.DB
}

// CreateDB creates a new database connection
func CreateDB(databaseURL string) (*Database, error) {
	// For stub mode, we can skip actual DB connection
	// Return nil DB - services should handle nil gracefully
	return &Database{}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	if d.DB != nil {
		return d.DB.Close()
	}
	return nil
}

// UserCredentials holds RTC/RTM credentials for a user
type UserCredentials struct {
	UID int     `json:"uid"`
	Rtc string  `json:"rtc"`
	Rtm *string `json:"rtm,omitempty"`
}

// UserAccount represents a user account in the database
type UserAccount struct {
	ID         int64          `db:"id"`
	Identifier string         `db:"identifier"`
	UserName   sql.NullString `db:"user_name"`
	Email      string         `db:"email"`
}

// Token represents an authentication token in the database
type Token struct {
	ID      int64  `db:"id"`
	TokenID string `db:"token_id"`
	UserID  int64  `db:"user_id"`
}

// Auth represents OAuth credentials in the database
type Auth struct {
	ID           int64     `db:"id"`
	Code         string    `db:"code"`
	AccessToken  string    `db:"access_token"`
	RefreshToken string    `db:"refresh_token"`
	TokenType    string    `db:"token_type"`
	Expiry       time.Time `db:"expiry"`
}

// Channel represents a channel in the database
type Channel struct {
	ID            int64  `db:"id"`
	ChannelName   string `db:"channel_name"`
	ChannelSecret string `db:"channel_secret"`
	DTMF          string `db:"dtmf"`
}
