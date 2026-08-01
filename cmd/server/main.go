package main

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
	"github.com/weauratech/aura-power/internal/adapters/driven/kubernetes"
	"github.com/weauratech/aura-power/internal/adapters/driven/opencost"
	promclient "github.com/weauratech/aura-power/internal/adapters/driven/prometheus"
	"github.com/weauratech/aura-power/internal/adapters/driving/api"
	"github.com/weauratech/aura-power/internal/core/domain"
)

//go:embed all:panelassets
var panelAssets embed.FS

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	// Structured logging
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	slog.Info("starting aura-power-server", "version", "2.0.0")

	// Context with graceful shutdown (must be created early for cache)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("received shutdown signal", "signal", sig)
		cancel()
	}()

	// Configuration
	port := getEnvOrDefault("API_PORT", "8080")
	prometheusURL := getEnvOrDefault("PROMETHEUS_URL", "")
	opencostURL := getEnvOrDefault("OPENCOST_URL", "")
	dbPath := getEnvOrDefault("AUTH_DB_PATH", "/data/aura-power.db")
	jwtSecret := getEnvOrDefault("JWT_SECRET", "")
	adminUser := getEnvOrDefault("ADMIN_USERNAME", "admin")
	adminPass := getEnvOrDefault("ADMIN_PASSWORD", "")

	if jwtSecret == "" {
		slog.Error("JWT_SECRET is required")
		os.Exit(1)
	}

	// Build K8s config
	var config *rest.Config
	var err error
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		slog.Error("failed to build k8s config", "error", err)
		os.Exit(1)
	}

	// Create cached client (informer-based reads, direct writes)
	cachedClient, err := kubernetes.NewCachedClient(ctx, kubernetes.CachedClientConfig{
		RestConfig: config,
		Scheme:     scheme,
	})
	if err != nil {
		slog.Error("failed to create cached k8s client", "error", err)
		os.Exit(1)
	}
	var k8sClient client.Client = cachedClient

	slog.Info("k8s cached client initialized")

	// Initialize auth store (SQLite)
	authStore, err := auth.NewSQLiteStore(dbPath)
	if err != nil {
		slog.Error("failed to initialize auth store", "error", err, "path", dbPath)
		os.Exit(1)
	}
	slog.Info("auth store initialized", "path", dbPath)

	// JWT service
	jwtService := auth.NewJWTService(auth.JWTConfig{
		SecretKey:       jwtSecret,
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 7 * 24 * time.Hour,
	})

	// Create initial admin if configured (ensure role is always admin on startup)
	if adminPass != "" {
		existing, getErr := authStore.GetUserByUsername(adminUser)
		if getErr != nil {
			// User doesn't exist — create it
			if _, createErr := authStore.CreateUser(adminUser, adminPass, auth.RoleAdmin); createErr == nil {
				slog.Info("created initial admin user", "username", adminUser)
			}
		} else if existing.Role != auth.RoleAdmin {
			// User exists but role changed — force back to admin
			if err := authStore.UpdateUser(existing.ID, auth.RoleAdmin); err == nil {
				slog.Info("restored admin role for initial admin user", "username", adminUser)
			}
		}
	}

	// Domain config
	guardrailConfig := domain.DefaultGuardrailConfig()
	costConfig := domain.DefaultCostConfig()

	// Create API server
	apiServer := api.NewServer(k8sClient, nil, api.ServerConfig{
		Port:            port,
		GuardrailConfig: guardrailConfig,
		CostConfig:      costConfig,
		PanelAssets:     panelAssets,
	})

	// Register auth (mandatory in v2.0)
	apiServer.RegisterAuthRoutes(authStore, jwtService)
	slog.Info("auth enabled")

	// Register metrics providers (optional)
	var promProvider *promclient.Client
	if prometheusURL != "" {
		promProvider = promclient.NewClient(promclient.Config{URL: prometheusURL})
		slog.Info("prometheus provider configured", "url", prometheusURL)
	}

	var costDataProvider *opencost.Client
	if opencostURL != "" {
		costDataProvider = opencost.NewClient(opencost.Config{URL: opencostURL})
		slog.Info("opencost provider configured", "url", opencostURL)
	}

	if promProvider != nil || costDataProvider != nil {
		apiServer.RegisterMetricsRoutes(promProvider, costDataProvider)
	}

	// Finalize routes (must be after auth + metrics registration)
	apiServer.FinalizeRoutes()

	// Start server (blocking until context is cancelled)
	slog.Info("starting HTTP server", "port", port)
	if err := apiServer.Run(ctx); err != nil {
		if ctx.Err() == nil {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}

	slog.Info("server stopped")
	fmt.Println("aura-power-server shutdown complete")
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
