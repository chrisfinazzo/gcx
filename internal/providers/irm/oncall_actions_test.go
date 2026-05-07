package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// fakeOnCallAPI is a minimal stub used to drive runAcknowledge in tests
// without spinning up an httptest server. It embeds OnCallAPI so any
// method we don't override returns a nil-interface panic if called — the
// acknowledge code path only needs GetAlertGroup + AcknowledgeAlertGroup.
type fakeOnCallAPI struct {
	OnCallAPI

	getAlertGroupFn         func(context.Context, string) (*AlertGroup, error)
	acknowledgeAlertGroupFn func(context.Context, string) error
	calls                   []string
}

func (f *fakeOnCallAPI) GetAlertGroup(ctx context.Context, id string) (*AlertGroup, error) {
	if f.getAlertGroupFn != nil {
		return f.getAlertGroupFn(ctx, id)
	}
	return &AlertGroup{PK: id, Status: float64(0)}, nil // default: firing
}

func (f *fakeOnCallAPI) AcknowledgeAlertGroup(ctx context.Context, id string) error {
	f.calls = append(f.calls, id)
	if f.acknowledgeAlertGroupFn != nil {
		return f.acknowledgeAlertGroupFn(ctx, id)
	}
	return nil
}

// fakeLoader implements OnCallConfigLoader by returning the fake client.
type fakeLoader struct {
	client OnCallAPI
}

func (l *fakeLoader) LoadOnCallClient(_ context.Context) (OnCallAPI, string, error) {
	return l.client, "stacks-test", nil
}

// runAck drives runAcknowledge with captured stdout/stderr and a no-op exit
// function. Returns the captured streams, the returned error (if any), and
// the captured exit code (-1 if not called).
func runAck(t *testing.T, args []string, opts *alertGroupActionVerbOpts, fake *fakeOnCallAPI) (stdoutStr, stderrStr string, gotErr error, gotExit int) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetContext(context.Background())

	gotExit = -1
	prev := exitFuncForTesting
	exitFuncForTesting = func(code int) { gotExit = code }
	t.Cleanup(func() { exitFuncForTesting = prev })

	loader := &fakeLoader{client: fake}
	gotErr = runAcknowledge(cmd, args, opts, loader)
	return stdout.String(), stderr.String(), gotErr, gotExit
}

// resetAgentMode resets the agent-mode detection between tests.
func resetAgentMode(t *testing.T) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
}

// --- Guardrail tests ---

func TestRunAcknowledge_RequiresIDOrFilter(t *testing.T) {
	resetAgentMode(t)
	stdout, _, err, exit := runAck(t, nil, &alertGroupActionVerbOpts{}, &fakeOnCallAPI{})
	if exit != -1 {
		t.Errorf("did not expect os.Exit; got %d", exit)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout; got %q", stdout)
	}
	if err == nil {
		t.Fatal("expected runAcknowledge to return an error")
	}
	var de fail.DetailedError
	if !errors.As(err, &de) {
		t.Fatalf("expected DetailedError, got %T: %v", err, err)
	}
	if de.ExitCode == nil || *de.ExitCode != 2 {
		t.Errorf("expected exit 2; got %v", de.ExitCode)
	}
	if !strings.Contains(de.Summary, "argument or filter flag required") {
		t.Errorf("unexpected summary: %q", de.Summary)
	}
}

func TestRunAcknowledge_RejectsIDPlusFilter(t *testing.T) {
	resetAgentMode(t)
	opts := &alertGroupActionVerbOpts{Teams: []string{"prod-sre"}}
	_, _, err, _ := runAck(t, []string{"I123"}, opts, &fakeOnCallAPI{})
	if err == nil {
		t.Fatal("expected error for id+filter combination")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually exclusive error; got: %v", err)
	}
}

// --- Single-target tests ---

func TestRunAcknowledge_SingleTarget_Changes(t *testing.T) {
	resetAgentMode(t)
	fake := &fakeOnCallAPI{
		getAlertGroupFn: func(_ context.Context, id string) (*AlertGroup, error) {
			return &AlertGroup{PK: id, Status: float64(0)}, nil // firing
		},
	}
	stdout, _, err, exit := runAck(t, []string{"IABC"}, &alertGroupActionVerbOpts{}, fake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exit != -1 {
		t.Errorf("unexpected exit code %d (single-target success should return cleanly)", exit)
	}
	var got MutationResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout=%s", err, stdout)
	}
	if got.Action != "acknowledge" {
		t.Errorf("action: got %q want %q", got.Action, "acknowledge")
	}
	if got.Summary.Matched != 1 || got.Summary.Changed != 1 || got.Summary.Errors != 0 {
		t.Errorf("summary: got %+v want matched=1 changed=1 errors=0", got.Summary)
	}
	if len(got.Targets) != 1 {
		t.Fatalf("targets: got %d want 1", len(got.Targets))
	}
	if got.Targets[0].AlertGroupID != "IABC" || !got.Targets[0].Changed || got.Targets[0].Error != nil {
		t.Errorf("target[0]: got %+v", got.Targets[0])
	}
	if len(fake.calls) != 1 || fake.calls[0] != "IABC" {
		t.Errorf("expected 1 ack call for IABC; got %v", fake.calls)
	}
}

func TestRunAcknowledge_SingleTarget_IdempotentNoOp(t *testing.T) {
	resetAgentMode(t)
	fake := &fakeOnCallAPI{
		getAlertGroupFn: func(_ context.Context, id string) (*AlertGroup, error) {
			return &AlertGroup{PK: id, Status: float64(1)}, nil // already acknowledged
		},
	}
	stdout, _, _, _ := runAck(t, []string{"IDONE"}, &alertGroupActionVerbOpts{}, fake)
	var got MutationResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout JSON: %v\nstdout=%s", err, stdout)
	}
	if got.Summary.Matched != 1 || got.Summary.Changed != 0 || got.Summary.Errors != 0 {
		t.Errorf("summary: got %+v want matched=1 changed=0 errors=0", got.Summary)
	}
	if got.Targets[0].Changed {
		t.Errorf("expected idempotent changed:false, got Changed=true")
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected zero acknowledge calls (idempotent skip); got %v", fake.calls)
	}
}

func TestRunAcknowledge_SingleTarget_ApplyError(t *testing.T) {
	resetAgentMode(t)
	fake := &fakeOnCallAPI{
		getAlertGroupFn: func(_ context.Context, id string) (*AlertGroup, error) {
			return &AlertGroup{PK: id, Status: float64(0)}, nil
		},
		acknowledgeAlertGroupFn: func(_ context.Context, _ string) error {
			return errors.New("backend boom")
		},
	}
	stdout, _, _, exit := runAck(t, []string{"IFAIL"}, &alertGroupActionVerbOpts{}, fake)
	if exit != 1 {
		t.Errorf("expected exit 1; got %d", exit)
	}
	var got MutationResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout JSON: %v\nstdout=%s", err, stdout)
	}
	if got.Summary.Errors != 1 || got.Summary.Changed != 0 {
		t.Errorf("summary: got %+v want errors=1 changed=0", got.Summary)
	}
	if got.Targets[0].Error == nil || got.Targets[0].Error.Code != "acknowledge_failed" {
		t.Errorf("expected error envelope; got %+v", got.Targets[0].Error)
	}
}

// --- MutationResult builder tests ---

func TestMutationResult_JSONShape(t *testing.T) {
	r := MutationResult{
		Action:  "acknowledge",
		Summary: MutationSummary{Matched: 3, Changed: 2, Errors: 1},
		Targets: []MutationTargetResult{
			{AlertGroupID: "I1", Changed: true},
			{AlertGroupID: "I2", Changed: false},
			{AlertGroupID: "I3", Changed: false, Error: &MutationTargetError{Code: "x", Message: "y"}},
		},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := map[string]any{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"action", "summary", "targets"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing top-level key %q in %s", k, string(b))
		}
	}
	sumKeys := got["summary"].(map[string]any)
	for _, k := range []string{"matched", "changed", "errors"} {
		if _, ok := sumKeys[k]; !ok {
			t.Errorf("summary missing key %q", k)
		}
	}
	// Confirm targets[*].changed and targets[*].alertGroupID are present.
	tgts := got["targets"].([]any)
	if len(tgts) != 3 {
		t.Fatalf("expected 3 targets; got %d", len(tgts))
	}
	first := tgts[0].(map[string]any)
	if _, ok := first["alertGroupID"]; !ok {
		t.Errorf("missing alertGroupID on target[0]")
	}
	if _, ok := first["changed"]; !ok {
		t.Errorf("missing changed on target[0]")
	}
}

// --- Confirmation prompt tests ---

func TestConfirmTTY_YesVariants(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"", false},
		{"abc\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			stdin := strings.NewReader(tc.input)
			stderr := &bytes.Buffer{}
			got, err := confirmTTY(stdin, stderr, "About to do thing.")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v want %v for input %q", got, tc.want, tc.input)
			}
			if !strings.Contains(stderr.String(), "About to do thing.") {
				t.Errorf("prompt not written to stderr: %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), "[y/N]") {
				t.Errorf("expected [y/N] in prompt: %q", stderr.String())
			}
		})
	}
}

// --- toListFilters tests ---

func TestToListFilters_Defaults(t *testing.T) {
	opts := &alertGroupActionVerbOpts{Teams: []string{"prod-sre"}}
	f, err := opts.toListFilters()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Default: status filter excludes resolved (no 2 in the slice), is_root=true.
	if len(f.Statuses) != 3 {
		t.Errorf("expected 3 default statuses (firing+ack+silenced); got %v", f.Statuses)
	}
	if f.IsRoot == nil || !*f.IsRoot {
		t.Errorf("expected is_root=true by default; got %v", f.IsRoot)
	}
	if len(f.Teams) != 1 || f.Teams[0] != "prod-sre" {
		t.Errorf("teams: got %v", f.Teams)
	}
}

func TestToListFilters_AllBypassesDefaults(t *testing.T) {
	opts := &alertGroupActionVerbOpts{All: true}
	f, _ := opts.toListFilters()
	if len(f.Statuses) != 0 {
		t.Errorf("--all should drop status filter; got %v", f.Statuses)
	}
	if f.IsRoot != nil {
		t.Errorf("--all should drop is_root filter; got %v", f.IsRoot)
	}
}

func TestToListFilters_ExplicitState(t *testing.T) {
	opts := &alertGroupActionVerbOpts{States: []string{"firing"}}
	f, err := opts.toListFilters()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(f.Statuses) != 1 || f.Statuses[0] != 0 {
		t.Errorf("expected [0] (firing only); got %v", f.Statuses)
	}
}

func TestToListFilters_InvalidState(t *testing.T) {
	opts := &alertGroupActionVerbOpts{States: []string{"bogus"}}
	_, err := opts.toListFilters()
	if err == nil {
		t.Fatal("expected error for invalid --state")
	}
	if !strings.Contains(err.Error(), "invalid --state") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- HasAnyFilter tests ---

func TestHasAnyFilter(t *testing.T) {
	cases := []struct {
		name string
		opts *alertGroupActionVerbOpts
		want bool
	}{
		{"empty", &alertGroupActionVerbOpts{}, false},
		{"yes only", &alertGroupActionVerbOpts{Yes: true}, false},
		{"max-age", &alertGroupActionVerbOpts{MaxAge: "1h"}, true},
		{"team", &alertGroupActionVerbOpts{Teams: []string{"a"}}, true},
		{"state", &alertGroupActionVerbOpts{States: []string{"firing"}}, true},
		{"integration", &alertGroupActionVerbOpts{Integrations: []string{"x"}}, true},
		{"mine", &alertGroupActionVerbOpts{Mine: true}, true},
		{"all", &alertGroupActionVerbOpts{All: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.hasAnyFilter(); got != tc.want {
				t.Errorf("got %v want %v for %+v", got, tc.want, tc.opts)
			}
		})
	}
}
