package oncalltypes

// Rich types render the AlertGroup/Alert payloads in the spec/status shape that
// gcx exposes via `irm oncall alert-groups get|list|list-alerts` and
// `irm oncall alerts get`. They differ from the API-shaped AlertGroup/Alert
// types in this package (which mirror the OnCall internal API verbatim).
//
// Empty fields are routinely expected because:
//  - the AlertGroups list endpoint does not return last_alert.raw_request_data,
//    so all fields extracted from the Alertmanager-shaped payload stay empty,
//  - integrations like formatted_webhook/generic webhook do not provide labels
//    or annotations, so the promoted rule/dashboard/instance fields stay empty.
// `omitempty` keeps the YAML tidy in those cases.

// AlertGroupRich is the K8s-envelope spec/status payload for an AlertGroup.
// It is marshalled to a top-level {"spec": ..., "status": ...} object so the
// command layer can lift those keys directly into the K8s envelope.
type AlertGroupRich struct {
	Spec   AlertGroupSpec   `json:"spec"`
	Status AlertGroupStatus `json:"status"`
}

// AlertGroupSpec captures stable identity-ish metadata about the alert group.
type AlertGroupSpec struct {
	Integration IntegrationRef  `json:"integration"`
	Team        *TeamRef        `json:"team,omitempty"`
	Permalinks  AlertGroupLinks `json:"permalinks"`
}

// AlertGroupStatus captures the live state of the alert group, including
// fields promoted out of the Alertmanager-shaped payload.
type AlertGroupStatus struct {
	Title       string           `json:"title,omitempty"`
	Summary     string           `json:"summary,omitempty"`
	Severity    string           `json:"severity,omitempty"`
	State       string           `json:"state,omitempty"`
	RunbookURL  string           `json:"runbookURL,omitempty"`
	Target      *AlertTarget     `json:"target,omitempty"`
	Timestamps  *AlertTimestamps `json:"timestamps,omitempty"`
	Links       *AlertLinks      `json:"links,omitempty"`
	AlertsCount int              `json:"alertsCount,omitempty"`
	Raw         *AlertGroupRaw   `json:"raw,omitempty"`
}

// AlertLinks groups the cross-provider pivot identifiers and URLs reachable
// from this alert: the rule that fired, this firing instance, the linked
// dashboard, and (when applicable) the backing SLO.
type AlertLinks struct {
	Alert     *AlertLinkAlert `json:"alert,omitempty"`
	Dashboard *AlertDashboard `json:"dashboard,omitempty"`
	SLO       *AlertLinkSLO   `json:"slo,omitempty"`
}

// AlertLinkAlert pairs the alert rule and its specific firing instance.
type AlertLinkAlert struct {
	Rule     *AlertRule     `json:"rule,omitempty"`
	Instance *AlertInstance `json:"instance,omitempty"`
}

// AlertLinkSLO identifies the Grafana SLO this alert measures, when present.
// Pivot via `gcx slo definitions get <uid>`.
type AlertLinkSLO struct {
	UID  string `json:"uid,omitempty"`
	Name string `json:"name,omitempty"`
}

// IntegrationRef identifies the OnCall integration that produced the group.
type IntegrationRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// TeamRef identifies the OnCall team owning the alert group.
type TeamRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// AlertGroupLinks holds the OnCall-rendered permalinks.
type AlertGroupLinks struct {
	Web      string `json:"web,omitempty"`
	Slack    string `json:"slack,omitempty"`
	SlackApp string `json:"slack_app,omitempty"`
	Telegram string `json:"telegram,omitempty"`
}

// AlertTarget is the standardized target descriptor extracted from common labels.
type AlertTarget struct {
	Cluster   string `json:"cluster,omitempty"`
	Service   string `json:"service,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// AlertTimestamps groups the lifecycle timestamps of the AlertGroup.
type AlertTimestamps struct {
	Started      string `json:"started,omitempty"`
	Acknowledged string `json:"acknowledged,omitempty"`
	Resolved     string `json:"resolved,omitempty"`
	Silenced     string `json:"silenced,omitempty"`
}

// AlertRule identifies the upstream Grafana alert rule.
type AlertRule struct {
	UID string `json:"uid,omitempty"`
	URL string `json:"url,omitempty"`
}

// AlertInstance identifies a single alert instance within a group.
type AlertInstance struct {
	ID         string `json:"id,omitempty"`
	SilenceURL string `json:"silenceURL,omitempty"`
}

// AlertDashboard describes the Grafana dashboard / panel context for the alert.
type AlertDashboard struct {
	UID   string      `json:"uid,omitempty"`
	URL   string      `json:"url,omitempty"`
	Panel *AlertPanel `json:"panel,omitempty"`
}

// AlertPanel identifies a panel on the linked dashboard.
type AlertPanel struct {
	ID  int    `json:"id,omitempty"`
	URL string `json:"url,omitempty"`
}

// AlertGroupRaw is the subset of raw_request_data preserved on the AlertGroup
// for diagnostic / power-user use.
type AlertGroupRaw struct {
	CommonLabels      map[string]string `json:"commonLabels,omitempty"`
	CommonAnnotations map[string]string `json:"commonAnnotations,omitempty"`
	GroupLabels       map[string]string `json:"groupLabels,omitempty"`
}

// AlertRich is the K8s-envelope spec/status payload for a single Alert.
type AlertRich struct {
	Spec   AlertSpec   `json:"spec"`
	Status AlertStatus `json:"status"`
}

// AlertSpec captures back-pointer identity for the alert.
type AlertSpec struct {
	AlertGroupID string `json:"alertGroupID,omitempty"`
}

// AlertStatus captures the rendered state of an alert plus the full payload.
//
// Raw is the unprocessed Alertmanager-shape group webhook (= the API's
// raw_request_data). Hidden by default and gated behind `--include-raw` on
// the CLI; the extracted fields above (target/links/...) are the curated
// promotion of the same data.
type AlertStatus struct {
	State    string        `json:"state,omitempty"`
	Severity string        `json:"severity,omitempty"`
	Target   *AlertTarget  `json:"target,omitempty"`
	Links    *AlertLinks   `json:"links,omitempty"`
	Raw      *AlertPayload `json:"raw,omitempty"`
}

// AlertPayload is the Alertmanager-shape group webhook (raw_request_data).
type AlertPayload struct {
	Status            string              `json:"status,omitempty"`
	GroupKey          string              `json:"groupKey,omitempty"`
	ExternalURL       string              `json:"externalURL,omitempty"`
	Receiver          string              `json:"receiver,omitempty"`
	NumFiring         int                 `json:"numFiring,omitempty"`
	NumResolved       int                 `json:"numResolved,omitempty"`
	TruncatedAlerts   int                 `json:"truncatedAlerts,omitempty"`
	GroupLabels       map[string]string   `json:"groupLabels,omitempty"`
	CommonLabels      map[string]string   `json:"commonLabels,omitempty"`
	CommonAnnotations map[string]string   `json:"commonAnnotations,omitempty"`
	Alerts            []AlertmanagerAlert `json:"alerts,omitempty"`
}

// AlertmanagerAlert mirrors the alerts[] entry of the Alertmanager webhook.
// We keep the optional first-class fields the OnCall backend adds for
// grafana_alerting integrations (ruleUID, dashboardURL, panelURL, silenceURL).
type AlertmanagerAlert struct {
	Status       string            `json:"status,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	Fingerprint  string            `json:"fingerprint,omitempty"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
	StartsAt     string            `json:"startsAt,omitempty"`
	EndsAt       string            `json:"endsAt,omitempty"`

	// grafana_alerting first-class fields (absent on alertmanager / webhook integrations).
	RuleUID      string `json:"ruleUID,omitempty"`
	DashboardURL string `json:"dashboardURL,omitempty"`
	PanelURL     string `json:"panelURL,omitempty"`
	SilenceURL   string `json:"silenceURL,omitempty"`
}
