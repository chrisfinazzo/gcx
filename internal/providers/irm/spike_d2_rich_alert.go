// spike_d2_rich_alert.go — ADR 001 Decision 2 spike: prove/disprove the rich Alert payload claim.
// THIS IS THROWAWAY POC. No tests, no error wrapping. Panic is intentional.
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	registerSpikeBuilder(buildSpikeD2RichAlertCmd)
}

func buildSpikeD2RichAlertCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "d2 <alert-group-id>",
		Short: "[POC] ADR 001 D2: probe rich Alert payload via list vs retrieve endpoints",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alertGroupID := args[0]
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			api, _, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return fmt.Errorf("load oncall client: %w", err)
			}

			// We need raw HTTP — type-assert to *OnCallClient (OAuth proxy path).
			// SA-token mode returns *oncallpublic.Client which lacks DoRequest.
			c, ok := api.(*OnCallClient)
			if !ok {
				return fmt.Errorf("spike requires OAuth context (plugin proxy). got type %T — re-run with an OAuth-authenticated context", api)
			}

			// ----------------------------------------------------------------
			// 1. LIST endpoint: alerts/?alert_group_id=<id>
			// ----------------------------------------------------------------
			fmt.Fprintf(out, "\n=== LIST endpoint payload (current AlertSerializer) ===\n")
			fmt.Fprintf(out, "GET alerts/?alert_group_id=%s\n\n", alertGroupID)

			listStart := time.Now()
			listResp, err := doRaw(ctx, c, http.MethodGet, fmt.Sprintf("alerts/?alert_group_id=%s", alertGroupID))
			listElapsed := time.Since(listStart)
			if err != nil {
				panic(err)
			}

			listBody, err := io.ReadAll(listResp.Body)
			listResp.Body.Close()
			if err != nil {
				panic(err)
			}
			if listResp.StatusCode != http.StatusOK {
				return fmt.Errorf("list alerts: HTTP %d: %s", listResp.StatusCode, string(listBody))
			}

			// Parse the paginated result to get results array.
			var listParsed struct {
				Results []json.RawMessage `json:"results"`
				Count   int               `json:"count"`
			}
			if err := json.Unmarshal(listBody, &listParsed); err != nil {
				panic(err)
			}

			if len(listParsed.Results) == 0 {
				fmt.Fprintf(out, "BLOCKED: alert group %s has 0 alerts — cannot continue spike.\n", alertGroupID)
				return nil
			}

			// Print first item from list.
			printPretty(out, listParsed.Results[0])
			fmt.Fprintf(out, "\nList call: %dms  (total alerts in group: %d)\n", listElapsed.Milliseconds(), listParsed.Count)

			// Extract ID of first alert to use for retrieve.
			var firstAlert struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(listParsed.Results[0], &firstAlert); err != nil {
				panic(err)
			}
			alertID := firstAlert.ID
			fmt.Fprintf(out, "First alert ID: %s\n", alertID)

			// ----------------------------------------------------------------
			// 2. RETRIEVE endpoint: alerts/<id>/
			// ----------------------------------------------------------------
			fmt.Fprintf(out, "\n=== RETRIEVE endpoint payload (AlertRawSerializer) ===\n")
			fmt.Fprintf(out, "GET alerts/%s/\n\n", alertID)

			// Run 3 iterations to get a stable timing sample.
			var retrieveBody []byte
			var retrieveTotal time.Duration
			const iters = 3
			for i := 0; i < iters; i++ {
				t0 := time.Now()
				retrieveResp, err := doRaw(ctx, c, http.MethodGet, fmt.Sprintf("alerts/%s/", alertID))
				retrieveTotal += time.Since(t0)
				if err != nil {
					panic(err)
				}
				b, err := io.ReadAll(retrieveResp.Body)
				retrieveResp.Body.Close()
				if err != nil {
					panic(err)
				}
				if retrieveResp.StatusCode != http.StatusOK {
					return fmt.Errorf("retrieve alert: HTTP %d: %s", retrieveResp.StatusCode, string(b))
				}
				if i == 0 {
					retrieveBody = b
				}
			}
			retrieveMean := retrieveTotal / iters

			var retrieveParsed json.RawMessage
			if err := json.Unmarshal(retrieveBody, &retrieveParsed); err != nil {
				panic(err)
			}
			printPretty(out, retrieveParsed)
			fmt.Fprintf(out, "\nRetrieve call (mean of %d): %dms\n", iters, retrieveMean.Milliseconds())

			// ----------------------------------------------------------------
			// 3. Promoted-field extraction from raw_request_data
			// ----------------------------------------------------------------
			fmt.Fprintf(out, "\n=== Promoted-field extraction ===\n")

			var rawDoc map[string]json.RawMessage
			if err := json.Unmarshal(retrieveBody, &rawDoc); err != nil {
				panic(err)
			}

			// raw_request_data is the Alertmanager webhook payload.
			var rawReqData map[string]json.RawMessage
			if rawBytes, ok := rawDoc["raw_request_data"]; ok {
				if err := json.Unmarshal(rawBytes, &rawReqData); err != nil {
					fmt.Fprintf(out, "raw_request_data: could not parse as object: %v\n", err)
				}
			} else {
				fmt.Fprintf(out, "raw_request_data: key absent\n")
			}

			// alerts[0].labels from the Alertmanager payload.
			var amAlerts []map[string]json.RawMessage
			if alertsBytes, ok := rawReqData["alerts"]; ok {
				if err := json.Unmarshal(alertsBytes, &amAlerts); err != nil {
					fmt.Fprintf(out, "raw_request_data.alerts: could not parse: %v\n", err)
				}
			}

			var alertLabels map[string]string
			var alertAnnotations map[string]string
			var alertFingerprint string
			var alertGeneratorURL string

			if len(amAlerts) > 0 {
				a0 := amAlerts[0]
				if labBytes, ok := a0["labels"]; ok {
					json.Unmarshal(labBytes, &alertLabels) //nolint:errcheck
				}
				if annBytes, ok := a0["annotations"]; ok {
					json.Unmarshal(annBytes, &alertAnnotations) //nolint:errcheck
				}
				if fpBytes, ok := a0["fingerprint"]; ok {
					json.Unmarshal(fpBytes, &alertFingerprint) //nolint:errcheck
				}
				if guBytes, ok := a0["generatorURL"]; ok {
					json.Unmarshal(guBytes, &alertGeneratorURL) //nolint:errcheck
				}
			}

			var commonLabels map[string]string
			if clBytes, ok := rawReqData["commonLabels"]; ok {
				json.Unmarshal(clBytes, &commonLabels) //nolint:errcheck
			}

			type extractResult struct {
				field  string
				found  bool
				source string
				value  string
			}

			results := []extractResult{
				extractField("alertRuleUID",
					alertLabels["__alert_rule_uid__"],
					"raw_request_data.alerts[0].labels.__alert_rule_uid__",
					commonLabels["__alert_rule_uid__"],
					"raw_request_data.commonLabels.__alert_rule_uid__"),
				extractField("dashboardUID",
					alertLabels["dashboard_uid"],
					"raw_request_data.alerts[0].labels.dashboard_uid",
					alertAnnotations["dashboardUID"],
					"raw_request_data.alerts[0].annotations.dashboardUID"),
				extractField("panelID",
					alertLabels["panel_id"],
					"raw_request_data.alerts[0].labels.panel_id",
					alertAnnotations["panelID"],
					"raw_request_data.alerts[0].annotations.panelID"),
				extractField("alertGroupUID",
					alertLabels["grafana_folder_uid"],
					"raw_request_data.alerts[0].labels.grafana_folder_uid",
					"", ""),
				{"alertInstanceID (fingerprint)", alertFingerprint != "", "raw_request_data.alerts[0].fingerprint", alertFingerprint},
			}

			fmt.Fprintf(out, "%-20s %-6s %-55s %s\n", "Promoted field", "Found?", "Source path", "Value (truncated)")
			fmt.Fprintf(out, "%-20s %-6s %-55s %s\n", "---", "---", "---", "---")
			for _, r := range results {
				foundStr := "no"
				if r.found {
					foundStr = "yes"
				}
				val := r.value
				if len(val) > 40 {
					val = val[:40] + "..."
				}
				fmt.Fprintf(out, "%-20s %-6s %-55s %s\n", r.field, foundStr, r.source, val)
			}

			// ----------------------------------------------------------------
			// 4. Alertgroup bonus check: does /alertgroups/<id>/ carry rich data?
			// ----------------------------------------------------------------
			fmt.Fprintf(out, "\n=== ALERTGROUP bonus check ===\n")
			fmt.Fprintf(out, "GET alertgroups/%s/\n\n", alertGroupID)

			agStart := time.Now()
			agResp, err := doRaw(ctx, c, http.MethodGet, fmt.Sprintf("alertgroups/%s/", alertGroupID))
			agElapsed := time.Since(agStart)
			if err != nil {
				panic(err)
			}
			agBody, err := io.ReadAll(agResp.Body)
			agResp.Body.Close()
			if err != nil {
				panic(err)
			}
			if agResp.StatusCode != http.StatusOK {
				fmt.Fprintf(out, "alertgroup retrieve: HTTP %d: %s\n", agResp.StatusCode, string(agBody))
			} else {
				var agParsed json.RawMessage
				if err := json.Unmarshal(agBody, &agParsed); err != nil {
					panic(err)
				}
				printPretty(out, agParsed)
				fmt.Fprintf(out, "\nAlertgroup call: %dms\n", agElapsed.Milliseconds())
			}

			// ----------------------------------------------------------------
			// 5. N+1 cost estimate
			// ----------------------------------------------------------------
			fmt.Fprintf(out, "\n=== N+1 cost estimate ===\n")
			n := listParsed.Count
			estimated := listElapsed + time.Duration(n)*retrieveMean
			fmt.Fprintf(out, "List: %dms\n", listElapsed.Milliseconds())
			fmt.Fprintf(out, "Per-retrieve (mean %d iters): %dms\n", iters, retrieveMean.Milliseconds())
			fmt.Fprintf(out, "N+1 total for N=%d alerts: ~%dms\n", n, estimated.Milliseconds())
			fmt.Fprintf(out, "Alertgroup single call: %dms\n", agElapsed.Milliseconds())

			return nil
		},
	}
	return cmd
}

// doRaw issues a raw GET/etc. without body parsing.
func doRaw(ctx context.Context, c *OnCallClient, method, path string) (*http.Response, error) {
	return c.DoRequest(ctx, method, path, nil)
}

// printPretty pretty-prints raw JSON to w.
func printPretty(w io.Writer, raw json.RawMessage) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		fmt.Fprintf(w, "(could not indent JSON: %v)\n%s\n", err, string(raw))
		return
	}
	fmt.Fprintln(w, buf.String())
}

// extractField picks the first non-empty value from two candidate (value, path) pairs.
func extractField(field, primary, primaryPath, secondary, secondaryPath string) struct {
	field  string
	found  bool
	source string
	value  string
} {
	if primary != "" {
		return struct {
			field  string
			found  bool
			source string
			value  string
		}{field, true, primaryPath, primary}
	}
	if secondary != "" {
		return struct {
			field  string
			found  bool
			source string
			value  string
		}{field, true, secondaryPath, secondary}
	}
	return struct {
		field  string
		found  bool
		source string
		value  string
	}{field, false, primaryPath, ""}
}
