package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Role defines user access level.
type Role string

const (
	RoleMember   Role = "member"
	RoleApprover Role = "approver"
	RoleAdmin    Role = "admin"
)

// User represents an authenticated user.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
}

// PendingChange represents a change awaiting approval.
type PendingChange struct {
	ID          string    `json:"id"`
	UserID      string    `json:"userId"`
	Username    string    `json:"username"`
	Action      string    `json:"action"` // create, update, delete
	ResourceKind string   `json:"resourceKind"` // PowerPolicy, PowerOverride
	ResourceName string   `json:"resourceName"`
	Payload     string    `json:"payload"` // JSON of the resource spec
	Status      string    `json:"status"` // pending, approved, rejected
	CreatedAt   time.Time `json:"createdAt"`
	ReviewedBy  string    `json:"reviewedBy,omitempty"`
	ReviewedAt  *time.Time `json:"reviewedAt,omitempty"`
}

// Store defines the interface for user/auth storage.
type Store interface {
	// Users
	CreateUser(username, password string, role Role) (*User, error)
	GetUserByUsername(username string) (*User, error)
	GetUserByID(id string) (*User, error)
	ListUsers() ([]User, error)
	UpdateUser(id string, role Role) error
	DeleteUser(id string) error
	ValidatePassword(user *User, password string) bool

	// Pending Changes
	CreatePendingChange(change PendingChange) (*PendingChange, error)
	ListPendingChanges() ([]PendingChange, error)
	ApprovePendingChange(id, reviewerID string) (*PendingChange, error)
	RejectPendingChange(id, reviewerID string) (*PendingChange, error)
	GetPendingChange(id string) (*PendingChange, error)
}

// HashPassword hashes a password with bcrypt.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}

// CheckPassword compares a password against a hash.
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateID creates a random hex ID.
func GenerateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ErrUserNotFound is returned when a user is not found.
var ErrUserNotFound = errors.New("user not found")

// ErrUserExists is returned when username already exists.
var ErrUserExists = errors.New("username already exists")

// ErrPendingNotFound is returned when a pending change is not found.
var ErrPendingNotFound = errors.New("pending change not found")
