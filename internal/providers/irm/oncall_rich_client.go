package irm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/grafana/gcx/internal/providers/irm/oncalltypes"
)

// teamsCache is a per-OnCallClient lazy cache of team-id → team-name.
// Populated on first use of resolveTeams; the OnCallClient instance is
// discarded between commands so this cache is naturally per-command.
type teamsCache struct {
	once sync.Once
	m    map[string]string
	err  error
}

// teamsCaches keeps a teams cache per *OnCallClient so the cache lifetime
// matches the client. Callers should use resolveTeams.
var teamsCacheMu sync.Mutex //nolint:gochecknoglobals

//nolint:gochecknoglobals // package-private map keyed by client pointer.
var teamsCacheByClient = map[*OnCallClient]*teamsCache{}

// resolveTeams returns a map[teamID]→teamName, fetching the OnCall teams list
// at most once per client lifetime. Errors are sticky.
func (c *OnCallClient) resolveTeams(ctx context.Context) (map[string]string, error) {
	teamsCacheMu.Lock()
	cache, ok := teamsCacheByClient[c]
	if !ok {
		cache = &teamsCache{}
		teamsCacheByClient[c] = cache
	}
	teamsCacheMu.Unlock()

	cache.once.Do(func() {
		teams, err := c.ListTeams(ctx)
		if err != nil {
			cache.err = err
			return
		}
		m := make(map[string]string, len(teams))
		for _, t := range teams {
			m[t.ID] = t.Name
		}
		cache.m = m
	})
	return cache.m, cache.err
}

// GetAlertGroupRich fetches an alert group from the internal API and returns
// the rich AlertGroupRich plus the resource name (PK) and metadata timestamp
// (started_at) for the caller to assemble the K8s envelope.
func (c *OnCallClient) GetAlertGroupRich(ctx context.Context, id string) (*alertGroupAPI, *oncalltypes.AlertGroupRich, error) {
	resp, err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s%s/", alertGroupsPath, url.PathEscape(id)), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("irm: get alert group: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, fmt.Errorf("irm: alert group %q not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, handleErrorResponse(resp)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("irm: read alert group: %w", err)
	}

	var api alertGroupAPI
	if err := json.Unmarshal(body, &api); err != nil {
		return nil, nil, fmt.Errorf("irm: decode alert group: %w", err)
	}

	teams, terr := c.resolveTeams(ctx)
	if terr != nil {
		// Non-fatal: leave team name unresolved and continue.
		teams = nil
	}

	rich := buildAlertGroupRich(&api, teams)
	return &api, rich, nil
}

// listAlertGroupRich parses a single AlertGroup item from a list endpoint
// response (no last_alert.raw_request_data, so all extracted fields stay empty).
func listAlertGroupRichFromBytes(item json.RawMessage, teams map[string]string) (*alertGroupAPI, *oncalltypes.AlertGroupRich, error) {
	var api alertGroupAPI
	if err := json.Unmarshal(item, &api); err != nil {
		return nil, nil, err
	}
	rich := buildAlertGroupRich(&api, teams)
	return &api, rich, nil
}

// buildAlertGroupRich folds an alertGroupAPI plus optional teams map into the
// rich shape. Promoted fields are populated only when raw_request_data is
// present (i.e., the get endpoint, not the list endpoint).
func buildAlertGroupRich(api *alertGroupAPI, teams map[string]string) *oncalltypes.AlertGroupRich {
	rich := &oncalltypes.AlertGroupRich{}

	rich.Spec.Integration = extractIntegrationRef(api.AlertReceiveChannel)
	teamID := extractTeamID(api.Team)
	if teamName := teams[teamID]; teamID != "" || teamName != "" {
		rich.Spec.Team = &oncalltypes.TeamRef{
			ID:   teamID,
			Name: teamName,
		}
	}
	rich.Spec.Permalinks = extractAlertGroupLinks(api.Permalinks)

	rich.Status.AlertsCount = api.AlertsCount
	rich.Status.State = decodeAlertGroupState(api.Status)
	rich.Status.Title = extractTitleFromRenderForWeb(api.RenderForWeb)
	if api.StartedAt != "" || api.AcknowledgedAt != "" || api.ResolvedAt != "" || api.SilencedAt != "" {
		rich.Status.Timestamps = &oncalltypes.AlertTimestamps{
			Started:      api.StartedAt,
			Acknowledged: api.AcknowledgedAt,
			Resolved:     api.ResolvedAt,
			Silenced:     api.SilencedAt,
		}
	}

	// Promoted fields require the Alertmanager-shape payload, which only the
	// alertgroups/<id>/ retrieve endpoint includes.
	raw, ok := extractRawRequestDataFromLastAlert(api.LastAlert)
	if !ok {
		return rich
	}

	first := firstAlertOrNil(raw.Alerts)
	commonLabels := parseStringMap(raw.CommonLabels)
	commonAnnotations := parseStringMap(raw.CommonAnnotations)
	groupLabels := parseStringMap(raw.GroupLabels)

	var alertLabels, alertAnnotations map[string]string
	if first != nil {
		alertLabels = parseStringMap(first.Labels)
		alertAnnotations = parseStringMap(first.Annotations)
	}

	rich.Status.Severity = firstNonEmpty(commonLabels["severity"], groupLabels["severity"])
	rich.Status.Summary = firstNonEmpty(commonAnnotations["summary"], commonAnnotations["description"])
	rich.Status.RunbookURL = firstNonEmpty(commonAnnotations["runbook_url"], commonAnnotations["runbookURL"])
	rich.Status.Target = extractTarget(commonLabels)

	rule := extractRule(first, alertLabels, commonLabels)
	var instance *oncalltypes.AlertInstance
	if first != nil && (first.Fingerprint != "" || first.SilenceURL != "") {
		instance = &oncalltypes.AlertInstance{
			ID:         first.Fingerprint,
			SilenceURL: first.SilenceURL,
		}
	}
	dashboard := extractDashboard(first, alertAnnotations, commonAnnotations)
	slo := extractSLO(commonLabels, commonAnnotations)
	rich.Status.Links = buildAlertLinks(rule, instance, dashboard, slo)

	if len(commonLabels) > 0 || len(commonAnnotations) > 0 || len(groupLabels) > 0 {
		rich.Status.Raw = &oncalltypes.AlertGroupRaw{
			CommonLabels:      commonLabels,
			CommonAnnotations: commonAnnotations,
			GroupLabels:       groupLabels,
		}
	}
	return rich
}

// GetAlertRich fetches a single alert via the AlertRawSerializer endpoint and
// returns the rich shape plus the meta fields (id, created_at, alert_group_id).
func (c *OnCallClient) GetAlertRich(ctx context.Context, id string) (*alertAPI, *oncalltypes.AlertRich, error) {
	resp, err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s%s/", alertsPath, url.PathEscape(id)), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("irm: get alert: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, fmt.Errorf("irm: alert %q not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, handleErrorResponse(resp)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("irm: read alert: %w", err)
	}

	var api alertAPI
	if err := json.Unmarshal(body, &api); err != nil {
		return nil, nil, fmt.Errorf("irm: decode alert: %w", err)
	}

	rich := buildAlertRich(&api)
	return &api, rich, nil
}

// buildAlertRich folds an alertAPI into the rich Alert shape, including the
// full Alertmanager-shape payload under status.raw. Callers strip status.raw
// post-build when --include-raw is not set.
func buildAlertRich(api *alertAPI) *oncalltypes.AlertRich {
	rich := &oncalltypes.AlertRich{}
	rich.Spec.AlertGroupID = firstNonEmpty(api.AlertGroupPK, api.AlertGroupID)

	if len(api.RawRequestData) == 0 || string(api.RawRequestData) == "null" {
		return rich
	}

	var raw rawRequestData
	if err := json.Unmarshal(api.RawRequestData, &raw); err != nil {
		return rich
	}

	first := firstAlertOrNil(raw.Alerts)
	commonLabels := parseStringMap(raw.CommonLabels)
	commonAnnotations := parseStringMap(raw.CommonAnnotations)
	groupLabels := parseStringMap(raw.GroupLabels)

	var alertLabels, alertAnnotations map[string]string
	if first != nil {
		alertLabels = parseStringMap(first.Labels)
		alertAnnotations = parseStringMap(first.Annotations)
		rich.Status.State = first.Status
	}

	// For per-alert target/severity, prefer the alert's own labels (the per-alert
	// dimensionality is what callers want when iterating alerts), falling back
	// to commonLabels.
	rich.Status.Severity = firstNonEmpty(alertLabels["severity"], commonLabels["severity"])
	rich.Status.Target = extractTarget(mergeLabelMaps(commonLabels, alertLabels))

	rule := extractRule(first, alertLabels, commonLabels)
	var instance *oncalltypes.AlertInstance
	if first != nil && (first.Fingerprint != "" || first.SilenceURL != "") {
		instance = &oncalltypes.AlertInstance{
			ID:         first.Fingerprint,
			SilenceURL: first.SilenceURL,
		}
	}
	dashboard := extractDashboard(first, alertAnnotations, commonAnnotations)
	slo := extractSLO(mergeLabelMaps(commonLabels, alertLabels), mergeLabelMaps(commonAnnotations, alertAnnotations))
	rich.Status.Links = buildAlertLinks(rule, instance, dashboard, slo)

	rich.Status.Raw = &oncalltypes.AlertPayload{
		Status:            raw.Status,
		GroupKey:          raw.GroupKey,
		ExternalURL:       raw.ExternalURL,
		Receiver:          raw.Receiver,
		NumFiring:         raw.NumFiring,
		NumResolved:       raw.NumResolved,
		TruncatedAlerts:   raw.TruncatedAlerts,
		GroupLabels:       groupLabels,
		CommonLabels:      commonLabels,
		CommonAnnotations: commonAnnotations,
		Alerts:            toAlertmanagerAlerts(raw.Alerts),
	}
	return rich
}

// mergeLabelMaps returns a shallow merge with later maps overriding earlier ones.
func mergeLabelMaps(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// listAlertIDs lists alert IDs (with cap) for an alert group via the slim list endpoint.
// Returns the IDs in API order, the total count from the response, and any error.
func (c *OnCallClient) listAlertIDs(ctx context.Context, alertGroupID string, limit int) ([]string, int, error) {
	params := url.Values{}
	params.Set("alert_group_id", alertGroupID)
	if limit > 0 {
		params.Set("perpage", fmt.Sprintf("%d", limit))
	}
	resp, err := c.DoRequest(ctx, http.MethodGet, alertsPath+"?"+params.Encode(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("irm: list alerts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, handleErrorResponse(resp)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("irm: read alerts list: %w", err)
	}

	var page struct {
		Count   int `json:"count"`
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		// Non-paginated raw array fallback.
		var arr []struct {
			ID string `json:"id"`
		}
		if errArr := json.Unmarshal(body, &arr); errArr != nil {
			return nil, 0, fmt.Errorf("irm: decode alerts list: %w", err)
		}
		ids := make([]string, len(arr))
		for i, a := range arr {
			ids[i] = a.ID
		}
		return ids, len(arr), nil
	}
	ids := make([]string, 0, len(page.Results))
	for _, r := range page.Results {
		if r.ID != "" {
			ids = append(ids, r.ID)
		}
	}
	if page.Count == 0 {
		page.Count = len(ids)
	}
	return ids, page.Count, nil
}
