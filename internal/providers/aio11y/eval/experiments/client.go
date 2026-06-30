package experiments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

const basePath = "/eval/experiments"
const testSuitesBasePath = "/eval/test-suites"
const testCaseTrialsBasePath = "/eval/test-case-trials"

// ErrNotFound is returned by per-run methods (Get, Update, Cancel, GetReport)
// when the server responds with 404 so callers can distinguish a missing run
// from other API errors.
var ErrNotFound = errors.New("experiment not found")

// Client wraps the AI Observability plugin proxy with experiment-specific endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new experiments client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns experiments, paginated. Pass 0 for no limit.
func (c *Client) List(ctx context.Context, limit int) ([]Experiment, error) {
	return aio11yhttp.ListAll[Experiment](ctx, c.base, basePath, nil, limit)
}

// ListSuites returns test suites, paginated. Pass 0 for no limit.
func (c *Client) ListSuites(ctx context.Context, limit int) ([]TestSuite, error) {
	return aio11yhttp.ListAll[TestSuite](ctx, c.base, testSuitesBasePath, nil, limit)
}

// GetSuite returns a single test suite with its versions.
func (c *Client) GetSuite(ctx context.Context, suiteID string) (*TestSuite, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, testSuitesBasePath+"/"+url.PathEscape(suiteID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get test suite %s: %w", suiteID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", suiteID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var suite TestSuite
	if err := json.NewDecoder(resp.Body).Decode(&suite); err != nil {
		return nil, fmt.Errorf("failed to decode test suite response: %w", err)
	}
	return &suite, nil
}

// CreateSuite creates a new test suite.
func (c *Client) CreateSuite(ctx context.Context, suite *TestSuite) (*TestSuite, error) {
	body, err := json.Marshal(suite)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create suite request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, testSuitesBasePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create test suite: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var created TestSuite
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode test suite response: %w", err)
	}
	return &created, nil
}

// UpdateSuite patches a test suite.
func (c *Client) UpdateSuite(ctx context.Context, suiteID string, req *UpdateTestSuiteRequest) (*TestSuite, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal update suite request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPatch, testSuitesBasePath+"/"+url.PathEscape(suiteID), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update test suite %s: %w", suiteID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", suiteID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var suite TestSuite
	if err := json.NewDecoder(resp.Body).Decode(&suite); err != nil {
		return nil, fmt.Errorf("failed to decode test suite response: %w", err)
	}
	return &suite, nil
}

type UpdateTestSuiteRequest struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

type CreateTestSuiteVersionRequest struct {
	Changelog  string `json:"changelog,omitempty"`
	EmptyDraft bool   `json:"empty_draft,omitempty"`
}

// CreateSuiteVersion creates a draft version for a suite.
func (c *Client) CreateSuiteVersion(ctx context.Context, suiteID string, req *CreateTestSuiteVersionRequest) (*TestSuiteVersion, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create suite version request: %w", err)
	}

	path := testSuitesBasePath + "/" + url.PathEscape(suiteID) + "/versions"
	resp, err := c.base.DoRequest(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create test suite version for %s: %w", suiteID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var version TestSuiteVersion
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		return nil, fmt.Errorf("failed to decode test suite version response: %w", err)
	}
	return &version, nil
}

// PublishSuiteVersion publishes a draft suite version.
func (c *Client) PublishSuiteVersion(ctx context.Context, suiteID, version string) (*TestSuiteVersion, error) {
	escapedVersion := strings.ReplaceAll(url.PathEscape(version), ":", "%3A")
	path := testSuitesBasePath + "/" + url.PathEscape(suiteID) + "/versions/" + escapedVersion + ":publish"
	resp, err := c.base.DoRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to publish test suite version %s/%s: %w", suiteID, version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s/%s: %w", suiteID, version, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var published TestSuiteVersion
	if err := json.NewDecoder(resp.Body).Decode(&published); err != nil {
		return nil, fmt.Errorf("failed to decode test suite version response: %w", err)
	}
	return &published, nil
}

func testCasesPath(suiteID, version string) string {
	return testSuitesBasePath + "/" + url.PathEscape(suiteID) + "/versions/" + url.PathEscape(version) + "/test-cases"
}

// ListCases returns test cases for a suite version.
func (c *Client) ListCases(ctx context.Context, suiteID, version string, limit int) ([]TestCase, error) {
	return aio11yhttp.ListAll[TestCase](ctx, c.base, testCasesPath(suiteID, version), nil, limit)
}

// GetCase returns a single test case.
func (c *Client) GetCase(ctx context.Context, suiteID, version, testCaseID string) (*TestCase, error) {
	path := testCasesPath(suiteID, version) + "/" + url.PathEscape(testCaseID)
	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get test case %s: %w", testCaseID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", testCaseID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var tc TestCase
	if err := json.NewDecoder(resp.Body).Decode(&tc); err != nil {
		return nil, fmt.Errorf("failed to decode test case response: %w", err)
	}
	return &tc, nil
}

// UpsertCase creates or replaces a test case in a mutable suite version.
func (c *Client) UpsertCase(ctx context.Context, suiteID, version string, tc *TestCase) (*TestCase, error) {
	body, err := json.Marshal(tc)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal test case request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, testCasesPath(suiteID, version), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to upsert test case: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var out TestCase
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode test case response: %w", err)
	}
	return &out, nil
}

// PatchCase patches a test case in a mutable suite version.
func (c *Client) PatchCase(ctx context.Context, suiteID, version, testCaseID string, patch map[string]any) (*TestCase, error) {
	body, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal test case patch: %w", err)
	}

	path := testCasesPath(suiteID, version) + "/" + url.PathEscape(testCaseID)
	resp, err := c.base.DoRequest(ctx, http.MethodPatch, path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to patch test case %s: %w", testCaseID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", testCaseID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var out TestCase
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode test case response: %w", err)
	}
	return &out, nil
}

// DeleteCase deletes a test case from a mutable suite version.
func (c *Client) DeleteCase(ctx context.Context, suiteID, version, testCaseID string) error {
	path := testCasesPath(suiteID, version) + "/" + url.PathEscape(testCaseID)
	resp, err := c.base.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete test case %s: %w", testCaseID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w", testCaseID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return aio11yhttp.HandleErrorResponse(resp)
	}
	return nil
}

// Get returns a single experiment by run ID.
func (c *Client) Get(ctx context.Context, runID string) (*Experiment, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, basePath+"/"+url.PathEscape(runID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get experiment %s: %w", runID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", runID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var exp Experiment
	if err := json.NewDecoder(resp.Body).Decode(&exp); err != nil {
		return nil, fmt.Errorf("failed to decode experiment response: %w", err)
	}
	return &exp, nil
}

// ListTrials returns test case trials for an experiment.
func (c *Client) ListTrials(ctx context.Context, experimentID string, limit int) ([]TestCaseTrial, error) {
	path := basePath + "/" + url.PathEscape(experimentID) + "/trials"
	return aio11yhttp.ListAll[TestCaseTrial](ctx, c.base, path, nil, limit)
}

// CreateTrial creates or upserts a test case trial for an experiment.
func (c *Client) CreateTrial(ctx context.Context, experimentID string, trial *TestCaseTrial) (*TestCaseTrial, error) {
	body, err := json.Marshal(trial)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create trial request: %w", err)
	}

	path := basePath + "/" + url.PathEscape(experimentID) + "/trials"
	resp, err := c.base.DoRequest(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create trial for experiment %s: %w", experimentID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var out TestCaseTrial
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode test case trial response: %w", err)
	}
	return &out, nil
}

// GetTrial returns a single test case trial.
func (c *Client) GetTrial(ctx context.Context, trialID string) (*TestCaseTrial, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, testCaseTrialsBasePath+"/"+url.PathEscape(trialID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get trial %s: %w", trialID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", trialID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var out TestCaseTrial
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode test case trial response: %w", err)
	}
	return &out, nil
}

// UpdateTrial patches a single test case trial.
func (c *Client) UpdateTrial(ctx context.Context, trialID string, req *UpdateTrialRequest) (*TestCaseTrial, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal update trial request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPatch, testCaseTrialsBasePath+"/"+url.PathEscape(trialID), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update trial %s: %w", trialID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", trialID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var out TestCaseTrial
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode test case trial response: %w", err)
	}
	return &out, nil
}

// ListTrialScores returns scores associated with a test case trial.
func (c *Client) ListTrialScores(ctx context.Context, trialID string, limit int) ([]ScoreItem, error) {
	path := testCaseTrialsBasePath + "/" + url.PathEscape(trialID) + "/scores"
	return aio11yhttp.ListAll[ScoreItem](ctx, c.base, path, nil, limit)
}

// ListTrialArtifacts returns artifacts associated with a test case trial.
func (c *Client) ListTrialArtifacts(ctx context.Context, trialID string, limit int) ([]Artifact, error) {
	path := testCaseTrialsBasePath + "/" + url.PathEscape(trialID) + "/artifacts"
	return aio11yhttp.ListAll[Artifact](ctx, c.base, path, nil, limit)
}

// Create creates a new experiment.
func (c *Client) Create(ctx context.Context, exp *Experiment) (*Experiment, error) {
	body, err := json.Marshal(exp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, basePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create experiment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var created Experiment
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode experiment response: %w", err)
	}
	return &created, nil
}

// Update sends a partial PATCH against an existing experiment.
func (c *Client) Update(ctx context.Context, runID string, req *UpdateRequest) (*Experiment, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal update request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPatch, basePath+"/"+url.PathEscape(runID), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update experiment %s: %w", runID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", runID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var exp Experiment
	if err := json.NewDecoder(resp.Body).Decode(&exp); err != nil {
		return nil, fmt.Errorf("failed to decode experiment response: %w", err)
	}
	return &exp, nil
}

// Cancel transitions a running experiment to a canceled state.
//
// The plugin proxy matches the `:cancel` suffix on the run ID segment
// (single-segment path), not `/cancel`. url.PathEscape does not escape
// `:` (it's an allowed sub-delim in path segments), which would make the
// route ambiguous if a runID ever contained a literal colon, so we escape
// it manually before appending the action suffix.
func (c *Client) Cancel(ctx context.Context, runID string) error {
	escaped := strings.ReplaceAll(url.PathEscape(runID), ":", "%3A")
	resp, err := c.base.DoRequest(ctx, http.MethodPost, basePath+"/"+escaped+":cancel", nil)
	if err != nil {
		return fmt.Errorf("failed to cancel experiment %s: %w", runID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w", runID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusAccepted {
		return aio11yhttp.HandleErrorResponse(resp)
	}
	return nil
}

// ListScores returns scores associated with a single experiment run.
func (c *Client) ListScores(ctx context.Context, runID string, limit int) ([]ScoreItem, error) {
	path := basePath + "/" + url.PathEscape(runID) + "/scores"
	return aio11yhttp.ListAll[ScoreItem](ctx, c.base, path, nil, limit)
}

// GetReport returns the aggregate report for an experiment run.
func (c *Client) GetReport(ctx context.Context, runID string) (*ExperimentReport, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, basePath+"/"+url.PathEscape(runID)+"/report", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get experiment report %s: %w", runID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", runID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var report ExperimentReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, fmt.Errorf("failed to decode experiment report: %w", err)
	}
	return &report, nil
}
