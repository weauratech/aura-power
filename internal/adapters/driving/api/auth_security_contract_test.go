package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
)

func loginContract(t *testing.T, f *contractFixture, username, password string) (*auth.TokenPair, int) {
	t.Helper()
	response := requestContract(t, f.server.Handler(), http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": username, "password": password,
	})
	if response.Code != http.StatusOK {
		return nil, response.Code
	}
	pair := decodeContract[auth.TokenPair](t, response)
	return &pair, response.Code
}

func TestPasswordRotationRequiresCurrentPasswordAndRevokesBothTokens(t *testing.T) {
	f := newContractFixture(t)
	oldPassword := "Old primary passphrase 2026!"
	newPassword := "New primary passphrase 2026!"
	user, err := f.store.CreateUser("primary-admin", oldPassword, auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	oldPair, status := loginContract(t, f, user.Username, oldPassword)
	if status != http.StatusOK {
		t.Fatalf("initial login returned %d", status)
	}

	wrong := requestContract(t, f.server.Handler(), http.MethodPut, "/api/v1/auth/password", oldPair.AccessToken, map[string]string{
		"currentPassword": "incorrect current password", "newPassword": newPassword,
	})
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password returned %d", wrong.Code)
	}
	weak := requestContract(t, f.server.Handler(), http.MethodPut, "/api/v1/auth/password", oldPair.AccessToken, map[string]string{
		"currentPassword": oldPassword, "newPassword": "too-short",
	})
	if weak.Code != http.StatusBadRequest {
		t.Fatalf("weak new password returned %d", weak.Code)
	}
	unchanged := requestContract(t, f.server.Handler(), http.MethodPut, "/api/v1/auth/password", oldPair.AccessToken, map[string]string{
		"currentPassword": oldPassword, "newPassword": oldPassword,
	})
	if unchanged.Code != http.StatusBadRequest {
		t.Fatalf("unchanged new password returned %d", unchanged.Code)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodGet, "/api/v1/auth/me", oldPair.AccessToken, nil); response.Code != http.StatusOK {
		t.Fatalf("rejected rotation revoked access token: %d", response.Code)
	}
	if _, status := loginContract(t, f, user.Username, oldPassword); status != http.StatusOK {
		t.Fatalf("rejected rotation changed current password: %d", status)
	}
	rotated := requestContract(t, f.server.Handler(), http.MethodPut, "/api/v1/auth/password", oldPair.AccessToken, map[string]string{
		"currentPassword": oldPassword, "newPassword": newPassword,
	})
	if rotated.Code != http.StatusOK {
		t.Fatalf("password rotation returned %d: %s", rotated.Code, rotated.Body.String())
	}
	if _, status := loginContract(t, f, user.Username, oldPassword); status != http.StatusUnauthorized {
		t.Fatalf("old password login returned %d", status)
	}
	newPair, status := loginContract(t, f, user.Username, newPassword)
	if status != http.StatusOK {
		t.Fatalf("new password login returned %d", status)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodGet, "/api/v1/auth/me", oldPair.AccessToken, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("pre-rotation access token returned %d", response.Code)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refreshToken": oldPair.RefreshToken}); response.Code != http.StatusUnauthorized {
		t.Fatalf("pre-rotation refresh token returned %d", response.Code)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodGet, "/api/v1/auth/me", newPair.AccessToken, nil); response.Code != http.StatusOK {
		t.Fatalf("post-rotation access token returned %d", response.Code)
	}
}

func TestTokenPurposesCannotBeExchanged(t *testing.T) {
	f := newContractFixture(t)
	password := "Typed token passphrase 2026!"
	user, err := f.store.CreateUser("typed-token-user", password, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	pair, status := loginContract(t, f, user.Username, password)
	if status != http.StatusOK {
		t.Fatalf("login returned %d", status)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodGet, "/api/v1/status", pair.RefreshToken, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("refresh token used as access returned %d", response.Code)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refreshToken": pair.AccessToken}); response.Code != http.StatusUnauthorized {
		t.Fatalf("access token used as refresh returned %d", response.Code)
	}
}

func TestRoleChangeAndDeletionRevokeExistingSessions(t *testing.T) {
	f := newContractFixture(t)
	adminToken := f.token(t, auth.RoleAdmin)
	password := "Revoked member passphrase 2026!"
	member, err := f.store.CreateUser("revoked-member", password, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	memberPair, _ := loginContract(t, f, member.Username, password)
	if response := requestContract(t, f.server.Handler(), http.MethodPut, "/api/v1/users/"+member.ID, adminToken, map[string]string{"role": string(auth.RoleApprover)}); response.Code != http.StatusOK {
		t.Fatalf("role update returned %d: %s", response.Code, response.Body.String())
	}
	if response := requestContract(t, f.server.Handler(), http.MethodGet, "/api/v1/status", memberPair.AccessToken, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("stale-role access returned %d", response.Code)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refreshToken": memberPair.RefreshToken}); response.Code != http.StatusUnauthorized {
		t.Fatalf("stale-role refresh returned %d", response.Code)
	}
	memberPair, _ = loginContract(t, f, member.Username, password)
	if response := requestContract(t, f.server.Handler(), http.MethodDelete, "/api/v1/users/"+member.ID, adminToken, nil); response.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", response.Code, response.Body.String())
	}
	if response := requestContract(t, f.server.Handler(), http.MethodGet, "/api/v1/status", memberPair.AccessToken, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("deleted-user access returned %d", response.Code)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refreshToken": memberPair.RefreshToken}); response.Code != http.StatusUnauthorized {
		t.Fatalf("deleted-user refresh returned %d", response.Code)
	}
}

func TestUserAdministrationSafetyContracts(t *testing.T) {
	f := newContractFixture(t)
	adminToken := f.token(t, auth.RoleAdmin)
	claims, err := f.jwt.ValidateToken(adminToken, auth.TokenTypeAccess)
	if err != nil {
		t.Fatal(err)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodDelete, "/api/v1/users/"+claims.UserID, adminToken, nil); response.Code != http.StatusConflict {
		t.Fatalf("self-delete returned %d", response.Code)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodPut, "/api/v1/users/"+claims.UserID, adminToken, map[string]string{"role": string(auth.RoleMember)}); response.Code != http.StatusConflict {
		t.Fatalf("last-admin demotion returned %d", response.Code)
	}
	if response := requestContract(t, f.server.Handler(), http.MethodPost, "/api/v1/users", adminToken, map[string]string{
		"username": "weak-user", "password": "short", "role": string(auth.RoleMember),
	}); response.Code != http.StatusBadRequest {
		t.Fatalf("weak user creation returned %d", response.Code)
	}

	member, err := f.store.CreateUser("history-owner", "Approval history owner passphrase", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreatePendingChange(auth.PendingChange{UserID: member.ID, Username: member.Username, Action: "create", ResourceKind: "PowerPolicy", ResourceName: "history", Payload: `{}`}); err != nil {
		t.Fatal(err)
	}
	response := requestContract(t, f.server.Handler(), http.MethodDelete, "/api/v1/users/"+member.ID, adminToken, nil)
	if response.Code != http.StatusConflict || !errors.Is(f.store.DeleteUser(member.ID), auth.ErrUserReferenced) {
		t.Fatalf("referenced user delete returned %d: %s", response.Code, response.Body.String())
	}
}
