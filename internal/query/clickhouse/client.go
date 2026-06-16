package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const (
	maxResponseBytes = 50 << 20 // 50 MB
	// datasourceType is the Grafana plugin ID that owns ClickHouse datasources.
	datasourceType = "grafana-clickhouse-datasource"
	// formatTable corresponds to the ClickHouse plugin's table query format.
	// 0 = time series, 1 = table, 2 = logs, 3 = trace.
	formatTable = 1
)

// Client executes ClickHouse SQL queries via Grafana's datasource proxy.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new ClickHouse query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{
		restConfig: cfg,
		httpClient: httpClient,
	}, nil
}

// Query executes a SQL query against the specified ClickHouse datasource.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	query := map[string]any{
		"refId": "A",
		"datasource": map[string]any{
			"type": datasourceType,
			"uid":  datasourceUID,
		},
		"rawSql":        req.SQL,
		"format":        formatTable,
		"queryType":     "table",
		"intervalMs":    60000,
		"maxDataPoints": 100,
	}

	from, to := "now-5m", "now"
	if req.IsRange() {
		from = strconv.FormatInt(req.Start.UnixMilli(), 10)
		to = strconv.FormatInt(req.End.UnixMilli(), 10)
	}

	body, err := json.Marshal(map[string]any{
		"queries": []any{query},
		"from":    from,
		"to":      to,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respBody, statusCode, err := c.postQuery(ctx, body)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, queryerror.FromBody("clickhouse", "query", statusCode, respBody)
	}

	var envelope grafanaQueryResponse
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result, ok := envelope.Results["A"]
	if !ok {
		return &QueryResponse{}, nil
	}
	if result.Error != "" {
		status := result.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		return nil, queryerror.New("clickhouse", "query", status, result.Error, result.ErrorSource)
	}
	if len(result.Frames) == 0 {
		return &QueryResponse{}, nil
	}
	frame := result.Frames[0]
	return &frame, nil
}

// postQuery POSTs the encoded body to the K8s query endpoint first and falls
// back to /api/ds/query on 404 (older Grafana, or stacks where the K8s API is
// not registered for this plugin). Returns the response body and HTTP status.
func (c *Client) postQuery(ctx context.Context, body []byte) ([]byte, int, error) {
	respBody, statusCode, err := c.do(ctx, c.buildK8sQueryPath(), body)
	if err != nil {
		return nil, 0, err
	}
	if statusCode == http.StatusNotFound {
		respBody, statusCode, err = c.do(ctx, "/api/ds/query", body)
		if err != nil {
			return nil, 0, err
		}
	}
	return respBody, statusCode, nil
}

func (c *Client) do(ctx context.Context, apiPath string, body []byte) ([]byte, int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

func (c *Client) buildK8sQueryPath() string {
	return fmt.Sprintf("/apis/query.grafana.app/v0alpha1/namespaces/%s/query", c.restConfig.Namespace)
}
