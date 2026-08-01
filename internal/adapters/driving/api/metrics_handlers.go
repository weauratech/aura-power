package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/weauratech/aura-power/internal/ports"
)

// RegisterMetricsRoutes adds metrics endpoints to the router.
func (s *Server) RegisterMetricsRoutes(metricsProvider ports.MetricsProvider, costProvider ports.CostProvider) {
	s.metricsProvider = metricsProvider
	s.costProvider = costProvider

	api := s.router.Group("/api/v1/metrics")
	{
		api.GET("/cluster", s.handleClusterMetrics)
		api.GET("/namespace/:namespace", s.handleNamespaceMetrics)
		api.GET("/workload/:namespace/:name", s.handleWorkloadMetrics)
		api.GET("/nodes", s.handleNodeMetrics)
		api.GET("/cost", s.handleCostSummary)
	}
}

func (s *Server) handleClusterMetrics(c *gin.Context) {
	if s.metricsProvider == nil || !s.metricsProvider.IsAvailable(c.Request.Context()) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "metrics provider not available",
			"message": "Configure Prometheus or metrics-server in the Helm values to enable metrics.",
		})
		return
	}

	rangeStr := c.DefaultQuery("range", "24h")
	r := ports.ParseRange(rangeStr)

	metrics, err := s.metricsProvider.GetClusterMetrics(c.Request.Context(), r)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

func (s *Server) handleNamespaceMetrics(c *gin.Context) {
	if s.metricsProvider == nil || !s.metricsProvider.IsAvailable(c.Request.Context()) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "metrics provider not available"})
		return
	}

	namespace := c.Param("namespace")
	rangeStr := c.DefaultQuery("range", "24h")
	r := ports.ParseRange(rangeStr)

	metrics, err := s.metricsProvider.GetNamespaceMetrics(c.Request.Context(), namespace, r)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

func (s *Server) handleWorkloadMetrics(c *gin.Context) {
	if s.metricsProvider == nil || !s.metricsProvider.IsAvailable(c.Request.Context()) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "metrics provider not available"})
		return
	}

	namespace := c.Param("namespace")
	name := c.Param("name")
	rangeStr := c.DefaultQuery("range", "24h")
	r := ports.ParseRange(rangeStr)

	metrics, err := s.metricsProvider.GetWorkloadMetrics(c.Request.Context(), namespace, name, r)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

func (s *Server) handleNodeMetrics(c *gin.Context) {
	if s.metricsProvider == nil || !s.metricsProvider.IsAvailable(c.Request.Context()) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "metrics provider not available"})
		return
	}

	rangeStr := c.DefaultQuery("range", "24h")
	r := ports.ParseRange(rangeStr)

	metrics, err := s.metricsProvider.GetNodeMetrics(c.Request.Context(), r)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

func (s *Server) handleCostSummary(c *gin.Context) {
	if s.costProvider == nil || !s.costProvider.IsAvailable(c.Request.Context()) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "cost provider not available",
			"message": "Configure OpenCost in the Helm values to enable cost data.",
		})
		return
	}

	summary, err := s.costProvider.GetCostSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, summary)
}
