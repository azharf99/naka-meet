package db

import (
	"context"
	"errors"
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

var (
	ErrUserNotFound = errors.New("user not found")
)

// UserStore abstracts persistence of host accounts so the API layer can be
// unit-tested (via an in-memory fake) without a real Postgres connection.
// GormUserStore is the production implementation backed by *gorm.DB.
type UserStore interface {
	CreateUser(ctx context.Context, u *User) error
	FindUserByEmail(ctx context.Context, email string) (*User, error)
}

type GormUserStore struct {
	db *gorm.DB
}

func NewGormUserStore(db *gorm.DB) *GormUserStore {
	return &GormUserStore{db: db}
}

func (s *GormUserStore) CreateUser(ctx context.Context, u *User) error {
	return s.db.WithContext(ctx).Create(u).Error
}

func (s *GormUserStore) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	if err := s.db.WithContext(ctx).Where("email = ?", email).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}
