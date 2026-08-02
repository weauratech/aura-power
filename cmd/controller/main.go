package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/kubernetes"
	"github.com/weauratech/aura-power/internal/adapters/driven/observability"
	"github.com/weauratech/aura-power/internal/adapters/driving/background"
	"github.com/weauratech/aura-power/internal/adapters/driving/reconciler"
	"github.com/weauratech/aura-power/internal/core/domain"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	opts := zap.Options{Development: os.Getenv("DEV_MODE") == "true"}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("setup")

	// Configuration
	guardrailConfig := domain.DefaultGuardrailConfig()
	// Allow overriding system namespaces via env (comma-separated)
	if extra := os.Getenv("EXTRA_SYSTEM_NAMESPACES"); extra != "" {
		for _, ns := range splitAndTrim(extra) {
			guardrailConfig.SystemNamespaces = append(guardrailConfig.SystemNamespaces, ns)
		}
	}
	leaderElectionID := getEnvOrDefault("LEADER_ELECTION_ID", "aura-power-controller-leader.power.aura.sh")

	// Create manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		LeaderElection:         true,
		LeaderElectionID:       leaderElectionID,
		HealthProbeBindAddress: ":8081",
	})
	if err != nil {
		log.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Create driven adapters
	k8sClient := mgr.GetClient()
	executor := kubernetes.NewExecutor(k8sClient)
	auditRecorder := kubernetes.NewAuditRecorder(k8sClient, mgr.GetEventRecorderFor("aura-power"), "aura-system")
	metricsExporter := observability.NewPrometheusExporter()

	// Register reconcilers
	targetReconciler := &reconciler.TargetReconciler{
		Client:   k8sClient,
		Config:   guardrailConfig,
		Executor: executor,
		Audit:    auditRecorder,
		Metrics:  metricsExporter,
	}
	if err := targetReconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "PowerTarget")
		os.Exit(1)
	}

	policyReconciler := &reconciler.PolicyReconciler{
		Client: k8sClient,
		Audit:  auditRecorder,
	}
	if err := policyReconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "PowerPolicy")
		os.Exit(1)
	}

	overrideReconciler := &reconciler.OverrideReconciler{
		Client: k8sClient,
		Audit:  auditRecorder,
	}
	if err := overrideReconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "PowerOverride")
		os.Exit(1)
	}

	// Health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Start audit cleanup (background)
	ctx := ctrl.SetupSignalHandler()
	go runAuditCleanup(ctx, auditRecorder)

	// Start discovery loop (as manager runnable — starts after cache is synced)
	discoverer := kubernetes.NewDiscoverer(k8sClient)
	discoveryLoop := &background.DiscoveryLoop{
		Client:     k8sClient,
		Discoverer: discoverer,
		Config: background.DiscoveryConfig{
			Interval:         60 * time.Second,
			Namespace:        "aura-system",
			SystemNamespaces: guardrailConfig.SystemNamespaces,
			OptInAnnotation:  guardrailConfig.OptInAnnotation,
			ExemptAnnotation: guardrailConfig.ExemptAnnotation,
		},
	}
	if err := mgr.Add(discoveryLoop); err != nil {
		log.Error(err, "unable to add discovery loop")
		os.Exit(1)
	}

	// Start manager (blocking)
	log.Info("starting aura-power-controller", "leaderElection", leaderElectionID)
	if err := mgr.Start(ctx); err != nil {
		log.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func runAuditCleanup(ctx context.Context, recorder *kubernetes.AuditRecorder) {
	retentionDays := 7
	if v := os.Getenv("AUDIT_RETENTION_DAYS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			retentionDays = parsed
		}
	}

	cleanupInterval := 6 * time.Hour
	if v := os.Getenv("AUDIT_CLEANUP_INTERVAL"); v != "" {
		if parsed, err := time.ParseDuration(v); err == nil {
			cleanupInterval = parsed
		}
	}

	log := ctrl.Log.WithName("audit-cleanup")
	log.Info("audit retention configured", "retentionDays", retentionDays, "cleanupInterval", cleanupInterval)

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := recorder.CleanupExpired(ctx, retentionDays)
			if err != nil {
				log.Error(err, "cleanup failed")
			} else if deleted > 0 {
				log.Info("cleaned expired events", "deleted", deleted, "olderThanDays", retentionDays)
			}
		}
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
