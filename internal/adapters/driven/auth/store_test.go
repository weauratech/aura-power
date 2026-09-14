package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSQLiteBusyTimeoutAppliesToEveryPooledConnection(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "auth db?.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	first, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	for i, conn := range []*sql.Conn{first, second} {
		var timeout int
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatalf("connection %d busy timeout: %v", i+1, err)
		}
		if timeout != sqliteBusyTimeoutMillis {
			t.Fatalf("connection %d busy timeout=%d, want %d", i+1, timeout, sqliteBusyTimeoutMillis)
		}
	}
}

func TestSQLiteUserLifecycleAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	password := GenerateID()
	u, err := s.CreateUser("member", password, RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == "" || u.PasswordHash == password || !s.ValidatePassword(u, password) || s.ValidatePassword(u, password+"wrong") {
		t.Fatal("password storage or validation contract violated")
	}
	if _, err := s.CreateUser("member", password, RoleAdmin); !errors.Is(err, ErrUserExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := s.UpdateUser(u.ID, RoleApprover); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ping(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetUserByUsername("member")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID || got.Role != RoleApprover || !s.ValidatePassword(got, password) {
		t.Fatal("reopened database lost user state")
	}
	users, err := s.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != u.ID {
		t.Fatalf("unexpected users: %+v", users)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), got.PasswordHash) || strings.Contains(string(raw), password) {
		t.Fatal("JSON exposed credential material")
	}
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetUserByID(u.ID); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
	if err := s.UpdateUser(u.ID, RoleAdmin); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("update deleted: %v", err)
	}
	if err := s.DeleteUser(u.ID); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestSQLitePendingDecisionIsTerminal(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, decision := range []string{"approved", "rejected"} {
		t.Run(decision, func(t *testing.T) {
			c, err := s.CreatePendingChange(PendingChange{UserID: "requester", Username: "member", Action: "create", ResourceKind: "PowerPolicy", ResourceName: decision, Payload: `{"scope":{"namespaces":["dev"]}}`})
			if err != nil {
				t.Fatal(err)
			}
			if c.Status != "pending" || c.CreatedAt.IsZero() {
				t.Fatalf("new change: %+v", c)
			}
			pending, err := s.ListPendingChanges()
			if err != nil || len(pending) != 1 {
				t.Fatalf("pending: %d, %v", len(pending), err)
			}
			var got *PendingChange
			if decision == "approved" {
				got, err = s.ApprovePendingChange(c.ID, "reviewer")
			} else {
				got, err = s.RejectPendingChange(c.ID, "reviewer")
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != decision || got.ReviewedBy != "reviewer" || got.ReviewedAt == nil || got.Payload != c.Payload {
				t.Fatalf("review state not persisted: %+v", got)
			}
			if _, err := s.ApprovePendingChange(c.ID, "other"); !errors.Is(err, ErrPendingDecisionConflict) {
				t.Fatalf("repeat approve: %v", err)
			}
			if _, err := s.RejectPendingChange(c.ID, "other"); !errors.Is(err, ErrPendingDecisionConflict) {
				t.Fatalf("repeat reject: %v", err)
			}
			pending, err = s.ListPendingChanges()
			if err != nil || len(pending) != 0 {
				t.Fatalf("terminal item remains pending: %d, %v", len(pending), err)
			}
		})
	}
	if _, err := s.GetPendingChange("missing"); !errors.Is(err, ErrPendingNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestSQLitePendingDecisionCannotReverseAfterDurableIntent(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	change, err := s.CreatePendingChange(PendingChange{UserID: "requester", Username: "member", Action: "create", ResourceKind: "PowerPolicy", ResourceName: "nightly", Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.BeginPendingDecision(change.ID, "reviewer", "approve")
	if err != nil || started.Status != "approving" {
		t.Fatalf("begin approve: %+v %v", started, err)
	}
	if _, err := s.BeginPendingDecision(change.ID, "reviewer", "reject"); !errors.Is(err, ErrPendingDecisionConflict) {
		t.Fatalf("opposite decision was accepted after durable approval intent: %v", err)
	}
	finished, err := s.FinalizePendingDecision(change.ID, "reviewer", "approve")
	if err != nil || finished.Status != "approved" {
		t.Fatalf("finalize approve: %+v %v", finished, err)
	}
}

func TestSQLitePendingDecisionCanCancelOrReclaimExpiredLease(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	change, err := s.CreatePendingChange(PendingChange{UserID: "requester", Username: "member", Action: "create", ResourceKind: "PowerPolicy", ResourceName: "nightly", Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginPendingDecision(change.ID, "reviewer-one", "approve"); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelPendingDecision(change.ID, "reviewer-one"); err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.GetPendingChange(change.ID)
	if err != nil || cancelled.Status != "pending" || cancelled.ReviewedBy != "" || cancelled.ReviewedAt != nil {
		t.Fatalf("cancel did not release decision: %+v %v", cancelled, err)
	}
	if _, err := s.BeginPendingDecision(change.ID, "reviewer-one", "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE pending_changes SET reviewed_at = ? WHERE id = ?", time.Now().Add(-6*time.Minute), change.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginPendingDecision(change.ID, "reviewer-two", "reject"); !errors.Is(err, ErrPendingDecisionConflict) {
		t.Fatalf("expired approval was reversed: %v", err)
	}
	reclaimed, err := s.BeginPendingDecision(change.ID, "reviewer-two", "approve")
	if err != nil || reclaimed.Status != "approving" || reclaimed.ReviewedBy != "reviewer-one" {
		t.Fatalf("expired decision lease was not reclaimable: %+v %v", reclaimed, err)
	}
	if _, err := s.FinalizePendingDecision(change.ID, "reviewer-two", "approve"); !errors.Is(err, ErrPendingDecisionConflict) {
		t.Fatalf("takeover replaced the durable decision owner: %v", err)
	}
	finalized, err := s.FinalizePendingDecision(change.ID, "reviewer-one", "approve")
	if err != nil || finalized.Status != "approved" || finalized.ReviewedBy != "reviewer-one" {
		t.Fatalf("original decision owner could not be finalized after takeover: %+v %v", finalized, err)
	}
}

func TestSQLitePendingDecisionLeaseIsExclusiveForSameReviewer(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	change, err := s.CreatePendingChange(PendingChange{UserID: "requester", Username: "member", Action: "create", ResourceKind: "PowerPolicy", ResourceName: "nightly", Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginPendingDecision(change.ID, "reviewer", "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginPendingDecision(change.ID, "reviewer", "approve"); !errors.Is(err, ErrPendingDecisionConflict) {
		t.Fatalf("same reviewer acquired the live lease twice: %v", err)
	}
}

func TestSQLitePendingDecisionConcurrentAcquisitionHasOneWinner(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	change, err := s.CreatePendingChange(PendingChange{UserID: "requester", Username: "member", Action: "create", ResourceKind: "PowerPolicy", ResourceName: "nightly", Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, acquireErr := s.BeginPendingDecision(change.ID, "same-reviewer", "approve")
			results <- acquireErr
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for result := range results {
		if result == nil {
			successes++
		} else if errors.Is(result, ErrPendingDecisionConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected acquisition error: %v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("lease winners=%d conflicts=%d", successes, conflicts)
	}
}

func TestJWTConfiguredLifetimeAndInvalidTokens(t *testing.T) {
	svc := NewJWTService(JWTConfig{AccessTokenTTL: time.Minute, RefreshTokenTTL: time.Hour})
	pair, err := svc.GenerateTokens(&User{ID: GenerateID(), Username: "member", Role: RoleMember})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := svc.ValidateToken(pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Username != "member" || claims.Role != RoleMember || claims.ExpiresAt.Unix() != pair.ExpiresAt {
		t.Fatal("access claims mismatch")
	}
	if svc.AccessTokenTTL() != time.Minute || svc.RefreshTokenTTL() != time.Hour {
		t.Fatal("configured TTL lost")
	}
	for _, token := range []string{"", "not-a-jwt", pair.AccessToken + "tampered"} {
		if _, err := svc.ValidateToken(token); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	other := NewJWTService(JWTConfig{})
	if _, err := other.ValidateToken(pair.AccessToken); err == nil {
		t.Fatal("different signing key accepted")
	}
	expired := NewJWTService(JWTConfig{AccessTokenTTL: -time.Minute})
	old, err := expired.GenerateTokens(&User{ID: GenerateID()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := expired.ValidateToken(old.AccessToken); err == nil {
		t.Fatal("expired token accepted")
	}
}
