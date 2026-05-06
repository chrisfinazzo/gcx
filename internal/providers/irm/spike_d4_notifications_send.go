// spike_d4_notifications_send.go — POC for ADR 001 Decision 4.
// THIS IS THROWAWAY CODE. No tests, no error wrapping, panic/CheckErr everywhere.
// Validates: unified `notifications send` verb driving /direct_paging/ (dry-run)
// and /users/{id}/{make_test_call|send_test_sms|send_test_push}/ (live test).
package irm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	registerSpikeBuilder(buildSpikeD4NotificationsSendCmd)
}

func buildSpikeD4NotificationsSendCmd(loader OnCallConfigLoader) *cobra.Command {
	var (
		userIDs   string
		team      string
		important bool
		test      bool
		via       string
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:   "d4 [message]",
		Short: "[POC] Validate ADR 001 D4: notifications send (direct_paging + test endpoints)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			message := "spike test"
			if len(args) > 0 {
				message = args[0]
			}

			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			errw := cmd.ErrOrStderr()

			apiClient, _, err := loader.LoadOnCallClient(ctx)
			cobra.CheckErr(err)

			// --- Resolve current user via GET user/ (singular) ---
			// Use the typed method but also dump raw to see full shape.
			// We do a raw DoRequest so we can print the raw JSON body.
			rawClient, ok := apiClient.(*OnCallClient)
			if !ok {
				cobra.CheckErr(fmt.Errorf("spike d4: expected *OnCallClient (OAuth mode), got %T — SA token mode not supported for this spike", apiClient))
			}

			fmt.Fprintln(errw, "> GET user/")
			userResp, err := rawClient.DoRequest(ctx, http.MethodGet, currentUserPath, nil)
			cobra.CheckErr(err)
			defer userResp.Body.Close()

			userBody, err := io.ReadAll(userResp.Body)
			cobra.CheckErr(err)

			fmt.Fprintf(errw, "  status: %d\n", userResp.StatusCode)

			var meRaw map[string]any
			cobra.CheckErr(json.Unmarshal(userBody, &meRaw))

			mePK, _ := meRaw["pk"].(string)
			if mePK == "" {
				// fallback: some shapes use "id"
				mePK, _ = meRaw["id"].(string)
			}

			fmt.Fprintln(out, "=== /user/ response ===")
			prettyPrint(out, meRaw)

			if mePK == "" {
				cobra.CheckErr(fmt.Errorf("spike d4: could not extract pk/id from /user/ response"))
			}
			fmt.Fprintf(errw, "  resolved me → pk=%s\n", mePK)

			// --- Resolve user IDs: replace "me" with mePK ---
			var resolvedIDs []string
			if userIDs != "" {
				for _, uid := range strings.Split(userIDs, ",") {
					uid = strings.TrimSpace(uid)
					if uid == "me" {
						resolvedIDs = append(resolvedIDs, mePK)
					} else {
						resolvedIDs = append(resolvedIDs, uid)
					}
				}
			}

			// --- Test mode: POST to users/<pk>/<endpoint>/ ---
			if test {
				targetPK := mePK
				if len(resolvedIDs) > 0 {
					targetPK = resolvedIDs[0]
				}

				endpoint := viaToEndpoint(via)
				path := fmt.Sprintf("%s%s/%s/", usersPath, url.PathEscape(targetPK), endpoint)

				fmt.Fprintf(errw, "> POST %s\n", path)
				fmt.Fprintln(errw, "  body: (empty)")

				resp, err := rawClient.DoRequest(ctx, http.MethodPost, path, nil)
				cobra.CheckErr(err)
				defer resp.Body.Close()

				body, err := io.ReadAll(resp.Body)
				cobra.CheckErr(err)

				fmt.Fprintf(errw, "  status: %d\n", resp.StatusCode)
				fmt.Fprintf(out, "\n=== POST %s ===\n", path)
				fmt.Fprintf(out, "status: %d\n", resp.StatusCode)
				if len(body) > 0 {
					fmt.Fprintf(out, "body: %s\n", string(body))
				} else {
					fmt.Fprintln(out, "body: (empty)")
				}
				return nil
			}

			// --- Non-test mode: build DirectPagingInput and POST or dry-run ---
			input := DirectPagingInput{
				Title:   message,
				Message: message,
			}

			if team != "" {
				// The backend has two fields: "team" (PK) and "team_name" (name/slug).
				// DirectPagingInput only models "team". We send the value as-is and
				// report whether it was accepted. To also test team_name, we build
				// a raw map so both paths are visible.
				input.Team = team
				if important {
					input.ImportantTeamEscalation = true
				}
			} else if len(resolvedIDs) > 0 {
				// users and team are mutually exclusive per backend validation.
				for _, pk := range resolvedIDs {
					input.Users = append(input.Users, UserReference{
						ID:        pk, // send as PK; backend also accepts 'username' field
						Important: important,
					})
				}
			}

			bodyBytes, err := json.MarshalIndent(input, "", "  ")
			cobra.CheckErr(err)

			targetURL := rawClient.Host + BasePath + "/" + directPagingPath
			fmt.Fprintf(errw, "> POST %s\n", targetURL)
			fmt.Fprintf(errw, "  body:\n%s\n", string(bodyBytes))

			if dryRun {
				fmt.Fprintln(out, "\n=== DRY-RUN: would POST to direct_paging ===")
				fmt.Fprintf(out, "URL: %s\n", targetURL)
				fmt.Fprintln(out, "Body:")
				fmt.Fprintln(out, string(bodyBytes))

				// Also show the team_name variant so the spec writer sees both options.
				if team != "" {
					teamNameVariant := map[string]any{
						"title":                     message,
						"message":                   message,
						"team_name":                 team,
						"important_team_escalation": important,
					}
					fmt.Fprintln(out, "\nAlternative body using team_name field (slug lookup):")
					prettyPrint(out, teamNameVariant)
				}
				return nil
			}

			// Actually POST (only reached when --dry-run=false, which the smoke tests never do for direct_paging).
			resp, err := rawClient.DoRequest(ctx, http.MethodPost, directPagingPath, bytes.NewReader(bodyBytes))
			cobra.CheckErr(err)
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			cobra.CheckErr(err)

			fmt.Fprintf(errw, "  status: %d\n", resp.StatusCode)
			fmt.Fprintf(out, "\n=== POST direct_paging response ===\n")
			fmt.Fprintf(out, "status: %d\n", resp.StatusCode)
			if len(body) > 0 {
				var result map[string]any
				if jsonErr := json.Unmarshal(body, &result); jsonErr == nil {
					prettyPrint(out, result)
				} else {
					fmt.Fprintf(out, "body: %s\n", string(body))
				}
			} else {
				fmt.Fprintln(out, "body: (empty)")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&userIDs, "user-ids", "", "Comma-separated user IDs or PKs; 'me' resolves to the authenticated user")
	cmd.Flags().StringVar(&team, "team", "", "Team PK or slug (try both; spike reports which the backend accepts)")
	cmd.Flags().BoolVar(&important, "important", false, "Per-user important=true (user mode) or important_team_escalation=true (team mode)")
	cmd.Flags().BoolVar(&test, "test", false, "Use test notification endpoints instead of direct_paging")
	cmd.Flags().StringVar(&via, "via", "push", "Test channel: push|sms|call (only used with --test)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "Print request body and exit without POSTing to direct_paging")

	return cmd
}

// viaToEndpoint maps the --via flag value to the OnCall API endpoint name.
func viaToEndpoint(via string) string {
	switch via {
	case "sms":
		return "send_test_sms"
	case "call":
		return "make_test_call"
	default: // "push" and anything else
		return "send_test_push"
	}
}

// prettyPrint writes v as indented JSON to w.
func prettyPrint(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
