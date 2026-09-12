package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
	"github.com/weauratech/aura-power/internal/core/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type contractFixture struct {
	server *Server
	store  *auth.SQLiteStore
	jwt    *auth.JWTService
	client client.Client
}

func newContractFixture(t *testing.T, objects ...client.Object) *contractFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	store, err := auth.NewSQLiteStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	jwt := auth.NewJWTService(auth.JWTConfig{})
	s := NewServer(c, nil, ServerConfig{GuardrailConfig: domain.DefaultGuardrailConfig(), CostConfig: domain.DefaultCostConfig()})
	s.RegisterAuthRoutes(store, jwt)
	s.FinalizeRoutes()
	return &contractFixture{s, store, jwt, c}
}
func (f *contractFixture) token(t *testing.T, role auth.Role) string {
	t.Helper()
	p, e := f.jwt.GenerateTokens(&auth.User{ID: auth.GenerateID(), Username: string(role), Role: role})
	if e != nil {
		t.Fatal(e)
	}
	return p.AccessToken
}
func requestContract(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	return out
}
func decodeContract[T any](t *testing.T, r *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if e := json.Unmarshal(r.Body.Bytes(), &out); e != nil {
		t.Fatalf("decode status %d: %v", r.Code, e)
	}
	return out
}

func TestHTTPRoleMatrix(t *testing.T) {
	// Each route is exercised with a fresh resource; allowed actions must persist their effect.
	for _, role := range []auth.Role{auth.RoleMember, auth.RoleApprover, auth.RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			f := newContractFixture(t)
			token := f.token(t, role)
			reads := []string{"/status", "/dashboard", "/discover", "/targets", "/savings", "/savings/breakdown", "/audit", "/policies", "/overrides", "/namespaces", "/namespace-groups", "/notification-channels"}
			for _, p := range reads {
				r := requestContract(t, f.server.Handler(), "GET", "/api/v1"+p, token, nil)
				if r.Code != 200 {
					t.Errorf("GET %s: got %d want 200", p, r.Code)
				}
			}
			for _, resource := range []struct {
				path   string
				object client.Object
			}{
				{"policies", &v1alpha1.PowerPolicy{}}, {"overrides", &v1alpha1.PowerOverride{}}, {"namespace-groups", &v1alpha1.PowerNamespaceGroup{}}, {"notification-channels", &v1alpha1.PowerNotificationChannel{}},
			} {
				t.Run(resource.path, func(t *testing.T) {
					object := resource.object.DeepCopyObject().(client.Object)
					object.SetName("contract")
					object.SetNamespace("aura-system")
					r := requestContract(t, f.server.Handler(), "POST", "/api/v1/"+resource.path, token, object)
					want := 201
					if role == auth.RoleMember {
						want = 403
					}
					if r.Code != want {
						t.Fatalf("create got %d want %d: %s", r.Code, want, r.Body.String())
					}
					err := f.client.Get(context.Background(), client.ObjectKeyFromObject(object), resource.object)
					if role == auth.RoleMember {
						if err == nil {
							t.Fatal("denied create persisted")
						}
						if err := f.client.Create(context.Background(), object); err != nil {
							t.Fatal(err)
						}
					} else if err != nil {
						t.Fatalf("successful create not persisted: %v", err)
					}
					if resource.path == "policies" || resource.path == "notification-channels" {
						r = requestContract(t, f.server.Handler(), "PUT", "/api/v1/"+resource.path+"/aura-system/contract", token, object)
						want = 200
						if role == auth.RoleMember {
							want = 403
						}
						if r.Code != want {
							t.Errorf("update got %d want %d", r.Code, want)
						}
					}
					r = requestContract(t, f.server.Handler(), "DELETE", "/api/v1/"+resource.path+"/aura-system/contract", token, nil)
					want = 403
					if role == auth.RoleAdmin {
						want = 200
					}
					if r.Code != want {
						t.Fatalf("delete got %d want %d", r.Code, want)
					}
					err = f.client.Get(context.Background(), client.ObjectKeyFromObject(object), resource.object)
					if role == auth.RoleAdmin && err == nil {
						t.Fatal("successful delete left resource")
					}
					if role != auth.RoleAdmin && err != nil {
						t.Fatal("denied delete changed resource")
					}
				})
			}
			for _, endpoint := range []string{"users", "pending"} {
				r := requestContract(t, f.server.Handler(), "GET", "/api/v1/"+endpoint, token, nil)
				want := 200
				if role == auth.RoleMember || (endpoint == "users" && role != auth.RoleAdmin) {
					want = 403
				}
				if r.Code != want {
					t.Errorf("%s got %d want %d", endpoint, r.Code, want)
				}
			}
		})
	}
}

func TestHTTPLoginRefreshCookieAndLogoutContract(t *testing.T) {
	f := newContractFixture(t)
	password := auth.GenerateID()
	user, err := f.store.CreateUser("alice", password, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []any{map[string]string{}, map[string]string{"username": "alice", "password": "wrong"}, map[string]string{"username": "missing", "password": password}} {
		r := requestContract(t, f.server.Handler(), "POST", "/api/v1/auth/login", "", body)
		want := 401
		if len(body.(map[string]string)) == 0 {
			want = 400
		}
		if r.Code != want {
			t.Fatalf("bad login got %d want %d", r.Code, want)
		}
	}
	body, _ := json.Marshal(map[string]string{"username": "alice", "password": password})
	req := httptest.NewRequest("POST", "https://example.test/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(r, req)
	if r.Code != 200 {
		t.Fatalf("login: %d", r.Code)
	}
	pair := decodeContract[auth.TokenPair](t, r)
	if pair.AccessToken == "" || pair.RefreshToken == "" || pair.ExpiresAt == 0 {
		t.Fatal("incomplete token response")
	}
	cookies := r.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies=%d", len(cookies))
	}
	for _, cookie := range cookies {
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge <= 0 {
			t.Errorf("invalid session cookie attributes: %s", cookie.Name)
		}
		wantPath := "/"
		if cookie.Name == "aura_refresh" {
			wantPath = "/api/v1/auth"
		}
		if cookie.Path != wantPath {
			t.Errorf("cookie %s path %s", cookie.Name, cookie.Path)
		}
	}
	me := requestContract(t, f.server.Handler(), "GET", "/api/v1/auth/me", pair.AccessToken, nil)
	got := decodeContract[auth.User](t, me)
	if me.Code != 200 || got.ID != user.ID || got.Role != auth.RoleMember {
		t.Fatal("me contract")
	}
	cookieReq := httptest.NewRequest("GET", "/api/v1/status", nil)
	cookieReq.AddCookie(&http.Cookie{Name: "aura_session", Value: pair.AccessToken})
	cookieResponse := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(cookieResponse, cookieReq)
	if cookieResponse.Code != 200 {
		t.Fatal("cookie auth failed")
	}
	refresh := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
	refresh.AddCookie(&http.Cookie{Name: "aura_refresh", Value: pair.RefreshToken})
	r = httptest.NewRecorder()
	f.server.Handler().ServeHTTP(r, refresh)
	if r.Code != 200 {
		t.Fatalf("cookie refresh: %d", r.Code)
	}
	refreshed := decodeContract[auth.TokenPair](t, r)
	claims, err := f.jwt.ValidateToken(refreshed.AccessToken)
	if err != nil || claims.UserID != user.ID {
		t.Fatal("refresh returned invalid identity")
	}
	r = requestContract(t, f.server.Handler(), "POST", "/api/v1/auth/logout", pair.AccessToken, nil)
	if r.Code != 200 {
		t.Fatal("logout failed")
	}
	for _, cookie := range r.Result().Cookies() {
		if cookie.MaxAge >= 0 || cookie.Value != "" {
			t.Fatal("logout must clear cookies")
		}
	}
	for _, path := range []string{"/status", "/policies", "/users", "/pending"} {
		r = requestContract(t, f.server.Handler(), "GET", "/api/v1"+path, "", nil)
		if r.Code != 401 {
			t.Errorf("missing auth %s: %d", path, r.Code)
		}
	}
}

type failingListClient struct{ client.Client }

func (c failingListClient) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("fixture unavailable")
}
func TestHTTPReadinessAndMalformedJSON(t *testing.T) {
	f := newContractFixture(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		r := requestContract(t, f.server.Handler(), "GET", path, "", nil)
		if r.Code != 200 {
			t.Fatalf("healthy %s: %d", path, r.Code)
		}
		if r.Header().Get("X-Content-Type-Options") != "nosniff" || r.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatal("missing response headers")
		}
	}
	original := f.server.client
	f.server.client = failingListClient{original}
	r := requestContract(t, f.server.Handler(), "GET", "/readyz", "", nil)
	if r.Code != 503 || decodeContract[map[string]any](t, r)["component"] != "kubernetes" {
		t.Fatal("Kubernetes failure must fail readiness")
	}
	f.server.client = original
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	r = requestContract(t, f.server.Handler(), "GET", "/readyz", "", nil)
	if r.Code != 503 || decodeContract[map[string]any](t, r)["component"] != "database" {
		t.Fatal("database failure must fail readiness")
	}
	for _, path := range []string{"/preview/policy", "/preview/override", "/policies", "/overrides", "/namespace-groups", "/notification-channels"} {
		req := httptest.NewRequest("POST", "/api/v1"+path, bytes.NewBufferString("{"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+f.token(t, auth.RoleAdmin))
		r := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(r, req)
		if r.Code != 400 {
			t.Errorf("malformed %s: %d", path, r.Code)
		}
	}
}

func TestHTTPPreviewDoesNotPersistAndTargetsFilter(t *testing.T) {
	target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "dev-api", Namespace: "aura-system", Labels: map[string]string{"power.aura.sh/target-namespace": "dev", "power.aura.sh/target-name": "api"}}, Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "dev", Name: "api", Kind: "Deployment"}}, Status: v1alpha1.PowerTargetStatus{DesiredState: "off"}}
	f := newContractFixture(t, target)
	token := f.token(t, auth.RoleMember)
	for _, path := range []string{"/preview/policy", "/preview/override"} {
		r := requestContract(t, f.server.Handler(), "POST", "/api/v1"+path, token, map[string]any{"scope": map[string]any{"namespaces": []string{"dev"}}, "schedule": map[string]string{"desiredState": "off"}, "state": "off"})
		if r.Code != 200 {
			t.Fatal(r.Code)
		}
		if decodeContract[map[string]int](t, requestContract(t, f.server.Handler(), "POST", "/api/v1/preview/policy", token, map[string]any{"scope": map[string]any{"namespaces": []string{"dev"}}, "schedule": map[string]string{"desiredState": "off"}}))["totalAffected"] != 1 {
			t.Fatal("namespace preview count")
		}
	}
	var policies v1alpha1.PowerPolicyList
	var overrides v1alpha1.PowerOverrideList
	if e := f.client.List(context.Background(), &policies); e != nil {
		t.Fatal(e)
	}
	if e := f.client.List(context.Background(), &overrides); e != nil {
		t.Fatal(e)
	}
	if len(policies.Items) != 0 || len(overrides.Items) != 0 {
		t.Fatal("preview mutated resources")
	}
	for _, query := range []struct {
		q string
		n int
	}{{"?namespace=dev&state=off", 1}, {"?namespace=prod", 0}, {"?state=on", 0}} {
		r := requestContract(t, f.server.Handler(), "GET", "/api/v1/targets"+query.q, token, nil)
		if r.Code != 200 || decodeContract[map[string]any](t, r)["count"] != float64(query.n) {
			t.Errorf("filter %s: %s", query.q, r.Body.String())
		}
	}
}
