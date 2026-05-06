package synth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/providers/synth/checks"
)

// ListChecksOptions narrows the result set of ListChecksFiltered.
//
// Server-side filters (added in synthetic-monitoring-api v1.6.0):
//   - Search:       case-insensitive substring against job and target.
//   - Enabled:      restrict to enabled or disabled checks; nil leaves it unset.
//   - MinFrequency: lower bound on check frequency (inclusive).
//   - MaxFrequency: upper bound on check frequency (inclusive).
//
// WithAlerts toggles the includeAlerts query string. The server rejects 400
// when WithAlerts is combined with any of the filter fields above; callers
// must pick one or the other.
type ListChecksOptions struct {
	Search       string
	Enabled      *bool
	MinFrequency time.Duration
	MaxFrequency time.Duration
	WithAlerts   bool
}

// ErrFiltersWithAlerts is returned when ListChecksOptions has both a filter
// field and WithAlerts set. The synthetic-monitoring-api rejects this combo
// with 400; we surface it before hitting the network.
var ErrFiltersWithAlerts = errors.New("synthetic-monitoring API does not support combining filters (--search, --enabled, --min-frequency, --max-frequency) with --with-alerts")

// hasFilters reports whether any narrowing field is set.
func (o ListChecksOptions) hasFilters() bool {
	return o.Search != "" || o.Enabled != nil || o.MinFrequency > 0 || o.MaxFrequency > 0
}

// query renders the options as a URL query string (without leading '?').
// Empty options return "".
func (o ListChecksOptions) query() string {
	v := url.Values{}
	if o.WithAlerts {
		v.Set("includeAlerts", "true")
	}
	if o.Search != "" {
		v.Set("search", o.Search)
	}
	if o.Enabled != nil {
		v.Set("enabled", strconv.FormatBool(*o.Enabled))
	}
	if o.MinFrequency > 0 {
		v.Set("min_frequency", strconv.FormatInt(o.MinFrequency.Milliseconds(), 10))
	}
	if o.MaxFrequency > 0 {
		v.Set("max_frequency", strconv.FormatInt(o.MaxFrequency.Milliseconds(), 10))
	}
	return v.Encode()
}

// ListChecks returns all SM checks visible through the given datasource.
func (c *Client) ListChecks(ctx context.Context, datasourceUID string) ([]checks.Check, error) {
	return c.ListChecksFiltered(ctx, datasourceUID, ListChecksOptions{})
}

// ListChecksWithAlerts returns all SM checks with their alert rules embedded
// in each Check.Alerts field. Backed by the SM datasource's
// /sm/check/list?includeAlerts=true server-side composition — the same
// endpoint the SM app uses to render the check-list page with alert state.
func (c *Client) ListChecksWithAlerts(ctx context.Context, datasourceUID string) ([]checks.Check, error) {
	return c.ListChecksFiltered(ctx, datasourceUID, ListChecksOptions{WithAlerts: true})
}

// ListChecksFiltered returns SM checks narrowed by the given options. See
// ListChecksOptions for filter semantics. Returns ErrFiltersWithAlerts when
// WithAlerts is combined with any filter field — the synthetic-monitoring-api
// rejects that combination.
func (c *Client) ListChecksFiltered(ctx context.Context, datasourceUID string, opts ListChecksOptions) ([]checks.Check, error) {
	if opts.WithAlerts && opts.hasFilters() {
		return nil, ErrFiltersWithAlerts
	}
	path := "sm/check/list"
	if q := opts.query(); q != "" {
		path += "?" + q
	}
	body, err := c.proxyGet(ctx, datasourceUID, path, "list checks")
	if err != nil {
		return nil, err
	}
	var result []checks.Check
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode checks: %w", err)
	}
	return result, nil
}
