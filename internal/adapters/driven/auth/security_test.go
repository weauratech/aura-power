package auth

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func TestPasswordPolicyAndInPlaceRotation(t *testing.T) {
	for _, password := range []string{"short", "password123!", "aaaaaaaaaaaa", string(make([]byte, 73))} {
		if err := ValidatePasswordStrength(password); !errors.Is(err, ErrWeakPassword) {
			t.Errorf("password %q: got %v, want ErrWeakPassword", password, err)
		}
	}
	for _, password := range []string{"correct horse battery staple", GenerateID()} {
		if err := ValidatePasswordStrength(password); err != nil {
			t.Errorf("strong password rejected: %v", err)
		}
	}

	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	oldPassword, newPassword := "Old admin passphrase 2026!", "New admin passphrase 2026!"
	user, err := s.CreateUser("admin", oldPassword, RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePassword(user.ID, "incorrect current password", newPassword); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("incorrect current password: %v", err)
	}
	if err := s.UpdatePassword(user.ID, oldPassword, "too-short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak new password: %v", err)
	}
	if err := s.UpdatePassword(user.ID, oldPassword, oldPassword); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("unchanged new password: %v", err)
	}
	unchanged, err := s.GetUserByID(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.PasswordHash != user.PasswordHash || unchanged.AuthVersion != user.AuthVersion {
		t.Fatal("rejected unchanged password modified credentials or auth version")
	}
	if err := s.UpdatePassword(user.ID, oldPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	rotated, err := s.GetUserByID(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.ID != user.ID || rotated.AuthVersion != user.AuthVersion+1 {
		t.Fatalf("rotation changed identity or lost version: before=%+v after=%+v", user, rotated)
	}
	if s.ValidatePassword(rotated, oldPassword) || !s.ValidatePassword(rotated, newPassword) {
		t.Fatal("password rotation did not replace the persisted hash")
	}
}

func TestLastAdministratorGuardsAreAtomic(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first := createTestUser(t, s, RoleAdmin)
	if err := s.UpdateUser(first.ID, RoleMember); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demoted final admin: %v", err)
	}
	if err := s.DeleteUser(first.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("deleted final admin: %v", err)
	}
	second := createTestUser(t, s, RoleAdmin)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{first.ID, second.ID} {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			<-start
			results <- s.UpdateUser(userID, RoleMember)
		}(id)
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded, blocked := 0, 0
	for result := range results {
		switch {
		case result == nil:
			succeeded++
		case errors.Is(result, ErrLastAdmin):
			blocked++
		default:
			t.Fatalf("unexpected concurrent result: %v", result)
		}
	}
	if succeeded != 1 || blocked != 1 {
		t.Fatalf("concurrent demotions succeeded=%d blocked=%d", succeeded, blocked)
	}
}

func TestForeignKeysApplyToEveryConnectionAndProtectHistory(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 2; i++ {
		conn, err := s.db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var enabled int
		if err := conn.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&enabled); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		conn.Close()
		if enabled != 1 {
			t.Fatalf("connection %d has foreign_keys=%d", i+1, enabled)
		}
	}
	user := createTestUser(t, s, RoleMember)
	if _, err := s.CreatePendingChange(PendingChange{UserID: user.ID, Username: user.Username, Action: "create", ResourceKind: "PowerPolicy", ResourceName: "history", Payload: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(user.ID); !errors.Is(err, ErrUserReferenced) {
		t.Fatalf("referenced user deletion: %v", err)
	}
	reviewer := createTestUser(t, s, RoleApprover)
	change, err := s.CreatePendingChange(PendingChange{UserID: user.ID, Username: user.Username, Action: "create", ResourceKind: "PowerPolicy", ResourceName: "reviewed-history", Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginPendingDecision(change.ID, reviewer.ID, "reject"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(reviewer.ID); !errors.Is(err, ErrUserReferenced) {
		t.Fatalf("reviewer deletion: %v", err)
	}
}

func TestOpeningLegacyDatabaseFailsClosedOnOrphanedReviewer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-reviewer.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashPassword("Legacy member passphrase 2026!")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'member', created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE pending_changes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, username TEXT NOT NULL, action TEXT NOT NULL, resource_kind TEXT NOT NULL, resource_name TEXT NOT NULL, payload TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', created_at DATETIME DEFAULT CURRENT_TIMESTAMP, reviewed_by TEXT, reviewed_at DATETIME, FOREIGN KEY (user_id) REFERENCES users(id));
		INSERT INTO users (id,username,password_hash,role) VALUES ('member','member',?,'member');
		INSERT INTO pending_changes (id,user_id,username,action,resource_kind,resource_name,payload,reviewed_by) VALUES ('reviewed','member','member','create','PowerPolicy','nightly','{}','removed-reviewer');`, hash)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if store, err := NewSQLiteStore(path); err == nil {
		store.Close()
		t.Fatal("database with an orphaned reviewer was accepted")
	}
}

func TestOpeningLegacyDatabaseFailsClosedOnOrphans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'member', created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE pending_changes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, username TEXT NOT NULL, action TEXT NOT NULL, resource_kind TEXT NOT NULL, resource_name TEXT NOT NULL, payload TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', created_at DATETIME DEFAULT CURRENT_TIMESTAMP, reviewed_by TEXT, reviewed_at DATETIME, FOREIGN KEY (user_id) REFERENCES users(id));
		INSERT INTO pending_changes (id,user_id,username,action,resource_kind,resource_name,payload) VALUES ('orphan','missing','removed','create','PowerPolicy','nightly','{}');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if store, err := NewSQLiteStore(path); err == nil {
		store.Close()
		t.Fatal("orphaned legacy database was accepted")
	}
}

func TestLegacyUserMigrationStartsAuthVersionAtOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashPassword("Legacy admin passphrase 2026!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'member', created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id,username,password_hash,role) VALUES ('legacy','admin',?,'admin')`, hash); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user, err := s.GetUserByID("legacy")
	if err != nil || user.AuthVersion != 1 {
		t.Fatalf("legacy auth version: user=%+v err=%v", user, err)
	}
}

func TestTokenPurposeIssuerAlgorithmAndAuthVersion(t *testing.T) {
	svc := NewJWTService(JWTConfig{SecretKey: "test-only-signing-key-with-sufficient-length"})
	user := &User{ID: GenerateID(), Username: "admin", Role: RoleAdmin, AuthVersion: 7}
	pair, err := svc.GenerateTokens(user)
	if err != nil {
		t.Fatal(err)
	}
	access, err := svc.ValidateToken(pair.AccessToken, TokenTypeAccess)
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := svc.ValidateToken(pair.RefreshToken, TokenTypeRefresh)
	if err != nil {
		t.Fatal(err)
	}
	if access.AuthVersion != 7 || refresh.AuthVersion != 7 || access.ID == "" || refresh.ID == "" || access.ID == refresh.ID {
		t.Fatalf("token claims incomplete: access=%+v refresh=%+v", access, refresh)
	}
	if _, err := svc.ValidateToken(pair.AccessToken, TokenTypeRefresh); err == nil {
		t.Fatal("access token accepted as refresh token")
	}
	if _, err := svc.ValidateToken(pair.RefreshToken, TokenTypeAccess); err == nil {
		t.Fatal("refresh token accepted as access token")
	}
}
