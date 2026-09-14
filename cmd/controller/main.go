package main

import (
	"context"
	"crypto/tls"
	"os"
	"strconv"
	"strings"
	"time"

	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	crwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/kubernetes"
	"github.com/weauratech/aura-power/internal/adapters/driven/notifications"
	"github.com/weauratech/aura-power/internal/adapters/driven/observability"
	"github.com/weauratech/aura-power/internal/adapters/driving/background"
	"github.com/weauratech/aura-power/internal/adapters/driving/reconciler"
	admissionwebhook "github.com/weauratech/aura-power/internal/adapters/driving/webhook"
	"github.com/weauratech/aura-power/internal/core/domain"
)

var scheme = runtime.NewScheme()

var (
	version = "dev"
	commit  = "unknown"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(extensionsv1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	opts := zap.Options{Development: os.Getenv("DEV_MODE") == "true"}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("setup")
	log.Info("starting aura-power-controller", "version", version, "commit", commit)

	// Configuration
	guardrailConfig := domain.DefaultGuardrailConfig()
	if configured := os.Getenv("SYSTEM_NAMESPACES"); configured != "" {
		guardrailConfig.SystemNamespaces = splitAndTrim(configured)
	} else if extra := os.Getenv("EXTRA_SYSTEM_NAMESPACES"); extra != "" {
		guardrailConfig.SystemNamespaces = append(guardrailConfig.SystemNamespaces, splitAndTrim(extra)...)
	}
	leaderElectionID := getEnvOrDefault("LEADER_ELECTION_ID", "aura-power-controller-leader.power.aura.sh")
	leaderElectionEnabled := envBool("LEADER_ELECTION_ENABLED", true)
	controlNamespace := getEnvOrDefault("CONTROL_NAMESPACE", "aura-system")
	controlCache := cache.ByObject{Namespaces: map[string]cache.Config{controlNamespace: {}}}

	managerOptions := ctrl.Options{
		Scheme:                 scheme,
		LeaderElection:         leaderElectionEnabled,
		LeaderElectionID:       leaderElectionID,
		HealthProbeBindAddress: ":8081",
		Cache: cache.Options{ByObject: map[client.Object]cache.ByObject{
			&v1alpha1.PowerTarget{}:               controlCache,
			&v1alpha1.PowerPolicy{}:               controlCache,
			&v1alpha1.PowerOverride{}:             controlCache,
			&v1alpha1.PowerSchedule{}:             controlCache,
			&v1alpha1.PowerNamespaceGroup{}:       controlCache,
			&v1alpha1.PowerNotificationChannel{}:  controlCache,
			&v1alpha1.PowerNotificationDelivery{}: controlCache,
			&v1alpha1.PowerAuditEvent{}:           controlCache,
		}},
	}
	webhookEnabled := envBool("WEBHOOK_ENABLED", false)
	if webhookEnabled {
		managerOptions.WebhookServer = crwebhook.NewServer(crwebhook.Options{
			Port:    envInt("WEBHOOK_PORT", 9443),
			CertDir: getEnvOrDefault("WEBHOOK_CERT_DIR", "/tmp/k8s-webhook-server/serving-certs"),
			TLSOpts: []func(*tls.Config){func(config *tls.Config) {
				config.MinVersion = tls.VersionTLS12
			}},
		})
	}

	// Create manager. The webhook server is lazy when disabled, so existing
	// installations do not require certificates until admission is opted in.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), managerOptions)
	if err != nil {
		log.Error(err, "unable to create manager")
		os.Exit(1)
	}
	if webhookEnabled {
		admissionwebhook.Register(mgr.GetWebhookServer(), mgr.GetScheme())
		log.Info("validation webhooks enabled")
	} else {
		log.Info("validation webhooks disabled")
	}

	// Create driven adapters
	k8sClient := mgr.GetClient()
	executor := kubernetes.NewExecutor(k8sClient)
	auditRecorder := kubernetes.NewAuditRecorderWithReader(k8sClient, mgr.GetAPIReader(), mgr.GetEventRecorderFor("aura-power"), controlNamespace)
	metricsExporter := observability.NewPrometheusExporter()

	// Create notification dispatcher
	notifDispatcher := notifications.NewDispatcherForNamespace(k8sClient, mgr.GetAPIReader(), controlNamespace)
	auditRecorder.SetNotifier(notifDispatcher)
	if err := notifDispatcher.SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create notification delivery controller")
		os.Exit(1)
	}

	// Register reconcilers
	targetReconciler := &reconciler.TargetReconciler{
		Client:           k8sClient,
		ControlNamespace: controlNamespace,
		APIReader:        mgr.GetAPIReader(),
		Config:           guardrailConfig,
		Executor:         executor,
		Audit:            auditRecorder,
		Metrics:          metricsExporter,
		RequeueAfter:     durationEnv("RECONCILIATION_INTERVAL", 30*time.Second),
	}
	if err := targetReconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "PowerTarget")
		os.Exit(1)
	}

	policyReconciler := &reconciler.PolicyReconciler{
		Client:           k8sClient,
		Audit:            auditRecorder,
		ControlNamespace: controlNamespace,
	}
	if err := policyReconciler.SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "PowerPolicy")
		os.Exit(1)
	}

	overrideReconciler := &reconciler.OverrideReconciler{
		Client:           k8sClient,
		Audit:            auditRecorder,
		ControlNamespace: controlNamespace,
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
	if err := mgr.AddReadyzCheck("notification-suppression-v1", notificationSuppressionSchemaCheck(mgr.GetAPIReader())); err != nil {
		log.Error(err, "unable to register notification suppression capability")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("notification-outbox-v1", notificationOutboxSchemaCheck(mgr.GetAPIReader())); err != nil {
		log.Error(err, "unable to register durable notification outbox capability")
		os.Exit(1)
	}
	if webhookEnabled {
		if err := mgr.AddReadyzCheck("webhook", mgr.GetWebhookServer().StartedChecker()); err != nil {
			log.Error(err, "unable to set up webhook ready check")
			os.Exit(1)
		}
	}

	// Start audit cleanup (background)
	ctx := ctrl.SetupSignalHandler()
	if err := startPprofServer(ctx, os.Getenv("PPROF_BIND_ADDRESS")); err != nil {
		log.Error(err, "unable to start optional pprof server")
		os.Exit(1)
	}
	go runAuditCleanup(ctx, auditRecorder)

	// Start discovery loop (as manager runnable — starts after cache is synced)
	// Discovery uses direct API reads so cluster-wide workload lists do not
	// create long-lived informer caches for every Deployment/StatefulSet/CronJob.
	discoverer := kubernetes.NewDiscoverer(mgr.GetAPIReader())
	// Configure built-in schedule timezone
	if tz := os.Getenv("BUILTIN_SCHEDULE_TIMEZONE"); tz != "" {
		background.DefaultTimezone = tz
	}

	discoveryLoop := &background.DiscoveryLoop{
		Client:     k8sClient,
		Discoverer: discoverer,
		Executor:   executor,
		Audit:      auditRecorder,
		Config: background.DiscoveryConfig{
			Interval:              durationEnv("DISCOVERY_INTERVAL", 60*time.Second),
			Namespace:             controlNamespace,
			SystemNamespaces:      guardrailConfig.SystemNamespaces,
			OptInAnnotation:       guardrailConfig.OptInAnnotation,
			ExemptAnnotation:      guardrailConfig.ExemptAnnotation,
			ArgoTrackingLabelKeys: splitAndTrim(os.Getenv("ARGO_TRACKING_LABEL_KEYS")),
		},
	}
	if err := mgr.Add(discoveryLoop); err != nil {
		log.Error(err, "unable to add discovery loop")
		os.Exit(1)
	}

	// Start manager (blocking)
	log.Info("starting aura-power-controller", "leaderElection", leaderElectionEnabled, "leaderElectionID", leaderElectionID, "controlNamespace", controlNamespace)
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
		if parsed, err := time.ParseDuration(v); err == nil && parsed > 0 {
			cleanupInterval = parsed
		}
	}
	deliveryRetentionDays := 30
	if v := os.Getenv("NOTIFICATION_DELIVERY_RETENTION_DAYS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			deliveryRetentionDays = parsed
		}
	}
	if deliveryRetentionDays < retentionDays {
		deliveryRetentionDays = retentionDays
	}

	log := ctrl.Log.WithName("audit-cleanup")
	log.Info("audit retention configured", "retentionDays", retentionDays, "deliveryRetentionDays", deliveryRetentionDays, "cleanupInterval", cleanupInterval)

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := recorder.CleanupExpiredWithDeliveryRetention(ctx, retentionDays, deliveryRetentionDays)
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

func envBool(key string, defaultValue bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func envInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 && parsed <= 65535 {
			return parsed
		}
	}
	return defaultValue
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
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
