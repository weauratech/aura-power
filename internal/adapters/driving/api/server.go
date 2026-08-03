package api

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

// ServerConfig holds configuration for the API server.
type ServerConfig struct {
	Port            string
	GuardrailConfig domain.GuardrailConfig
	CostConfig      domain.CostConfig
	PanelAssets     embed.FS
}

// Server is the Gin HTTP server serving REST API and panel.
type Server struct {
	router          *gin.Engine
	client          client.Client
	metrics         ports.MetricsExporter
	config          ServerConfig
	metricsProvider ports.MetricsProvider
	costProvider    ports.CostProvider
	jwtService      *auth.JWTService
	authStore       auth.Store
}

// NewServer creates a new API server.
func NewServer(c client.Client, metrics ports.MetricsExporter, config ServerConfig) *Server {
	gin.SetMode(gin.ReleaseMode)

	s := &Server{
		router:  gin.New(),
		client:  c,
		metrics: metrics,
		config:  config,
	}

	s.setupMiddleware()
	// Routes are setup lazily after auth is registered (see RegisterAuthRoutes / FinalizeRoutes)

	return s
}

// FinalizeRoutes sets up all API routes. Must be called after RegisterAuthRoutes.
func (s *Server) FinalizeRoutes() {
	s.setupRoutes()
	s.servePanelAssets()
}

func (s *Server) setupMiddleware() {
	s.router.Use(gin.Recovery())
	s.router.Use(securityHeaders())
	s.router.Use(prometheusMiddleware())
	s.router.Use(requestLogger())
}

func (s *Server) setupRoutes() {
	// Health endpoints (no auth)
	s.router.GET("/healthz", s.handleHealthz)
	s.router.GET("/readyz", s.handleReadyz)
	s.router.GET("/metrics", metricsHandler())

	// API v1 group
	api := s.router.Group("/api/v1")

	// Auth endpoints (no middleware — login, refresh, logout must be accessible)
	if s.authStore != nil && s.jwtService != nil {
		authHandlers := NewAuthHandlers(s.authStore, s.jwtService)
		authHandlers.RegisterRoutes(api)
	}

	// Apply auth middleware to remaining API routes
	if s.jwtService != nil {
		api.Use(AuthMiddleware(s.jwtService))
	}

	// Protected auth routes (me, users, pending)
	if s.authStore != nil && s.jwtService != nil {
		authHandlers := NewAuthHandlers(s.authStore, s.jwtService)
		authHandlers.RegisterProtectedRoutes(api)
	}
	{
		api.GET("/status", s.handleStatus)
		api.GET("/dashboard", s.handleDashboard)
		api.GET("/discover", s.handleDiscover)
		api.GET("/targets", s.handleListTargets)
		api.GET("/targets/:namespace/:name/explain", s.handleExplainTarget)
		api.POST("/preview/policy", s.handlePreviewPolicy)
		api.POST("/preview/override", s.handlePreviewOverride)
		api.GET("/savings", s.handleSavings)
		api.GET("/savings/breakdown", s.handleSavingsBreakdown)
		api.GET("/savings/export", s.handleSavingsExport)
		api.GET("/audit", s.handleAuditList)
		api.GET("/audit/export", s.handleAuditExport)
		api.GET("/policies", s.handleListPolicies)
		api.GET("/overrides", s.handleListOverrides)
		api.GET("/namespaces", s.handleListNamespaces)
		api.GET("/namespace-groups", s.handleListNamespaceGroups)
		api.GET("/notification-channels", s.handleListNotificationChannels)
	}

	// Write operations: approver + admin
	if s.jwtService != nil {
		write := api.Group("")
		write.Use(RequireRole(auth.RoleApprover, auth.RoleAdmin))
		{
			write.POST("/policies", s.handleCreatePolicy)
			write.PUT("/policies/:namespace/:name", s.handleUpdatePolicy)
			write.POST("/overrides", s.handleCreateOverride)
			write.POST("/namespace-groups", s.handleCreateNamespaceGroup)
			write.POST("/notification-channels", s.handleCreateNotificationChannel)
		}
		// Delete operations: admin only
		del := api.Group("")
		del.Use(RequireRole(auth.RoleAdmin))
		{
			del.DELETE("/policies/:namespace/:name", s.handleDeletePolicy)
			del.DELETE("/overrides/:namespace/:name", s.handleDeleteOverride)
			del.DELETE("/namespace-groups/:namespace/:name", s.handleDeleteNamespaceGroup)
			del.DELETE("/notification-channels/:namespace/:name", s.handleDeleteNotificationChannel)
		}
	} else {
		// No auth — all routes open
		api.POST("/policies", s.handleCreatePolicy)
		api.PUT("/policies/:namespace/:name", s.handleUpdatePolicy)
		api.DELETE("/policies/:namespace/:name", s.handleDeletePolicy)
		api.POST("/overrides", s.handleCreateOverride)
		api.DELETE("/overrides/:namespace/:name", s.handleDeleteOverride)
		api.POST("/namespace-groups", s.handleCreateNamespaceGroup)
		api.POST("/notification-channels", s.handleCreateNotificationChannel)
		api.DELETE("/namespace-groups/:namespace/:name", s.handleDeleteNamespaceGroup)
		api.DELETE("/notification-channels/:namespace/:name", s.handleDeleteNotificationChannel)
	}

	// Panel assets served via FinalizeRoutes (NoRoute handler)
}

func (s *Server) servePanelAssets() {
	// Try to serve embedded panel assets
	subFS, err := fs.Sub(s.config.PanelAssets, "panelassets")
	if err != nil {
		// Panel not embedded (dev mode) — serve a placeholder
		s.router.GET("/", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "Aura Power API", "panel": "not embedded (dev mode)"})
		})
		return
	}

	// Check if the FS actually has content
	entries, _ := fs.ReadDir(subFS, ".")
	if len(entries) == 0 {
		s.router.GET("/", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "Aura Power API", "panel": "not embedded (empty dist)"})
		})
		return
	}

	fileServer := http.FileServer(http.FS(subFS))
	s.router.NoRoute(func(c *gin.Context) {
		// Try to serve static file first
		path := c.Request.URL.Path
		if _, err := fs.Stat(subFS, path[1:]); err == nil {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		// SPA fallback: serve index.html for client-side routing
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}

// Handler returns the underlying HTTP handler for testing.
func (s *Server) Handler() http.Handler {
	return s.router
}

// Run starts the HTTP server (blocking).
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:    ":" + s.config.Port,
		Handler: s.router,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	return srv.ListenAndServe()
}

// Middleware

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; font-src 'self' https://cdn.jsdelivr.net")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		_ = time.Since(start) // duration available for metrics
	}
}

// RegisterAuthRoutes stores auth service references. Routes are setup in FinalizeRoutes.
func (s *Server) RegisterAuthRoutes(store auth.Store, jwtService *auth.JWTService) {
	s.jwtService = jwtService
	s.authStore = store
}
