package irm

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/irm/oncalltypes"
)

// TestStringifyAlertGroupListFilters covers the round-17 filter-summary
// stringifier used by the post-result hint on `alert-groups list`.
//
// Coverage:
//   - default-only (no flags) → fixed phrase exposing implicit exclusions,
//   - explicit flags → ordered, comma-joined description prefixed with
//     "default + " when implicit exclusions are still in effect,
//   - --all alone → "all" (caller is expected to suppress emission entirely
//     in this case via alertGroupListHasExplicitFilter — but the stringifier
//     still produces a sensible value),
//   - explicit --state suppresses the "default + " prefix (status-default no
//     longer applies),
//   - very long combinations collapse to the "<N filters>" fallback.
func TestStringifyAlertGroupListFilters(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   *alertGroupListOpts
		want string
	}{
		{
			name: "default only",
			in:   &alertGroupListOpts{},
			want: "default (excludes resolved + child groups)",
		},
		{
			name: "all alone",
			in:   &alertGroupListOpts{All: true},
			want: "all",
		},
		{
			name: "team only — defaults still in effect",
			in:   &alertGroupListOpts{Teams: []string{"prod-sre"}},
			want: "default + team=prod-sre",
		},
		{
			name: "explicit state — defaults dropped",
			in:   &alertGroupListOpts{States: []string{"firing"}},
			want: "status=firing",
		},
		{
			name: "max-age augments default",
			in:   &alertGroupListOpts{MaxAge: "24h"},
			want: "default + max-age=24h",
		},
		{
			name: "include-child-groups + team",
			in: &alertGroupListOpts{
				Teams:              []string{"prod-sre"},
				IncludeChildGroups: true,
			},
			want: "team=prod-sre, include-child-groups",
		},
		{
			name: "many filters collapse to count",
			in: &alertGroupListOpts{
				States:             []string{"firing", "acknowledged", "silenced"},
				Teams:              []string{"team-with-very-long-identifier-1", "team-with-very-long-identifier-2"},
				Integrations:       []string{"integration-with-a-long-name"},
				MaxAge:             "168h",
				Mine:               true,
				WithResolutionNote: true,
				HasRelatedIncident: true,
			},
			want: "7 filters",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stringifyAlertGroupListFilters(tc.in)
			if got != tc.want {
				t.Errorf("stringifyAlertGroupListFilters() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAlertGroupListHasExplicitFilter — silent-on-`--all` decision predicate.
func TestAlertGroupListHasExplicitFilter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   *alertGroupListOpts
		want bool
	}{
		{"empty", &alertGroupListOpts{}, false},
		{"all alone", &alertGroupListOpts{All: true}, false},
		{"team", &alertGroupListOpts{Teams: []string{"x"}}, true},
		{"max-age", &alertGroupListOpts{MaxAge: "1h"}, true},
		{"mine", &alertGroupListOpts{Mine: true}, true},
		{"include-child-groups", &alertGroupListOpts{IncludeChildGroups: true}, true},
		{"all + team", &alertGroupListOpts{All: true, Teams: []string{"x"}}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := alertGroupListHasExplicitFilter(tc.in); got != tc.want {
				t.Errorf("alertGroupListHasExplicitFilter() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFirstAlertRuleUID — first-occurrence-wins extraction used by the
// list-alerts rule-pivot hints.
func TestFirstAlertRuleUID(t *testing.T) {
	t.Parallel()
	mkEnv := func(uid string) alertEnvelope {
		var links *oncalltypes.AlertLinks
		if uid != "" {
			links = &oncalltypes.AlertLinks{
				Alert: &oncalltypes.AlertLinkAlert{
					Rule: &oncalltypes.AlertRule{UID: uid},
				},
			}
		}
		return alertEnvelope{
			Status: oncalltypes.AlertStatus{Links: links},
		}
	}

	cases := []struct {
		name string
		in   []alertEnvelope
		want string
	}{
		{"empty slice", nil, ""},
		{"all empty", []alertEnvelope{mkEnv(""), mkEnv("")}, ""},
		{"single", []alertEnvelope{mkEnv("rule-1")}, "rule-1"},
		{"multi same", []alertEnvelope{mkEnv("rule-1"), mkEnv("rule-1")}, "rule-1"},
		{"multi differ — first wins", []alertEnvelope{mkEnv("rule-a"), mkEnv("rule-b")}, "rule-a"},
		{"empty then non-empty", []alertEnvelope{mkEnv(""), mkEnv("rule-c")}, "rule-c"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := firstAlertRuleUID(tc.in)
			if got != tc.want {
				t.Errorf("firstAlertRuleUID() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAlertRuleCellPreferURL / TestAlertDashboardCellPreferURL — table-codec
// per-cell URL-over-UID precedence (D2 round 17 default columns).
func TestAlertRuleCellPreferURL(t *testing.T) {
	t.Parallel()
	mk := func(uid, urlStr string) alertEnvelope {
		return alertEnvelope{
			Status: oncalltypes.AlertStatus{
				Links: &oncalltypes.AlertLinks{
					Alert: &oncalltypes.AlertLinkAlert{
						Rule: &oncalltypes.AlertRule{UID: uid, URL: urlStr},
					},
				},
			},
		}
	}
	cases := []struct {
		name string
		in   alertEnvelope
		want string
	}{
		{"empty links", alertEnvelope{}, "-"},
		{"uid only", mk("uid-1", ""), "uid-1"},
		{"url preferred", mk("uid-1", "https://example.com/rule"), "https://example.com/rule"},
		{"both empty", mk("", ""), "-"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := alertRuleCellPreferURL(tc.in)
			if got != tc.want {
				t.Errorf("alertRuleCellPreferURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAlertDashboardCellPreferURL(t *testing.T) {
	t.Parallel()
	mk := func(uid, urlStr string) alertEnvelope {
		return alertEnvelope{
			Status: oncalltypes.AlertStatus{
				Links: &oncalltypes.AlertLinks{
					Dashboard: &oncalltypes.AlertDashboard{UID: uid, URL: urlStr},
				},
			},
		}
	}
	cases := []struct {
		name string
		in   alertEnvelope
		want string
	}{
		{"empty links", alertEnvelope{}, "-"},
		{"uid only", mk("dash-1", ""), "dash-1"},
		{"url preferred", mk("dash-1", "https://example.com/d/dash-1"), "https://example.com/d/dash-1"},
		{"both empty", mk("", ""), "-"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := alertDashboardCellPreferURL(tc.in)
			if got != tc.want {
				t.Errorf("alertDashboardCellPreferURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
