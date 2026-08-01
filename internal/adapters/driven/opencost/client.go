package opencost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/weauratech/aura-power/internal/ports"
)

// Config holds OpenCost connection configuration.
type Config struct {
	URL string
}

// Client implements CostProvider via OpenCost API.
type Client struct {
	config     Config
	httpClient *http.Client
}

func NewClient(config Config) *Client {
	return &Client{
		config:     config,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) IsAvailable(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", c.config.URL+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Client) GetCostSummary(ctx context.Context) (*ports.CostSummary, error) {
	// Query OpenCost allocation API for last 1h window
	reqURL := fmt.Sprintf("%s/allocation/compute?window=1h&aggregate=namespace&accumulate=false", c.config.URL)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencost request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencost returned %d: %s", resp.StatusCode, string(body))
	}

	var ocResp openCostResponse
	if err := json.NewDecoder(resp.Body).Decode(&ocResp); err != nil {
		return nil, fmt.Errorf("failed to decode opencost response: %w", err)
	}

	summary := &ports.CostSummary{
		CostByNamespace: make(map[string]float64),
	}

	for _, allocationSet := range ocResp.Data {
		for ns, alloc := range allocationSet {
			cost := alloc.TotalCost
			summary.CostByNamespace[ns] = cost
			summary.TotalClusterCostPerHour += cost
		}
	}

	return summary, nil
}

func (c *Client) GetWorkloadCostPerHour(ctx context.Context, namespace, name string) (float64, error) {
	reqURL := fmt.Sprintf("%s/allocation/compute?window=1h&aggregate=controller&filterNamespaces=%s&accumulate=false", c.config.URL, namespace)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("opencost returned %d", resp.StatusCode)
	}

	var ocResp openCostResponse
	if err := json.NewDecoder(resp.Body).Decode(&ocResp); err != nil {
		return 0, err
	}

	for _, allocationSet := range ocResp.Data {
		for controller, alloc := range allocationSet {
			if controller == namespace+"/"+name || controller == name {
				return alloc.TotalCost, nil
			}
		}
	}

	return 0, nil
}

// OpenCost response types
type openCostResponse struct {
	Code int                        `json:"code"`
	Data []map[string]openCostAlloc `json:"data"`
}

type openCostAlloc struct {
	Name       string  `json:"name"`
	CPUCost    float64 `json:"cpuCost"`
	MemoryCost float64 `json:"ramCost"`
	TotalCost  float64 `json:"totalCost"`
}
