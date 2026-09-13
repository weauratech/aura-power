package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// isSecureRequest returns true if the request arrived over HTTPS
// (either directly or via a reverse proxy setting X-Forwarded-Proto).
func isSecureRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	proto := c.GetHeader("X-Forwarded-Proto")
	return strings.EqualFold(proto, "https")
}

// AuthHandlers holds auth-related HTTP handlers.
type AuthHandlers struct {
	store            auth.Store
	jwtService       *auth.JWTService
	client           client.Client
	controlNamespace string
	mu               sync.Mutex
}

// NewAuthHandlers creates auth handlers.
func NewAuthHandlers(store auth.Store, jwtService *auth.JWTService, c client.Client, namespaces ...string) *AuthHandlers {
	namespace := "aura-system"
	if len(namespaces) > 0 && namespaces[0] != "" {
		namespace = namespaces[0]
	}
	return &AuthHandlers{store: store, jwtService: jwtService, client: c, controlNamespace: namespace}
}

// RegisterRoutes registers auth API endpoints.
func (h *AuthHandlers) RegisterRoutes(router *gin.RouterGroup) {
	// Public routes (no auth needed)
	router.POST("/auth/login", h.handleLogin)
	router.POST("/auth/refresh", h.handleRefresh)
	router.POST("/auth/logout", h.handleLogout)
}

// RegisterProtectedRoutes registers auth routes that require authentication.
func (h *AuthHandlers) RegisterProtectedRoutes(router *gin.RouterGroup) {
	router.GET("/auth/me", h.handleMe)
	router.POST("/pending", h.handleCreatePending)

	// User management (admin only)
	users := router.Group("/users")
	users.Use(RequireRole(auth.RoleAdmin))
	{
		users.GET("", h.handleListUsers)
		users.POST("", h.handleCreateUser)
		users.PUT("/:id", h.handleUpdateUser)
		users.DELETE("/:id", h.handleDeleteUser)
	}

	// Pending changes (approver + admin)
	pending := router.Group("/pending")
	pending.Use(RequireRole(auth.RoleApprover, auth.RoleAdmin))
	{
		pending.GET("", h.handleListPending)
		pending.GET("/:id", h.handleGetPending)
		pending.POST("/:id/approve", h.handleApprove)
		pending.POST("/:id/reject", h.handleReject)
	}
}

type pendingChangeRequest struct {
	Action            string          `json:"action" binding:"required"`
	ResourceKind      string          `json:"resourceKind" binding:"required"`
	ResourceNamespace string          `json:"resourceNamespace"`
	ResourceName      string          `json:"resourceName" binding:"required"`
	ResourceVersion   string          `json:"resourceVersion"`
	Payload           json.RawMessage `json:"payload"`
}

func (h *AuthHandlers) handleCreatePending(c *gin.Context) {
	var req pendingChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pending change: " + err.Error()})
		return
	}
	if req.ResourceNamespace == "" {
		req.ResourceNamespace = h.controlNamespace
	}
	if err := validatePendingRequest(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	change, err := h.store.CreatePendingChange(auth.PendingChange{
		UserID: c.GetString("userID"), Username: c.GetString("username"), Action: req.Action,
		ResourceKind: req.ResourceKind, ResourceNamespace: req.ResourceNamespace,
		ResourceName: req.ResourceName, ResourceVersion: req.ResourceVersion, Payload: string(req.Payload),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist pending change"})
		return
	}
	c.JSON(http.StatusCreated, change)
}

func validatePendingRequest(req pendingChangeRequest) error {
	if req.Action != "create" && req.Action != "update" && req.Action != "delete" {
		return fmt.Errorf("%w: action must be create, update, or delete", auth.ErrInvalidPendingChange)
	}
	if req.ResourceKind != "PowerPolicy" && req.ResourceKind != "PowerOverride" {
		return fmt.Errorf("%w: resourceKind must be PowerPolicy or PowerOverride", auth.ErrInvalidPendingChange)
	}
	if (req.Action == "update" || req.Action == "delete") && req.ResourceVersion == "" {
		return fmt.Errorf("%w: resourceVersion is required for %s", auth.ErrInvalidPendingChange, req.Action)
	}
	if req.Action != "delete" && len(req.Payload) == 0 {
		return fmt.Errorf("%w: payload is required for %s", auth.ErrInvalidPendingChange, req.Action)
	}
	return nil
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *AuthHandlers) handleLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}

	user, err := h.store.GetUserByUsername(req.Username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if !h.store.ValidatePassword(user, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	tokens, err := h.jwtService.GenerateTokens(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	// Set HttpOnly cookie for browser-based access (SPA)
	secure := isSecureRequest(c)
	maxAge := int(h.jwtService.AccessTokenTTL().Seconds())
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("aura_session", tokens.AccessToken, maxAge, "/", "", secure, true)
	// Set refresh token in a separate long-lived cookie
	refreshMaxAge := int(h.jwtService.RefreshTokenTTL().Seconds())
	c.SetCookie("aura_refresh", tokens.RefreshToken, refreshMaxAge, "/api/v1/auth", "", secure, true)

	// Also return JSON body (for CLI and programmatic access)
	c.JSON(http.StatusOK, tokens)
}

func (h *AuthHandlers) handleRefresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	// Try to get refresh token from cookie first, then from body
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		// Fallback to cookie
		cookieToken, cookieErr := c.Cookie("aura_refresh")
		if cookieErr != nil || cookieToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "refreshToken required"})
			return
		}
		req.RefreshToken = cookieToken
	}

	claims, err := h.jwtService.ValidateToken(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}

	user, err := h.store.GetUserByID(claims.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	tokens, err := h.jwtService.GenerateTokens(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	// Update cookies
	secure := isSecureRequest(c)
	maxAge := int(h.jwtService.AccessTokenTTL().Seconds())
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("aura_session", tokens.AccessToken, maxAge, "/", "", secure, true)
	refreshMaxAge := int(h.jwtService.RefreshTokenTTL().Seconds())
	c.SetCookie("aura_refresh", tokens.RefreshToken, refreshMaxAge, "/api/v1/auth", "", secure, true)

	c.JSON(http.StatusOK, tokens)
}

func (h *AuthHandlers) handleLogout(c *gin.Context) {
	// Clear session cookies
	secure := isSecureRequest(c)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("aura_session", "", -1, "/", "", secure, true)
	c.SetCookie("aura_refresh", "", -1, "/api/v1/auth", "", secure, true)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func (h *AuthHandlers) handleMe(c *gin.Context) {
	userID := c.GetString("userID")
	user, err := h.store.GetUserByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": user.ID, "username": user.Username, "role": user.Role})
}

func (h *AuthHandlers) handleListUsers(c *gin.Context) {
	users, err := h.store.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users, "count": len(users)})
}

type createUserRequest struct {
	Username string    `json:"username" binding:"required"`
	Password string    `json:"password" binding:"required,min=6"`
	Role     auth.Role `json:"role" binding:"required"`
}

func (h *AuthHandlers) handleCreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Role != auth.RoleMember && req.Role != auth.RoleApprover && req.Role != auth.RoleAdmin {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be member, approver, or admin"})
		return
	}

	user, err := h.store.CreateUser(req.Username, req.Password, req.Role)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": user.ID, "username": user.Username, "role": user.Role})
}

func (h *AuthHandlers) handleUpdateUser(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Role auth.Role `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Role != auth.RoleMember && req.Role != auth.RoleApprover && req.Role != auth.RoleAdmin {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be member, approver, or admin"})
		return
	}

	if err := h.store.UpdateUser(id, req.Role); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"updated": true})
}

func (h *AuthHandlers) handleDeleteUser(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.DeleteUser(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func (h *AuthHandlers) handleListPending(c *gin.Context) {
	changes, err := h.store.ListPendingChanges()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": changes, "count": len(changes)})
}

func (h *AuthHandlers) handleGetPending(c *gin.Context) {
	change, err := h.store.GetPendingChange(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, change)
}

func (h *AuthHandlers) handleApprove(c *gin.Context) {
	// SQLite is shared by this API process. Serializing this small critical
	// section prevents two reviewers from racing the same Kubernetes mutation.
	// Kubernetes resourceVersion checks and idempotent replays remain the
	// cross-process/restart safety boundary.
	h.mu.Lock()
	defer h.mu.Unlock()

	id := c.Param("id")
	reviewerID := c.GetString("userID")
	reviewer, err := h.requireCurrentReviewer(reviewerID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	change, err := h.store.GetPendingChange(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if change.UserID == reviewerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "requesters cannot approve their own change"})
		return
	}
	change, err = h.store.BeginPendingDecision(id, reviewerID, "approve")
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "pending change already has another decision"})
		return
	}
	decisionOwner := h.decisionOwner(change, reviewer)
	if err := h.applyPendingChange(c.Request.Context(), change); err != nil {
		if errors.Is(err, errPendingMutationNotApplied) {
			_ = h.store.CancelPendingDecision(id, decisionOwner.ID)
		}
		status := http.StatusUnprocessableEntity
		if errors.Is(err, errStalePendingChange) || apierrors.IsAlreadyExists(err) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error(), "status": change.Status})
		return
	}
	if err := h.recordApprovalDecision(c.Request.Context(), change, decisionOwner, true); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "status": change.Status})
		return
	}
	change, err = h.store.FinalizePendingDecision(id, decisionOwner.ID, "approve")
	if err != nil {
		// The Kubernetes operation is intentionally replayable. Returning an
		// error is truthful; a retry after restart observes the intended state
		// and completes the durable approval decision.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "resource applied but approval could not be finalized; retry safely"})
		return
	}

	c.JSON(http.StatusOK, change)
}

func (h *AuthHandlers) handleReject(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := c.Param("id")
	reviewerID := c.GetString("userID")
	reviewer, err := h.requireCurrentReviewer(reviewerID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	change, err := h.store.GetPendingChange(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	change, err = h.store.BeginPendingDecision(id, reviewerID, "reject")
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "pending change already has another decision"})
		return
	}
	decisionOwner := h.decisionOwner(change, reviewer)
	if err := h.recordApprovalDecision(c.Request.Context(), change, decisionOwner, false); err != nil {
		_ = h.store.CancelPendingDecision(id, decisionOwner.ID)
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "status": change.Status})
		return
	}
	change, err = h.store.FinalizePendingDecision(id, decisionOwner.ID, "reject")
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, change)
}

func (h *AuthHandlers) requireCurrentReviewer(userID string) (*auth.User, error) {
	user, err := h.store.GetUserByID(userID)
	if err != nil {
		return nil, errors.New("reviewer account is no longer active")
	}
	if user.Role != auth.RoleApprover && user.Role != auth.RoleAdmin {
		return nil, errors.New("reviewer no longer has approval permission")
	}
	return user, nil
}

func (h *AuthHandlers) decisionOwner(change *auth.PendingChange, current *auth.User) *auth.User {
	if change.ReviewedBy == "" || change.ReviewedBy == current.ID {
		return current
	}
	owner, err := h.store.GetUserByID(change.ReviewedBy)
	if err == nil {
		return owner
	}
	// Keep the durable reviewer identifier even if that account was removed.
	// The immutable audit event remains the source for its historical username.
	return &auth.User{ID: change.ReviewedBy, Username: change.ReviewedBy}
}

func (h *AuthHandlers) recordApprovalDecision(ctx context.Context, change *auth.PendingChange, reviewer *auth.User, approved bool) error {
	decision := "rejected"
	result := "blocked"
	if approved {
		decision = "approved"
		result = "success"
	}
	action := map[string]string{
		"PowerPolicy/create": "policy.created", "PowerPolicy/update": "policy.modified", "PowerPolicy/delete": "policy.deleted",
		"PowerOverride/create": "override.created", "PowerOverride/update": "override.modified", "PowerOverride/delete": "override.deleted",
	}[change.ResourceKind+"/"+change.Action]
	event := &v1alpha1.PowerAuditEvent{
		ObjectMeta: metav1.ObjectMeta{
			Name: "approval-" + change.ID, Namespace: change.ResourceNamespace,
			Labels: map[string]string{"power.aura.sh/action": action, "power.aura.sh/approval-id": change.ID},
		},
		Spec: v1alpha1.PowerAuditEventSpec{
			Timestamp: metav1.NewTime(time.Now()), Action: action, Actor: reviewer.Username,
			Target: v1alpha1.AuditResourceReference{Namespace: change.ResourceNamespace, Name: change.ResourceName, Kind: change.ResourceKind},
			Result: result, Reason: fmt.Sprintf("approval request %s was %s", change.ID, decision),
		},
	}
	if err := h.client.Create(ctx, event); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("record approval audit: %w", err)
		}
		var existing v1alpha1.PowerAuditEvent
		if getErr := h.client.Get(ctx, client.ObjectKeyFromObject(event), &existing); getErr != nil {
			return fmt.Errorf("verify existing approval audit: %w", getErr)
		}
		if existing.Spec.Action != event.Spec.Action || existing.Spec.Result != event.Spec.Result || existing.Spec.Target != event.Spec.Target || existing.Spec.Reason != event.Spec.Reason {
			return errors.New("existing approval audit does not match the durable decision")
		}
	}
	return nil
}

var errStalePendingChange = errors.New("resource changed since the approval request")
var errPendingMutationNotApplied = errors.New("approved mutation was not applied")

func (h *AuthHandlers) applyPendingChange(ctx context.Context, change *auth.PendingChange) error {
	if h.client == nil {
		return errors.New("Kubernetes client is unavailable")
	}
	req := pendingChangeRequest{Action: change.Action, ResourceKind: change.ResourceKind,
		ResourceNamespace: change.ResourceNamespace, ResourceName: change.ResourceName,
		ResourceVersion: change.ResourceVersion, Payload: json.RawMessage(change.Payload)}
	if err := validatePendingRequest(req); err != nil {
		return fmt.Errorf("%w: %w", errPendingMutationNotApplied, err)
	}
	switch change.ResourceKind {
	case "PowerPolicy":
		return applyPendingObject(ctx, h.client, change, &v1alpha1.PowerPolicy{})
	case "PowerOverride":
		return applyPendingObject(ctx, h.client, change, &v1alpha1.PowerOverride{})
	default:
		return fmt.Errorf("%w: unsupported resource kind %q", auth.ErrInvalidPendingChange, change.ResourceKind)
	}
}

func applyPendingObject(ctx context.Context, c client.Client, change *auth.PendingChange, desired client.Object) error {
	key := client.ObjectKey{Namespace: change.ResourceNamespace, Name: change.ResourceName}
	live := desired.DeepCopyObject().(client.Object)
	err := c.Get(ctx, key, live)

	switch change.Action {
	case "delete":
		if apierrors.IsNotFound(err) {
			return nil // replay after a successful delete
		}
		if err != nil {
			return fmt.Errorf("%w: read resource before delete: %w", errPendingMutationNotApplied, err)
		}
		if live.GetResourceVersion() != change.ResourceVersion {
			return fmt.Errorf("%w: %w", errPendingMutationNotApplied, errStalePendingChange)
		}
		if err := c.Delete(ctx, live, client.Preconditions{ResourceVersion: &change.ResourceVersion}); err != nil {
			fresh := desired.DeepCopyObject().(client.Object)
			if readErr := c.Get(ctx, key, fresh); apierrors.IsNotFound(readErr) {
				return nil
			} else if readErr == nil && fresh.GetResourceVersion() == change.ResourceVersion {
				return fmt.Errorf("%w: delete approved resource: %w", errPendingMutationNotApplied, err)
			}
			return fmt.Errorf("delete result is uncertain; retry the same approval: %w", err)
		}
		return nil
	case "create", "update":
		decoder := json.NewDecoder(bytes.NewBufferString(change.Payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(desired); err != nil {
			return fmt.Errorf("%w: decode approved payload: %w", errPendingMutationNotApplied, err)
		}
		if desired.GetNamespace() == "" {
			desired.SetNamespace(change.ResourceNamespace)
		}
		if desired.GetName() != change.ResourceName || desired.GetNamespace() != change.ResourceNamespace {
			return fmt.Errorf("%w: payload identity does not match pending change", errPendingMutationNotApplied)
		}
		if change.Action == "create" {
			if apierrors.IsNotFound(err) {
				desired.SetResourceVersion("")
				clearPendingObjectStatus(desired)
				if err := c.Create(ctx, desired); err != nil {
					fresh := desired.DeepCopyObject().(client.Object)
					if readErr := c.Get(ctx, key, fresh); readErr == nil && pendingObjectEqual(fresh, desired) {
						return nil
					} else if apierrors.IsNotFound(readErr) {
						return fmt.Errorf("%w: create approved resource: %w", errPendingMutationNotApplied, err)
					}
					return fmt.Errorf("create result is uncertain; retry the same approval: %w", err)
				}
				return nil
			}
			if err != nil {
				return fmt.Errorf("%w: read resource before create: %w", errPendingMutationNotApplied, err)
			}
			if pendingObjectEqual(live, desired) {
				return nil // replay after create succeeded but SQLite did not finalize
			}
			return fmt.Errorf("%w: %w", errPendingMutationNotApplied, apierrors.NewAlreadyExists(schema.GroupResource{Group: v1alpha1.GroupVersion.Group, Resource: strings.ToLower(change.ResourceKind) + "s"}, change.ResourceName))
		}
		if err != nil {
			return fmt.Errorf("%w: read resource before update: %w", errPendingMutationNotApplied, err)
		}
		if pendingObjectEqual(live, desired) {
			return nil // replay after update succeeded but SQLite did not finalize
		}
		if live.GetResourceVersion() != change.ResourceVersion {
			return fmt.Errorf("%w: %w", errPendingMutationNotApplied, errStalePendingChange)
		}
		applyPendingObjectSpec(live, desired)
		if err := c.Update(ctx, live); err != nil {
			fresh := desired.DeepCopyObject().(client.Object)
			if readErr := c.Get(ctx, key, fresh); readErr == nil && pendingObjectEqual(fresh, desired) {
				return nil
			} else if readErr == nil && fresh.GetResourceVersion() == change.ResourceVersion {
				return fmt.Errorf("%w: update approved resource: %w", errPendingMutationNotApplied, err)
			}
			return fmt.Errorf("update result is uncertain; retry the same approval: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("%w: %w: unsupported action %q", errPendingMutationNotApplied, auth.ErrInvalidPendingChange, change.Action)
	}
}

func applyPendingObjectSpec(live, desired client.Object) {
	switch l := live.(type) {
	case *v1alpha1.PowerPolicy:
		l.Spec = desired.(*v1alpha1.PowerPolicy).Spec
	case *v1alpha1.PowerOverride:
		l.Spec = desired.(*v1alpha1.PowerOverride).Spec
	}
}

func clearPendingObjectStatus(object client.Object) {
	switch item := object.(type) {
	case *v1alpha1.PowerPolicy:
		item.Status = v1alpha1.PowerPolicyStatus{}
	case *v1alpha1.PowerOverride:
		item.Status = v1alpha1.PowerOverrideStatus{}
	}
}

func pendingObjectEqual(live, desired client.Object) bool {
	switch l := live.(type) {
	case *v1alpha1.PowerPolicy:
		d, ok := desired.(*v1alpha1.PowerPolicy)
		return ok && apiequality.Semantic.DeepEqual(l.Spec, d.Spec)
	case *v1alpha1.PowerOverride:
		d, ok := desired.(*v1alpha1.PowerOverride)
		return ok && apiequality.Semantic.DeepEqual(l.Spec, d.Spec)
	default:
		return false
	}
}
