// spike_d1d3d6d7d8_demo.go — ADR 001 Decisions 1,3,6,7,8 combined client-side demo spike.
// THIS IS THROWAWAY POC. No tests, no structured errors. panic(err) intentional.
package irm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() {
	registerSpikeBuilder(buildSpikeClientDemoCmd)
}

// mutationResult is the D7 MutationResult envelope emitted on stdout.
type mutationResult struct {
	Action  string          `json:"action"`
	Target  mutationTarget  `json:"target"`
	Changed bool            `json:"changed"`
	Summary mutationSummary `json:"summary"`
}

type mutationTarget struct {
	AlertGroupIDs []string `json:"alertGroupIds"`
}

type mutationSummary struct {
	Matched   int  `json:"matched"`
	Succeeded int  `json:"succeeded"`
	Failed    int  `json:"failed"`
	DryRun    bool `json:"dryRun"`
}

// progressEvent is a D7 JSONL record emitted on stderr.
type progressEvent struct {
	Event        string `json:"event"`
	Class        string `json:"class"`
	AlertGroupID string `json:"alertGroupId,omitempty"`
	Status       any    `json:"status,omitempty"`
}

// diagnosticEvent is a D8 JSONL record emitted on stderr in agent mode.
type diagnosticEvent struct {
	Class   string `json:"class"`
	Summary string `json:"summary"`
	Command string `json:"command,omitempty"`
}

// emitStderr writes a single JSONL record or a plain prefixed line to stderr.
// In agent mode the record is marshalled as JSON; in TTY mode it's the plain prefix form.
func emitStderr(prefix, plain string, rec any) {
	if agent.IsAgentMode() {
		b, err := json.Marshal(rec)
		if err != nil {
			panic(err)
		}
		fmt.Fprintln(os.Stderr, string(b))
	} else {
		fmt.Fprintf(os.Stderr, "%s: %s\n", prefix, plain)
	}
}

// emitProgress writes a typed progress event to stderr (always JSONL regardless of mode,
// because progress events are machine-readable in both modes per the ADR §7.1 contract).
func emitProgress(ev progressEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, string(b))
}

// oncallGroup keeps the API group and version for deeplink resolution.
const (
	oncallAPIGroup = "oncall.ext.grafana.app"
	oncallVersion  = "v1alpha1"
)

// allOnCallKinds is the full inventory of OnCall resource kinds from the ADR backfill list
// plus the ones already known to be in the adapter.
var allOnCallKinds = []string{
	"AlertGroup",
	"Integration",
	"EscalationChain",
	"EscalationPolicy",
	"Schedule",
	"Shift",
	"Route",
	"Webhook",
	"User",
	"Team",
	"ResolutionNote",
	"ShiftSwap",
}

func buildSpikeClientDemoCmd(loader OnCallConfigLoader) *cobra.Command {
	var (
		team               string
		integrations       []string
		maxAge             time.Duration
		mine               bool
		includeResolved    bool
		includeChildGroups bool
		all                bool
		open               bool
		simulateBulkAck    bool
	)

	cmd := &cobra.Command{
		Use:   "client-demo",
		Short: "[POC] ADR 001 D1/D3/D6/D7/D8: list defaults, --open, bulk-ack simulation, MutationResult, hints",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			stderr := os.Stderr

			// ----------------------------------------------------------------
			// Load client — type-assert to *OnCallClient (OAuth proxy required).
			// ----------------------------------------------------------------
			api, _, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				panic(err)
			}
			c, ok := api.(*OnCallClient)
			if !ok {
				return fmt.Errorf("spike requires OAuth context (plugin proxy). got type %T — re-run with an OAuth-authenticated context", api)
			}
			// c.Host is the full Grafana server URL (e.g. https://ops.grafana-ops.net).
			// LoadOnCallClient's second return is the namespace — use c.Host for deeplink resolution.
			grafanaHost := c.Host

			// ----------------------------------------------------------------
			// Phase 1 — D1: build query params with new defaults.
			// ----------------------------------------------------------------
			params := url.Values{}

			if !all {
				// D1 defaults: status = 0(new/firing), 1(acknowledged), 3(silenced) and is_root=true.
				// The internal API uses integer status codes from AlertGroup.STATUS_CHOICES:
				//   0=New(firing), 1=Acknowledged, 2=Resolved, 3=Silenced
				params.Add("status", "0") // firing / new
				params.Add("status", "1") // acknowledged
				params.Add("status", "3") // silenced
				if includeResolved {
					params.Add("status", "2") // resolved
				}
				if !includeChildGroups {
					params.Set("is_root", "true")
				}
			}
			// --all: no status filter, no is_root — the escape hatch.

			if team != "" {
				params.Set("team", team)
			}
			for _, intg := range integrations {
				params.Add("integration", intg)
			}
			if maxAge > 0 {
				start := time.Now().Add(-maxAge).UTC().Format("2006-01-02T15:04:05")
				end := time.Now().UTC().Format("2006-01-02T15:04:05")
				params.Set("started_at", start+"_"+end)
			}

			queryPath := pathWithParams(alertGroupsPath, params)
			fmt.Fprintf(stderr, "# D1 query: alertgroups/?%s\n", params.Encode())

			if mine {
				fmt.Fprintf(stderr, "# --mine: would filter to current user PK (not implemented in spike)\n")
			}

			// GET first page only (D1 spike: just validate the params are sent and respected).
			resp, err := c.DoRequest(ctx, http.MethodGet, queryPath, nil)
			if err != nil {
				panic(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				bodyBytes, _ := io.ReadAll(resp.Body)
				panic(fmt.Errorf("list alert groups: HTTP %d: %s", resp.StatusCode, string(bodyBytes)))
			}

			var page paginatedResponse[AlertGroup]
			if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
				panic(err)
			}

			matched := page.Results

			// Emit D7 progress events for each matched group.
			for _, ag := range matched {
				emitProgress(progressEvent{
					Event:        "matched",
					Class:        "info",
					AlertGroupID: ag.PK,
					Status:       ag.Status,
				})
			}

			fmt.Fprintf(stderr, "# D1: matched %d groups on first page (total may be paginated)\n", len(matched))

			// ----------------------------------------------------------------
			// Phase 2 — D3: --open inventory.
			// ----------------------------------------------------------------
			if open {
				if len(matched) == 0 {
					emitStderr("warn", "no matched alert groups — cannot open", &diagnosticEvent{
						Class:   "warning",
						Summary: "no matched alert groups — cannot open",
					})
				} else {
					first := matched[0]
					gvk := schema.GroupVersionKind{
						Group:   oncallAPIGroup,
						Version: oncallVersion,
						Kind:    "AlertGroup",
					}
					resolvedURL := deeplink.Resolve(grafanaHost, gvk, first.PK)
					if resolvedURL != "" {
						fmt.Fprintf(stderr, "# --open URL: %s\n", resolvedURL)
					} else {
						// Fallback: hard-coded template (would use registry in production).
						fallbackURL := fmt.Sprintf("%s/a/grafana-irm-app/alert-groups/%s", grafanaHost, first.PK)
						fmt.Fprintf(stderr, "# --open fallback (no registry template): %s\n", fallbackURL)
						emitStderr("warn", "no deeplink template registered for AlertGroup — using hard-coded fallback", &diagnosticEvent{
							Class:   "warning",
							Summary: "no deeplink template registered for AlertGroup — using hard-coded fallback",
						})
					}
				}
			}

			// D3 inventory: probe each OnCall kind in the deeplink registry.
			fmt.Fprintf(stderr, "\n# D3 deeplink registry inventory (group=%s):\n", oncallAPIGroup)
			var missing []string
			for _, kind := range allOnCallKinds {
				gvk := schema.GroupVersionKind{
					Group:   oncallAPIGroup,
					Version: oncallVersion,
					Kind:    kind,
				}
				resolved := deeplink.Resolve(grafanaHost, gvk, "test-probe")
				if resolved != "" {
					fmt.Fprintf(stderr, "#   %-20s  REGISTERED  (sample: %s)\n", kind, resolved)
				} else {
					fmt.Fprintf(stderr, "#   %-20s  MISSING\n", kind)
					missing = append(missing, kind)
				}
			}
			fmt.Fprintf(stderr, "# D3 backfill needed: %v\n\n", missing)

			// ----------------------------------------------------------------
			// Phase 3 — D6: bulk-by-filter simulation (no actual POST).
			// ----------------------------------------------------------------
			var ackIDs []string
			if simulateBulkAck {
				for _, ag := range matched {
					ackIDs = append(ackIDs, ag.PK)
					// Emit what would be sent — no DoRequest call.
					fmt.Fprintf(stderr, "# D6 would POST: alertgroups/%s/acknowledge/ body={}\n", ag.PK)
					emitProgress(progressEvent{
						Event:        "would_acknowledge",
						Class:        "info",
						AlertGroupID: ag.PK,
					})
				}
			}

			// ----------------------------------------------------------------
			// Phase 4 — D7: emit ONE MutationResult envelope on stdout.
			// ----------------------------------------------------------------
			allIDs := make([]string, 0, len(matched))
			for _, ag := range matched {
				allIDs = append(allIDs, ag.PK)
			}

			result := mutationResult{
				Action:  "acknowledge",
				Target:  mutationTarget{AlertGroupIDs: allIDs},
				Changed: false, // dry-run, nothing mutated
				Summary: mutationSummary{
					Matched:   len(matched),
					Succeeded: 0,
					Failed:    0,
					DryRun:    true,
				},
			}
			resultJSON, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				panic(err)
			}
			fmt.Fprintln(os.Stdout, string(resultJSON))

			// ----------------------------------------------------------------
			// Phase 5 — D8: hint emission.
			// ----------------------------------------------------------------
			if len(matched) == 0 {
				emitStderr("note", "0 results — defaults exclude resolved+child groups; try --all", &diagnosticEvent{
					Class:   "note",
					Summary: "0 results — defaults exclude resolved+child groups; try --all",
				})
			}

			// Always emit the drill-in hint.
			hintCmd := "gcx irm oncall alert-groups list-alerts <group-id>"
			if len(matched) > 0 {
				hintCmd = fmt.Sprintf("gcx irm oncall alert-groups list-alerts %s", matched[0].PK)
			}
			emitStderr("hint", "Drill into alerts: "+hintCmd, &diagnosticEvent{
				Class:   "hint",
				Summary: "Drill into alerts",
				Command: hintCmd,
			})

			// --simulate-bulk-ack hint.
			if !simulateBulkAck && len(matched) > 0 {
				emitStderr("hint", "Simulate bulk-ack: rerun with --simulate-bulk-ack", &diagnosticEvent{
					Class:   "hint",
					Summary: "Simulate bulk-ack: rerun with --simulate-bulk-ack",
				})
			}

			_ = ctx // used above

			return nil
		},
	}

	cmd.Flags().StringVar(&team, "team", "", "Filter by team name or ID")
	cmd.Flags().StringSliceVar(&integrations, "integration", nil, "Filter by integration ID (repeatable)")
	cmd.Flags().DurationVar(&maxAge, "max-age", 0, "Only include groups started within this duration (e.g. 720h)")
	cmd.Flags().BoolVar(&mine, "mine", false, "Filter to current user's groups (spike: prints note only)")
	cmd.Flags().BoolVar(&includeResolved, "include-resolved", false, "Add status=resolved to the filter (opts into old behaviour)")
	cmd.Flags().BoolVar(&includeChildGroups, "include-child-groups", false, "Drop is_root filter — include child groups")
	cmd.Flags().BoolVar(&all, "all", false, "Bypass status+is_root filters entirely")
	cmd.Flags().BoolVar(&open, "open", false, "Resolve URL for first matched group and print it (no browser)")
	cmd.Flags().BoolVar(&simulateBulkAck, "simulate-bulk-ack", false, "For matched groups, emit the POST that WOULD acknowledge them (no mutation)")

	return cmd
}
