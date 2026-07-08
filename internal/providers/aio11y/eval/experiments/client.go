package experiments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
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

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, marshalErr, requestErr, notFoundID string, okStatuses ...int) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", marshalErr, err)
		}
		body = bytes.NewReader(encoded)
	}

	resp, err := c.base.DoRequest(ctx, method, path, body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", requestErr, err)
	}

	if resp.StatusCode == http.StatusNotFound && notFoundID != "" {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %w", notFoundID, ErrNotFound)
	}
	if !statusAllowed(resp.StatusCode, okStatuses...) {
		defer resp.Body.Close()
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}
	return resp, nil
}

func doDecode[T any](ctx context.Context, client *Client, method, path string, payload any, marshalErr, requestErr, notFoundID, decodeErr string, okStatuses ...int) (*T, error) {
	resp, err := client.doJSON(ctx, method, path, payload, marshalErr, requestErr, notFoundID, okStatuses...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: %w", decodeErr, err)
	}
	return &out, nil
}

func statusAllowed(status int, okStatuses ...int) bool {
	return slices.Contains(okStatuses, status)
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
	return doDecode[TestSuite](ctx, c, http.MethodGet, testSuitesBasePath+"/"+url.PathEscape(suiteID), nil,
		"", "failed to get test suite "+suiteID, suiteID, "failed to decode test suite response", http.StatusOK)
}

// CreateSuite creates a new test suite.
func (c *Client) CreateSuite(ctx context.Context, suite *TestSuite) (*TestSuite, error) {
	return doDecode[TestSuite](ctx, c, http.MethodPost, testSuitesBasePath, suite,
		"failed to marshal create suite request", "failed to create test suite", "", "failed to decode test suite response", http.StatusOK, http.StatusCreated)
}

// UpdateSuite patches a test suite.
func (c *Client) UpdateSuite(ctx context.Context, suiteID string, req *UpdateTestSuiteRequest) (*TestSuite, error) {
	return doDecode[TestSuite](ctx, c, http.MethodPatch, testSuitesBasePath+"/"+url.PathEscape(suiteID), req,
		"failed to marshal update suite request", "failed to update test suite "+suiteID, suiteID, "failed to decode test suite response", http.StatusOK)
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
	path := testSuitesBasePath + "/" + url.PathEscape(suiteID) + "/versions"
	return doDecode[TestSuiteVersion](ctx, c, http.MethodPost, path, req,
		"failed to marshal create suite version request", "failed to create test suite version for "+suiteID, "", "failed to decode test suite version response", http.StatusOK, http.StatusCreated)
}

// PublishSuiteVersion publishes a draft suite version.
func (c *Client) PublishSuiteVersion(ctx context.Context, suiteID, version string) (*TestSuiteVersion, error) {
	escapedVersion := strings.ReplaceAll(url.PathEscape(version), ":", "%3A")
	path := testSuitesBasePath + "/" + url.PathEscape(suiteID) + "/versions/" + escapedVersion + ":publish"
	return doDecode[TestSuiteVersion](ctx, c, http.MethodPost, path, nil,
		"", "failed to publish test suite version "+suiteID+"/"+version, suiteID+"/"+version, "failed to decode test suite version response", http.StatusOK)
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
	return doDecode[TestCase](ctx, c, http.MethodGet, path, nil,
		"", "failed to get test case "+testCaseID, testCaseID, "failed to decode test case response", http.StatusOK)
}

// UpsertCase creates or replaces a test case in a mutable suite version.
func (c *Client) UpsertCase(ctx context.Context, suiteID, version string, tc *TestCase) (*TestCase, error) {
	return doDecode[TestCase](ctx, c, http.MethodPost, testCasesPath(suiteID, version), tc,
		"failed to marshal test case request", "failed to upsert test case", "", "failed to decode test case response", http.StatusOK, http.StatusCreated)
}

// PatchCase patches a test case in a mutable suite version.
func (c *Client) PatchCase(ctx context.Context, suiteID, version, testCaseID string, patch map[string]any) (*TestCase, error) {
	path := testCasesPath(suiteID, version) + "/" + url.PathEscape(testCaseID)
	return doDecode[TestCase](ctx, c, http.MethodPatch, path, patch,
		"failed to marshal test case patch", "failed to patch test case "+testCaseID, testCaseID, "failed to decode test case response", http.StatusOK)
}

// DeleteCase deletes a test case from a mutable suite version.
func (c *Client) DeleteCase(ctx context.Context, suiteID, version, testCaseID string) error {
	path := testCasesPath(suiteID, version) + "/" + url.PathEscape(testCaseID)
	resp, err := c.doJSON(ctx, http.MethodDelete, path, nil,
		"", "failed to delete test case "+testCaseID, testCaseID, http.StatusOK, http.StatusNoContent)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Get returns a single experiment by run ID.
func (c *Client) Get(ctx context.Context, runID string) (*Experiment, error) {
	return doDecode[Experiment](ctx, c, http.MethodGet, basePath+"/"+url.PathEscape(runID), nil,
		"", "failed to get experiment "+runID, runID, "failed to decode experiment response", http.StatusOK)
}

// ListTrials returns test case trials for an experiment.
func (c *Client) ListTrials(ctx context.Context, experimentID string, limit int) ([]TestCaseTrial, error) {
	path := basePath + "/" + url.PathEscape(experimentID) + "/trials"
	return aio11yhttp.ListAll[TestCaseTrial](ctx, c.base, path, nil, limit)
}

// CreateTrial creates or upserts a test case trial for an experiment.
func (c *Client) CreateTrial(ctx context.Context, experimentID string, trial *TestCaseTrial) (*TestCaseTrial, error) {
	path := basePath + "/" + url.PathEscape(experimentID) + "/trials"
	return doDecode[TestCaseTrial](ctx, c, http.MethodPost, path, trial,
		"failed to marshal create trial request", "failed to create trial for experiment "+experimentID, "", "failed to decode test case trial response", http.StatusOK, http.StatusCreated)
}

// GetTrial returns a single test case trial.
func (c *Client) GetTrial(ctx context.Context, trialID string) (*TestCaseTrial, error) {
	return doDecode[TestCaseTrial](ctx, c, http.MethodGet, testCaseTrialsBasePath+"/"+url.PathEscape(trialID), nil,
		"", "failed to get trial "+trialID, trialID, "failed to decode test case trial response", http.StatusOK)
}

// UpdateTrial patches a single test case trial.
func (c *Client) UpdateTrial(ctx context.Context, trialID string, req *UpdateTrialRequest) (*TestCaseTrial, error) {
	return doDecode[TestCaseTrial](ctx, c, http.MethodPatch, testCaseTrialsBasePath+"/"+url.PathEscape(trialID), req,
		"failed to marshal update trial request", "failed to update trial "+trialID, trialID, "failed to decode test case trial response", http.StatusOK)
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
	return doDecode[Experiment](ctx, c, http.MethodPost, basePath, exp,
		"failed to marshal create request", "failed to create experiment", "", "failed to decode experiment response", http.StatusOK, http.StatusCreated)
}

// Update sends a partial PATCH against an existing experiment.
func (c *Client) Update(ctx context.Context, runID string, req *UpdateRequest) (*Experiment, error) {
	return doDecode[Experiment](ctx, c, http.MethodPatch, basePath+"/"+url.PathEscape(runID), req,
		"failed to marshal update request", "failed to update experiment "+runID, runID, "failed to decode experiment response", http.StatusOK)
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
	return doDecode[ExperimentReport](ctx, c, http.MethodGet, basePath+"/"+url.PathEscape(runID)+"/report", nil,
		"", "failed to get experiment report "+runID, runID, "failed to decode experiment report", http.StatusOK)
}
