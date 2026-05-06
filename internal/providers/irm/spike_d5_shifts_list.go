package irm

// spike_d5_shifts_list.go — ADR 001 Decision 5 validation spike.
// Proves/disproves that `shifts list` filter composition can answer
// the four SRE coverage questions using the canonical Shift shape +
// filter_events + on_call_now. THROWAWAY — delete when ADR is shipped.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	registerSpikeBuilder(buildSpikeD5ShiftsListCmd)
}

// --- time helpers ---

var reDuration = regexp.MustCompile(`^\+(\d+)d$`)

func resolveTime(s string) (time.Time, error) {
	if s == "" || s == "now" {
		return time.Now().UTC(), nil
	}
	if m := reDuration.FindStringSubmatch(s); m != nil {
		days, _ := strconv.Atoi(m[1])
		return time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour), nil
	}
	return time.Parse(time.RFC3339, s)
}

// --- raw JSON helpers (spike-only) ---

func rawGET(ctx context.Context, c *OnCallClient, path string) ([]byte, error) {
	resp, err := c.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func printJSON(label string, v any) {
	fmt.Fprintf(os.Stderr, "\n# === %s ===\n", label)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
}

func printRawJSON(label string, raw []byte) {
	fmt.Fprintf(os.Stderr, "\n# === %s ===\n", label)
	var pretty any
	if err := json.Unmarshal(raw, &pretty); err != nil {
		panic(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(pretty); err != nil {
		panic(err)
	}
}

// --- command builder ---

func buildSpikeD5ShiftsListCmd(loader OnCallConfigLoader) *cobra.Command {
	var (
		flagAt       string
		flagFrom     string
		flagTo       string
		flagSchedule string
		flagUser     string
		flagTeam     string
		flagMine     bool
	)

	cmd := &cobra.Command{
		Use:   "d5",
		Short: "[spike] ADR 001 Decision 5 — shifts list filter composition",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			// Load client; require OAuth proxy mode (SA token path lacks filter_events on internal API).
			api, _, err := loader.LoadOnCallClient(ctx)
			cobra.CheckErr(err)
			c, ok := api.(*OnCallClient)
			if !ok {
				panic("spike d5 requires OAuth proxy mode (use a context with Grafana OAuth login, not SA token)")
			}

			// Resolve --mine shortcut.
			if flagMine && flagUser == "" {
				flagUser = "me"
			}

			// Resolve --user me → PK.
			var userPK string
			if flagUser == "me" {
				t0 := time.Now()
				u, err := c.GetCurrentUser(ctx)
				cobra.CheckErr(err)
				userPK = u.PK
				fmt.Fprintf(os.Stderr, "# resolved 'me' → PK=%s email=%s (%.0fms)\n", userPK, u.Email, time.Since(t0).Seconds()*1000)
				printJSON("current user", u)
			} else if flagUser != "" {
				userPK = flagUser
			}

			atTime, err := resolveTime(flagAt)
			cobra.CheckErr(err)
			fromTime, err := resolveTime(flagFrom)
			cobra.CheckErr(err)
			toTime, err := resolveTime(flagTo)
			cobra.CheckErr(err)

			// If no --to but --from set, default to +30d.
			if flagFrom != "" && flagTo == "" {
				toTime = fromTime.Add(30 * 24 * time.Hour)
			}
			// If no range flags set and --at is set, use atTime as fromTime with 0d (point-in-time).
			if flagFrom == "" && flagTo == "" {
				fromTime = atTime
				toTime = atTime
			}

			// === Scenario 1 + 3: --schedule is set ===
			if flagSchedule != "" {
				t0 := time.Now()

				// a) Get the schedule object — on_call_now field gives cheap "who is on call".
				rawSched, err := rawGET(ctx, c, fmt.Sprintf("%s%s/", schedulesPath, url.PathEscape(flagSchedule)))
				cobra.CheckErr(err)
				fmt.Fprintf(os.Stderr, "# GET schedules/%s/ took %.0fms\n", flagSchedule, time.Since(t0).Seconds()*1000)
				printRawJSON(fmt.Sprintf("scenario 1a — schedules/%s/ (on_call_now)", flagSchedule), rawSched)

				// b) filter_events for the window.
				// For point-in-time (--at now): back up 2 days so that events starting
				// before atTime but still active are captured. filter_events returns events
				// that overlap the [startDate, startDate+days] window.
				days := 3
				startDate := fromTime.Add(-48 * time.Hour)
				if !toTime.Equal(fromTime) {
					// range query: use exact window
					startDate = fromTime
					d := toTime.Sub(fromTime)
					days = int(d.Hours()/24) + 1
				}

				t1 := time.Now()
				eventsResp, err := c.ListFilterEvents(ctx, flagSchedule, "UTC", startDate.Format("2006-01-02T15:04:05"), days)
				cobra.CheckErr(err)
				fmt.Fprintf(os.Stderr, "# GET schedules/%s/filter_events took %.0fms (%d events)\n",
					flagSchedule, time.Since(t1).Seconds()*1000, len(eventsResp.Events))
				printJSON(fmt.Sprintf("scenario 1b/3 — filter_events for %s to %s (%d days)",
					startDate.Format(time.RFC3339), toTime.Format(time.RFC3339), days), eventsResp)

				// c) Derive "currently on call" from events: start <= atTime < end.
				fmt.Fprintf(os.Stderr, "\n# === scenario 1c — derived on-call at %s ===\n", atTime.Format(time.RFC3339))
				type derivedEvent struct {
					Start      string `json:"start"`
					End        string `json:"end"`
					IsGap      bool   `json:"is_gap"`
					IsOverride bool   `json:"is_override"`
					OnCallNow  bool   `json:"on_call_now"`
					Users      any    `json:"users"`
				}
				var derived []derivedEvent
				for _, e := range eventsResp.Events {
					eStart, err2 := time.Parse(time.RFC3339, e.Start)
					if err2 != nil {
						continue
					}
					eEnd, err2 := time.Parse(time.RFC3339, e.End)
					if err2 != nil {
						continue
					}
					onCallNow := !e.IsGap && !eStart.After(atTime) && atTime.Before(eEnd)
					derived = append(derived, derivedEvent{
						Start:      e.Start,
						End:        e.End,
						IsGap:      e.IsGap,
						IsOverride: e.IsOverride,
						OnCallNow:  onCallNow,
						Users:      e.Users,
					})
				}
				printJSON("scenario 1c — derived on-call events (on_call_now=true means active at atTime)", derived)

				// d) Canonical Shift shape via oncall_shifts/?schedule_id=<id>.
				// NOTE: the filter param is "schedule_id" (not "schedule") per IRM backend source:
				// apps/api/views/on_call_shifts.py line 138: request.query_params.get("schedule_id", None)
				t2 := time.Now()
				rawShifts, err := rawGET(ctx, c, fmt.Sprintf("%s?schedule_id=%s", shiftsPath, url.QueryEscape(flagSchedule)))
				cobra.CheckErr(err)
				fmt.Fprintf(os.Stderr, "# GET oncall_shifts/?schedule_id=%s took %.0fms\n", flagSchedule, time.Since(t2).Seconds()*1000)
				printRawJSON(fmt.Sprintf("scenario 3 — canonical Shift shape (oncall_shifts/?schedule_id=%s)", flagSchedule), rawShifts)

				// e) Try team filter on schedules (to probe API support).
				if flagTeam != "" {
					t3 := time.Now()
					rawTeamScheds, err2 := rawGET(ctx, c, fmt.Sprintf("%s?team=%s", schedulesPath, url.QueryEscape(flagTeam)))
					if err2 != nil {
						fmt.Fprintf(os.Stderr, "# team filter on schedules failed (expected if unsupported): %v\n", err2)
					} else {
						fmt.Fprintf(os.Stderr, "# GET schedules/?team=%s took %.0fms\n", flagTeam, time.Since(t3).Seconds()*1000)
						printRawJSON(fmt.Sprintf("scenario 4 — schedules/?team=%s", flagTeam), rawTeamScheds)
					}
				}

				return nil
			}

			// === Scenario 2: --user set, no --schedule — fan-out across all schedules ===
			if userPK != "" {
				fmt.Fprintf(os.Stderr, "\n# === scenario 2 — user=%s upcoming shifts (fan-out strategy) ===\n", userPK)
				t0 := time.Now()

				schedules, err := c.ListSchedules(ctx)
				cobra.CheckErr(err)
				fmt.Fprintf(os.Stderr, "# listed %d schedules (%.0fms)\n", len(schedules), time.Since(t0).Seconds()*1000)

				days := int(toTime.Sub(fromTime).Hours()/24) + 1
				if days < 1 {
					days = 1
				}

				type userEvent struct {
					ScheduleID   string `json:"schedule_id"`
					ScheduleName string `json:"schedule_name"`
					Start        string `json:"start"`
					End          string `json:"end"`
					IsGap        bool   `json:"is_gap"`
					IsOverride   bool   `json:"is_override"`
					Users        any    `json:"users"`
				}
				var myEvents []userEvent
				var totalRTTs int

				for _, sched := range schedules {
					t1 := time.Now()
					eventsResp, err2 := c.ListFilterEvents(ctx, sched.ID, "UTC", fromTime.Format("2006-01-02T15:04:05"), days)
					totalRTTs++
					if err2 != nil {
						fmt.Fprintf(os.Stderr, "# WARN: filter_events for schedule %s failed: %v\n", sched.ID, err2)
						continue
					}
					dur := time.Since(t1)
					for _, e := range eventsResp.Events {
						if e.IsGap {
							continue
						}
						for _, u := range e.Users {
							if u.PK == userPK {
								myEvents = append(myEvents, userEvent{
									ScheduleID:   sched.ID,
									ScheduleName: sched.Name,
									Start:        e.Start,
									End:          e.End,
									IsGap:        e.IsGap,
									IsOverride:   e.IsOverride,
									Users:        e.Users,
								})
								break
							}
						}
					}
					fmt.Fprintf(os.Stderr, "# schedule %s (%s): %d events in %.0fms\n",
						sched.ID, sched.Name, len(eventsResp.Events), dur.Seconds()*1000)
				}

				totalDur := time.Since(t0)
				fmt.Fprintf(os.Stderr, "# fan-out complete: %d RTTs, %d matching events, total=%.0fms\n",
					totalRTTs, len(myEvents), totalDur.Seconds()*1000)
				printJSON(fmt.Sprintf("scenario 2 — user %s events from %s to %s (%d day window, %d RTTs)",
					userPK, fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339), days, totalRTTs), myEvents)
				return nil
			}

			// === Scenario 4: --team set, no --schedule/--user ===
			if flagTeam != "" {
				fmt.Fprintf(os.Stderr, "\n# === scenario 4 — team=%s coverage ===\n", flagTeam)

				// Try API-side team filter on schedules.
				t0 := time.Now()
				rawTeamScheds, err2 := rawGET(ctx, c, fmt.Sprintf("%s?team=%s", schedulesPath, url.QueryEscape(flagTeam)))
				if err2 != nil {
					fmt.Fprintf(os.Stderr, "# team filter via API failed: %v\n# Trying client-side filter instead...\n", err2)
					// Fall back: list all schedules and filter by team field.
					schedules, err := c.ListSchedules(ctx)
					cobra.CheckErr(err)
					var teamScheds []Schedule
					for _, s := range schedules {
						// Team field is any; marshal+compare via JSON.
						b, _ := json.Marshal(s.Team)
						if string(b) != "null" && string(b) != `""` {
							teamScheds = append(teamScheds, s)
						}
					}
					fmt.Fprintf(os.Stderr, "# client-side team filter: %d/%d schedules have a team set (took %.0fms)\n",
						len(teamScheds), len(schedules), time.Since(t0).Seconds()*1000)
					printJSON("scenario 4 — schedules with team set (client-side filter)", teamScheds)
				} else {
					fmt.Fprintf(os.Stderr, "# API team filter on schedules succeeded (%.0fms)\n", time.Since(t0).Seconds()*1000)
					printRawJSON(fmt.Sprintf("scenario 4 — schedules/?team=%s (API-side filter)", flagTeam), rawTeamScheds)
				}
				return nil
			}

			fmt.Fprintf(os.Stderr, "# no scenario flags set — pass --schedule, --user, --mine, or --team\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&flagAt, "at", "now", "Point-in-time (now or RFC3339)")
	cmd.Flags().StringVar(&flagFrom, "from", "", "Range start (now, +30d, or RFC3339)")
	cmd.Flags().StringVar(&flagTo, "to", "", "Range end (now, +30d, or RFC3339)")
	cmd.Flags().StringVar(&flagSchedule, "schedule", "", "Schedule PK to query")
	cmd.Flags().StringVar(&flagUser, "user", "", "User PK or 'me'")
	cmd.Flags().StringVar(&flagTeam, "team", "", "Team ID to filter")
	cmd.Flags().BoolVar(&flagMine, "mine", false, "Shortcut for --user me")

	return cmd
}
