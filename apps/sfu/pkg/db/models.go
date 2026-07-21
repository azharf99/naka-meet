package db

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type User struct {
	ID           string    `gorm:"primaryKey;type:uuid" json:"id"`
	Name         string    `json:"name"`
	Email        string    `gorm:"uniqueIndex" json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Room struct {
	ID        string    `gorm:"primaryKey;type:uuid" json:"id"`
	Slug      string    `gorm:"uniqueIndex" json:"slug"`
	HostID    string    `gorm:"type:uuid" json:"host_id"`
	CreatedAt time.Time `json:"created_at"`
}

type Recording struct {
	ID        string    `gorm:"primaryKey;type:uuid" json:"id"`
	RoomID    string    `json:"room_id"`
	S3URL     string    `json:"s3_url"`
	Status    string    `json:"status"` // 'processing', 'completed', 'failed'
	CreatedAt time.Time `json:"created_at"`
}

func InitDB(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, nil
	}

	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	// Auto Migrate PostgreSQL tables as specified in DATABASE.md
	if err := database.AutoMigrate(&User{}, &Room{}, &Recording{}); err != nil {
		return nil, fmt.Errorf("failed to auto-migrate database: %w", err)
	}

	return database, nil
}
