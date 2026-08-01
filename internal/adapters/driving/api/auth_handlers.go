package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
)

// AuthHandlers holds auth-related HTTP handlers.
type AuthHandlers struct {
	store      auth.Store
	jwtService *auth.JWTService
}

// NewAuthHandlers creates auth handlers.
func NewAuthHandlers(store auth.Store, jwtService *auth.JWTService) *AuthHandlers {
	return &AuthHandlers{store: store, jwtService: jwtService}
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
		pending.POST("/:id/approve", h.handleApprove)
		pending.POST("/:id/reject", h.handleReject)
	}
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
	maxAge := int(h.jwtService.AccessTokenTTL().Seconds())
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("aura_session", tokens.AccessToken, maxAge, "/", "", false, true)
	// Set refresh token in a separate long-lived cookie
	refreshMaxAge := int(h.jwtService.RefreshTokenTTL().Seconds())
	c.SetCookie("aura_refresh", tokens.RefreshToken, refreshMaxAge, "/api/v1/auth", "", false, true)

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
	maxAge := int(h.jwtService.AccessTokenTTL().Seconds())
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("aura_session", tokens.AccessToken, maxAge, "/", "", false, true)
	refreshMaxAge := int(h.jwtService.RefreshTokenTTL().Seconds())
	c.SetCookie("aura_refresh", tokens.RefreshToken, refreshMaxAge, "/api/v1/auth", "", false, true)

	c.JSON(http.StatusOK, tokens)
}

func (h *AuthHandlers) handleLogout(c *gin.Context) {
	// Clear session cookies
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("aura_session", "", -1, "/", "", false, true)
	c.SetCookie("aura_refresh", "", -1, "/api/v1/auth", "", false, true)
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

func (h *AuthHandlers) handleApprove(c *gin.Context) {
	id := c.Param("id")
	reviewerID := c.GetString("userID")

	change, err := h.store.ApprovePendingChange(id, reviewerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// TODO: Apply the actual change (create/update/delete the resource)
	// This would call the K8s client to apply the payload

	c.JSON(http.StatusOK, change)
}

func (h *AuthHandlers) handleReject(c *gin.Context) {
	id := c.Param("id")
	reviewerID := c.GetString("userID")

	change, err := h.store.RejectPendingChange(id, reviewerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, change)
}
