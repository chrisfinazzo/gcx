package datasources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

const (
	maxResponseBytes = 10 << 20 // 10 MB

	datasourcesPath      = "/api/datasources"
	datasourceByUIDPath  = "/api/datasources/uid/"
	datasourceByNamePath = "/api/datasources/name/"
)

// Datasource holds the fields exchanged with the legacy Grafana datasource REST
// API. The same struct is used for reads (list/get) and writes (create/update),
// so write-only and optional fields carry omitempty to keep manifests clean.
//
// secureJsonData is write-only: the API never returns it on reads, so it is
// absent from pulled manifests but honored on push when present.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Datasource struct {
	UID             string            `json:"uid,omitempty"`
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	URL             string            `json:"url,omitempty"`
	Access          string            `json:"access,omitempty"`
	Database        string            `json:"database,omitempty"`
	User            string            `json:"user,omitempty"`
	IsDefault       bool              `json:"isDefault,omitempty"`
	ReadOnly        bool              `json:"readOnly,omitempty"`
	BasicAuth       bool              `json:"basicAuth,omitempty"`
	BasicAuthUser   string            `json:"basicAuthUser,omitempty"`
	WithCredentials bool              `json:"withCredentials,omitempty"`
	JSONData        map[string]any    `json:"jsonData,omitempty"`
	SecureJSONData  map[string]string `json:"secureJsonData,omitempty"`
}

// GetResourceName returns the datasource UID, which serves as the stable
// resource identity (metadata.name) in the resources pipeline.
func (d Datasource) GetResourceName() string { return d.UID }

// SetResourceName restores the UID from metadata.name after a round-trip.
func (d *Datasource) SetResourceName(name string) { d.UID = name }

// Client queries Grafana datasources via the NamespacedRESTConfig
// transport, ensuring OAuth proxy mode and token refresh are respected.
// It mirrors the approach used by internal/query/prometheus and internal/query/loki.
type Client struct {
	host       string
	httpClient *http.Client
}

// NewClient creates a client backed by the given REST config's
// transport (including WrapTransport / RefreshTransport in OAuth proxy mode).
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{
		host:       cfg.Host,
		httpClient: httpClient,
	}, nil
}

// List returns all datasources visible to the authenticated user.
func (c *Client) List(ctx context.Context) ([]*Datasource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+datasourcesPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list datasources: %w", err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewAPIError("list datasources", "", resp.StatusCode, body)
	}

	var datasources []*Datasource
	if err := json.Unmarshal(body, &datasources); err != nil {
		return nil, fmt.Errorf("failed to parse datasources response: %w", err)
	}

	return datasources, nil
}

// GetByUID returns the datasource with the given UID.
func (c *Client) GetByUID(ctx context.Context, uid string) (*Datasource, error) {
	return c.get(ctx, datasourceByUIDPath+url.PathEscape(uid), uid)
}

// GetByName returns the datasource with the given display name.
func (c *Client) GetByName(ctx context.Context, name string) (*Datasource, error) {
	return c.get(ctx, datasourceByNamePath+url.PathEscape(name), name)
}

// mutationResponse is the envelope returned by the legacy create/update
// endpoints, e.g. {"datasource": {...}, "id": 1, "message": "Datasource added"}.
type mutationResponse struct {
	Datasource *Datasource `json:"datasource"`
}

// Create creates a new datasource via POST /api/datasources and returns the
// persisted datasource as echoed back by the API.
func (c *Client) Create(ctx context.Context, ds *Datasource) (*Datasource, error) {
	return c.write(ctx, http.MethodPost, c.host+datasourcesPath, "create datasource", ds.UID, ds)
}

// Update updates the datasource identified by uid via PUT
// /api/datasources/uid/{uid} and returns the persisted datasource.
func (c *Client) Update(ctx context.Context, uid string, ds *Datasource) (*Datasource, error) {
	return c.write(ctx, http.MethodPut, c.host+datasourceByUIDPath+url.PathEscape(uid), "update datasource", uid, ds)
}

// Delete removes the datasource identified by uid via DELETE
// /api/datasources/uid/{uid}.
func (c *Client) Delete(ctx context.Context, uid string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.host+datasourceByUIDPath+url.PathEscape(uid), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete datasource: %w", err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBytes)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return NewAPIError("delete datasource", uid, resp.StatusCode, body)
	}

	return nil
}

func (c *Client) write(ctx context.Context, method, endpoint, operation, identifier string, ds *Datasource) (*Datasource, error) {
	payload, err := json.Marshal(ds)
	if err != nil {
		return nil, fmt.Errorf("failed to encode datasource: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to %s: %w", operation, err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewAPIError(operation, identifier, resp.StatusCode, body)
	}

	var wrapped mutationResponse
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", operation, err)
	}
	if wrapped.Datasource == nil {
		return nil, fmt.Errorf("%s response did not include a datasource", operation)
	}

	return wrapped.Datasource, nil
}

func (c *Client) get(ctx context.Context, path, identifier string) (*Datasource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get datasource: %w", err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewAPIError("get datasource", identifier, resp.StatusCode, body)
	}

	var ds Datasource
	if err := json.Unmarshal(body, &ds); err != nil {
		return nil, fmt.Errorf("failed to parse datasource response: %w", err)
	}

	return &ds, nil
}
