package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/weauratech/aura-power/internal/ports"
)

// Config holds Prometheus connection configuration.
type Config struct {
	URL         string
	BearerToken string
	BasicUser   string
	BasicPass   string
}

// Client implements MetricsProvider via Prometheus PromQL.
type Client struct {
	config     Config
	httpClient *http.Client
}

func NewClient(config Config) *Client {
	return &Client{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Name() string { return "prometheus" }

func (c *Client) IsAvailable(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", c.config.URL+"/-/healthy", nil)
	if err != nil {
		return false
	}
	c.addAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Client) GetClusterMetrics(ctx context.Context, r ports.MetricsRange) (*ports.ClusterMetrics, error) {
	end := time.Now()
	start := end.Add(-r.Duration)

	cpuUsage, err := c.queryRange(ctx, `sum(rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m]))`, start, end, r.Step)
	if err != nil {
		return nil, fmt.Errorf("cpu usage query: %w", err)
	}

	cpuCap, err := c.queryRange(ctx, `sum(kube_node_status_allocatable{resource="cpu"})`, start, end, r.Step)
	if err != nil {
		return nil, fmt.Errorf("cpu capacity query: %w", err)
	}

	memUsage, err := c.queryRange(ctx, `sum(container_memory_working_set_bytes{container!="",container!="POD"})`, start, end, r.Step)
	if err != nil {
		return nil, fmt.Errorf("memory usage query: %w", err)
	}

	memCap, err := c.queryRange(ctx, `sum(kube_node_status_allocatable{resource="memory"})`, start, end, r.Step)
	if err != nil {
		return nil, fmt.Errorf("memory capacity query: %w", err)
	}

	nodeCount, err := c.queryRange(ctx, `count(kube_node_info)`, start, end, r.Step)
	if err != nil {
		return nil, fmt.Errorf("node count query: %w", err)
	}

	cpuRequested, _ := c.queryRange(ctx, `sum(kube_pod_container_resource_requests{resource="cpu"})`, start, end, r.Step)
	memRequested, _ := c.queryRange(ctx, `sum(kube_pod_container_resource_requests{resource="memory"})`, start, end, r.Step)

	return &ports.ClusterMetrics{
		CPUUsage:        cpuUsage,
		CPUCapacity:     cpuCap,
		CPURequested:    cpuRequested,
		MemoryUsage:     memUsage,
		MemoryCapacity:  memCap,
		MemoryRequested: memRequested,
		NodeCount:       nodeCount,
	}, nil
}

func (c *Client) GetNamespaceMetrics(ctx context.Context, namespace string, r ports.MetricsRange) (*ports.NamespaceMetrics, error) {
	end := time.Now()
	start := end.Add(-r.Duration)

	cpuUsage, err := c.queryRange(ctx, fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{container!="",namespace="%s"}[5m]))`, namespace), start, end, r.Step)
	if err != nil {
		return nil, err
	}

	cpuReq, err := c.queryRange(ctx, fmt.Sprintf(`sum(kube_pod_container_resource_requests{resource="cpu",namespace="%s"}) or sum(cluster:namespace:pod_cpu:active:kube_pod_container_resource_requests{namespace="%s"})`, namespace, namespace), start, end, r.Step)
	if err != nil {
		cpuReq = nil // non-critical, continue without
	}

	memUsage, err := c.queryRange(ctx, fmt.Sprintf(`sum(container_memory_working_set_bytes{container!="",namespace="%s"})`, namespace), start, end, r.Step)
	if err != nil {
		return nil, err
	}

	memReq, err := c.queryRange(ctx, fmt.Sprintf(`sum(kube_pod_container_resource_requests{resource="memory",namespace="%s"}) or sum(cluster:namespace:pod_memory:active:kube_pod_container_resource_requests{namespace="%s"})`, namespace, namespace), start, end, r.Step)
	if err != nil {
		memReq = nil // non-critical
	}

	return &ports.NamespaceMetrics{
		Namespace:       namespace,
		CPUUsage:        cpuUsage,
		CPURequested:    cpuReq,
		MemoryUsage:     memUsage,
		MemoryRequested: memReq,
	}, nil
}

func (c *Client) GetWorkloadMetrics(ctx context.Context, namespace, name string, r ports.MetricsRange) (*ports.WorkloadMetrics, error) {
	end := time.Now()
	start := end.Add(-r.Duration)

	podSelector := fmt.Sprintf(`namespace="%s",pod=~"%s-.*"`, namespace, name)

	cpuUsage, err := c.queryRange(ctx, fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{container!="",%s}[5m]))`, podSelector), start, end, r.Step)
	if err != nil {
		return nil, err
	}

	memUsage, err := c.queryRange(ctx, fmt.Sprintf(`sum(container_memory_working_set_bytes{container!="",%s})`, podSelector), start, end, r.Step)
	if err != nil {
		return nil, err
	}

	return &ports.WorkloadMetrics{
		Namespace:   namespace,
		Name:        name,
		CPUUsage:    cpuUsage,
		MemoryUsage: memUsage,
	}, nil
}

func (c *Client) GetNodeMetrics(ctx context.Context, r ports.MetricsRange) (*ports.NodeMetrics, error) {
	end := time.Now()
	start := end.Add(-r.Duration)

	nodeCount, err := c.queryRange(ctx, `count(kube_node_info)`, start, end, r.Step)
	if err != nil {
		return nil, err
	}

	return &ports.NodeMetrics{
		NodeCount: nodeCount,
	}, nil
}

// queryRange executes a PromQL range query and returns samples.
func (c *Client) queryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]ports.MetricsSample, error) {
	params := url.Values{
		"query": {query},
		"start": {strconv.FormatInt(start.Unix(), 10)},
		"end":   {strconv.FormatInt(end.Unix(), 10)},
		"step":  {step.String()},
	}

	reqURL := fmt.Sprintf("%s/api/v1/query_range?%s", c.config.URL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	c.addAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, string(body))
	}

	var promResp promResponse
	if err := json.NewDecoder(resp.Body).Decode(&promResp); err != nil {
		return nil, fmt.Errorf("failed to decode prometheus response: %w", err)
	}

	if promResp.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed: %s", promResp.Error)
	}

	return parseSamples(promResp), nil
}

func (c *Client) addAuth(req *http.Request) {
	if c.config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.BearerToken)
	}
	if c.config.BasicUser != "" {
		req.SetBasicAuth(c.config.BasicUser, c.config.BasicPass)
	}
}

// Prometheus response types
type promResponse struct {
	Status string   `json:"status"`
	Error  string   `json:"error"`
	Data   promData `json:"data"`
}

type promData struct {
	ResultType string       `json:"resultType"`
	Result     []promResult `json:"result"`
}

type promResult struct {
	Metric map[string]string `json:"metric"`
	Values [][]interface{}   `json:"values"`
}

func parseSamples(resp promResponse) []ports.MetricsSample {
	var samples []ports.MetricsSample

	for _, result := range resp.Data.Result {
		for _, v := range result.Values {
			if len(v) != 2 {
				continue
			}
			ts, ok := v[0].(float64)
			if !ok {
				continue
			}
			valStr, ok := v[1].(string)
			if !ok {
				continue
			}
			val, err := strconv.ParseFloat(valStr, 64)
			if err != nil {
				continue
			}
			samples = append(samples, ports.MetricsSample{
				Timestamp: time.Unix(int64(ts), 0),
				Value:     val,
			})
		}
	}

	return samples
}
