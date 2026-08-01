package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CachedClientConfig holds configuration for the cached client.
type CachedClientConfig struct {
	// RestConfig is the Kubernetes REST configuration.
	RestConfig *rest.Config
	// Scheme is the runtime scheme with registered types.
	Scheme *runtime.Scheme
	// SyncTimeout is how long to wait for initial cache sync.
	SyncTimeout time.Duration
}

// CachedClient implements sigs.k8s.io/controller-runtime/pkg/client.Client
// by delegating reads to an informer cache and writes to a direct API client.
// This avoids per-request API calls for the server binary while keeping
// write operations strongly consistent.
type CachedClient struct {
	cache       cache.Cache
	writeClient client.Client
	scheme      *runtime.Scheme
}

// NewCachedClient creates a client that reads from an informer cache and
// writes directly to the Kubernetes API.
func NewCachedClient(ctx context.Context, cfg CachedClientConfig) (*CachedClient, error) {
	if cfg.SyncTimeout == 0 {
		cfg.SyncTimeout = 60 * time.Second
	}

	// Create the informer cache (watches all registered types)
	informerCache, err := cache.New(cfg.RestConfig, cache.Options{
		Scheme: cfg.Scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create informer cache: %w", err)
	}

	// Create a direct client for writes
	directClient, err := client.New(cfg.RestConfig, client.Options{
		Scheme: cfg.Scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create direct client: %w", err)
	}

	// Start the cache in a background goroutine
	go func() {
		slog.Info("starting informer cache")
		if err := informerCache.Start(ctx); err != nil {
			slog.Error("informer cache error", "error", err)
		}
	}()

	// Wait for cache to sync
	syncCtx, syncCancel := context.WithTimeout(ctx, cfg.SyncTimeout)
	defer syncCancel()

	if !informerCache.WaitForCacheSync(syncCtx) {
		return nil, fmt.Errorf("informer cache sync timed out after %v", cfg.SyncTimeout)
	}
	slog.Info("informer cache synced")

	return &CachedClient{
		cache:       informerCache,
		writeClient: directClient,
		scheme:      cfg.Scheme,
	}, nil
}

// Client returns itself as a controller-runtime client.Client interface.
func (c *CachedClient) Client() client.Client {
	return c
}

// IsReady returns true if the informer cache is synced.
func (c *CachedClient) IsReady(ctx context.Context) bool {
	return c.cache.WaitForCacheSync(ctx)
}

// --- Read operations (from cache) ---

func (c *CachedClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	return c.cache.Get(ctx, key, obj, opts...)
}

func (c *CachedClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return c.cache.List(ctx, list, opts...)
}

// --- Write operations (direct to API) ---

func (c *CachedClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return c.writeClient.Create(ctx, obj, opts...)
}

func (c *CachedClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return c.writeClient.Update(ctx, obj, opts...)
}

func (c *CachedClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return c.writeClient.Patch(ctx, obj, patch, opts...)
}

func (c *CachedClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return c.writeClient.Delete(ctx, obj, opts...)
}

func (c *CachedClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return c.writeClient.DeleteAllOf(ctx, obj, opts...)
}

// --- Status sub-resource ---

func (c *CachedClient) Status() client.SubResourceWriter {
	return c.writeClient.Status()
}

// --- Sub-resource client ---

func (c *CachedClient) SubResource(subResource string) client.SubResourceClient {
	return c.writeClient.SubResource(subResource)
}

// --- Scheme and RESTMapper ---

func (c *CachedClient) Scheme() *runtime.Scheme {
	return c.scheme
}

func (c *CachedClient) RESTMapper() meta.RESTMapper {
	return c.writeClient.RESTMapper()
}

func (c *CachedClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return c.writeClient.GroupVersionKindFor(obj)
}

func (c *CachedClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return c.writeClient.IsObjectNamespaced(obj)
}
