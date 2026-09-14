package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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
	AuthVersion  int64     `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

// PendingChange represents a change awaiting approval.
type PendingChange struct {
	ID                string     `json:"id"`
	UserID            string     `json:"userId"`
	Username          string     `json:"username"`
	Action            string     `json:"action"`       // create, update, delete
	ResourceKind      string     `json:"resourceKind"` // PowerPolicy, PowerOverride
	ResourceNamespace string     `json:"resourceNamespace"`
	ResourceName      string     `json:"resourceName"`
	ResourceVersion   string     `json:"resourceVersion,omitempty"`
	Payload           string     `json:"payload"` // JSON of the resource spec
	Status            string     `json:"status"`  // pending, approving, rejecting, approved, rejected
	CreatedAt         time.Time  `json:"createdAt"`
	ReviewedBy        string     `json:"reviewedBy,omitempty"`
	ReviewedAt        *time.Time `json:"reviewedAt,omitempty"`
}

// Store defines the interface for user/auth storage.
type Store interface {
	// Health
	Ping() error

	// Users
	CreateUser(username, password string, role Role) (*User, error)
	GetUserByUsername(username string) (*User, error)
	GetUserByID(id string) (*User, error)
	ListUsers() ([]User, error)
	UpdateUser(id string, role Role) error
	UpdatePassword(id, currentPassword, newPassword string) error
	DeleteUser(id string) error
	ValidatePassword(user *User, password string) bool

	// Pending Changes
	CreatePendingChange(change PendingChange) (*PendingChange, error)
	ListPendingChanges() ([]PendingChange, error)
	BeginPendingDecision(id, reviewerID, decision string) (*PendingChange, error)
	CancelPendingDecision(id, reviewerID string) error
	FinalizePendingDecision(id, reviewerID, decision string) (*PendingChange, error)
	ApprovePendingChange(id, reviewerID string) (*PendingChange, error)
	RejectPendingChange(id, reviewerID string) (*PendingChange, error)
	GetPendingChange(id string) (*PendingChange, error)
}

const (
	minPasswordRunes = 12
	maxPasswordBytes = 72 // bcrypt silently ignores bytes after this boundary.
)

var rejectedPasswords = map[string]struct{}{
	"password123!":  {},
	"administrator": {},
	"changeme123!":  {},
}

// ValidatePasswordStrength enforces a predictable bcrypt-safe password policy.
// Composition rules are deliberately avoided: long generated passwords and
// passphrases are stronger than short strings that merely satisfy character
// class requirements.
func ValidatePasswordStrength(password string) error {
	if !utf8.ValidString(password) {
		return fmt.Errorf("%w: password must be valid UTF-8", ErrWeakPassword)
	}
	if utf8.RuneCountInString(password) < minPasswordRunes {
		return fmt.Errorf("%w: password must contain at least %d characters", ErrWeakPassword, minPasswordRunes)
	}
	if len([]byte(password)) > maxPasswordBytes {
		return fmt.Errorf("%w: password must not exceed %d bytes", ErrWeakPassword, maxPasswordBytes)
	}
	if _, rejected := rejectedPasswords[strings.ToLower(password)]; rejected {
		return fmt.Errorf("%w: password is too common", ErrWeakPassword)
	}
	var first rune
	allSame := true
	for i, r := range password {
		if i == 0 {
			first = r
		} else if r != first {
			allSame = false
		}
	}
	if allSame {
		return fmt.Errorf("%w: password is too repetitive", ErrWeakPassword)
	}
	return nil
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

// ErrInvalidCurrentPassword is returned when a password rotation cannot
// authenticate the current credential.
var ErrInvalidCurrentPassword = errors.New("current password is invalid")

// ErrWeakPassword is returned when a password does not satisfy the policy.
var ErrWeakPassword = errors.New("password does not satisfy the security policy")

// ErrLastAdmin prevents an operation from removing the final administrator.
var ErrLastAdmin = errors.New("cannot remove or demote the last administrator")

// ErrUserReferenced prevents deletion from orphaning approval history.
var ErrUserReferenced = errors.New("user is referenced by approval history")

// ErrPendingNotFound is returned when a pending change is not found.
var ErrPendingNotFound = errors.New("pending change not found")

// ErrInvalidPendingChange is returned when a requested operation cannot be
// represented safely by the approval workflow.
var ErrInvalidPendingChange = errors.New("invalid pending change")

// ErrPendingDecisionConflict indicates that another or opposite durable
// decision already owns the pending change.
var ErrPendingDecisionConflict = errors.New("pending change decision conflict")
