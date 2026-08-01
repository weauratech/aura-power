package auth

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite-backed auth store.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, err
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	return store, nil
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'member',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS pending_changes (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		username TEXT NOT NULL,
		action TEXT NOT NULL,
		resource_kind TEXT NOT NULL,
		resource_name TEXT NOT NULL,
		payload TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		reviewed_by TEXT,
		reviewed_at DATETIME,
		FOREIGN KEY (user_id) REFERENCES users(id)
	);

	CREATE INDEX IF NOT EXISTS idx_pending_status ON pending_changes(status);
	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	`
	_, err := s.db.Exec(schema)
	return err
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// --- Users ---

func (s *SQLiteStore) CreateUser(username, password string, role Role) (*User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	id := GenerateID()
	now := time.Now()

	_, err = s.db.Exec(
		"INSERT INTO users (id, username, password_hash, role, created_at) VALUES (?, ?, ?, ?, ?)",
		id, username, hash, string(role), now,
	)
	if err != nil {
		return nil, ErrUserExists
	}

	return &User{ID: id, Username: username, PasswordHash: hash, Role: role, CreatedAt: now}, nil
}

func (s *SQLiteStore) GetUserByUsername(username string) (*User, error) {
	var user User
	err := s.db.QueryRow(
		"SELECT id, username, password_hash, role, created_at FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *SQLiteStore) GetUserByID(id string) (*User, error) {
	var user User
	err := s.db.QueryRow(
		"SELECT id, username, password_hash, role, created_at FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *SQLiteStore) ListUsers() ([]User, error) {
	rows, err := s.db.Query("SELECT id, username, role, created_at FROM users ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (s *SQLiteStore) UpdateUser(id string, role Role) error {
	result, err := s.db.Exec("UPDATE users SET role = ? WHERE id = ?", string(role), id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteUser(id string) error {
	result, err := s.db.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *SQLiteStore) ValidatePassword(user *User, password string) bool {
	return CheckPassword(password, user.PasswordHash)
}

// --- Pending Changes ---

func (s *SQLiteStore) CreatePendingChange(change PendingChange) (*PendingChange, error) {
	change.ID = GenerateID()
	change.Status = "pending"
	change.CreatedAt = time.Now()

	_, err := s.db.Exec(
		"INSERT INTO pending_changes (id, user_id, username, action, resource_kind, resource_name, payload, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		change.ID, change.UserID, change.Username, change.Action, change.ResourceKind, change.ResourceName, change.Payload, change.Status, change.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &change, nil
}

func (s *SQLiteStore) ListPendingChanges() ([]PendingChange, error) {
	rows, err := s.db.Query(
		"SELECT id, user_id, username, action, resource_kind, resource_name, payload, status, created_at, reviewed_by, reviewed_at FROM pending_changes WHERE status = 'pending' ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []PendingChange
	for rows.Next() {
		var c PendingChange
		var reviewedBy sql.NullString
		var reviewedAt sql.NullTime
		if err := rows.Scan(&c.ID, &c.UserID, &c.Username, &c.Action, &c.ResourceKind, &c.ResourceName, &c.Payload, &c.Status, &c.CreatedAt, &reviewedBy, &reviewedAt); err != nil {
			return nil, err
		}
		if reviewedBy.Valid {
			c.ReviewedBy = reviewedBy.String
		}
		if reviewedAt.Valid {
			t := reviewedAt.Time
			c.ReviewedAt = &t
		}
		changes = append(changes, c)
	}
	return changes, nil
}

func (s *SQLiteStore) ApprovePendingChange(id, reviewerID string) (*PendingChange, error) {
	now := time.Now()
	result, err := s.db.Exec(
		"UPDATE pending_changes SET status = 'approved', reviewed_by = ?, reviewed_at = ? WHERE id = ? AND status = 'pending'",
		reviewerID, now, id,
	)
	if err != nil {
		return nil, err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, ErrPendingNotFound
	}
	return s.GetPendingChange(id)
}

func (s *SQLiteStore) RejectPendingChange(id, reviewerID string) (*PendingChange, error) {
	now := time.Now()
	result, err := s.db.Exec(
		"UPDATE pending_changes SET status = 'rejected', reviewed_by = ?, reviewed_at = ? WHERE id = ? AND status = 'pending'",
		reviewerID, now, id,
	)
	if err != nil {
		return nil, err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, ErrPendingNotFound
	}
	return s.GetPendingChange(id)
}

func (s *SQLiteStore) GetPendingChange(id string) (*PendingChange, error) {
	var c PendingChange
	var reviewedBy sql.NullString
	var reviewedAt sql.NullTime
	err := s.db.QueryRow(
		"SELECT id, user_id, username, action, resource_kind, resource_name, payload, status, created_at, reviewed_by, reviewed_at FROM pending_changes WHERE id = ?",
		id,
	).Scan(&c.ID, &c.UserID, &c.Username, &c.Action, &c.ResourceKind, &c.ResourceName, &c.Payload, &c.Status, &c.CreatedAt, &reviewedBy, &reviewedAt)
	if err == sql.ErrNoRows {
		return nil, ErrPendingNotFound
	}
	if err != nil {
		return nil, err
	}
	if reviewedBy.Valid {
		c.ReviewedBy = reviewedBy.String
	}
	if reviewedAt.Valid {
		t := reviewedAt.Time
		c.ReviewedAt = &t
	}
	return &c, nil
}
