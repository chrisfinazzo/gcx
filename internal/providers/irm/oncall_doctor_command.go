package irm

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// doctor command — "check your own config vs the expected configuration"
//
// Resolves the current user via the plugin-proxy (GetCurrentUser on the active gcx
// context), then looks that user up in the org compliance evaluation. The evaluation
// itself lives only on the OnCall public API today, so doctor reuses the same TEST-ONLY
// direct transport as `compliance-rules evaluate` (--oncall-url/--oncall-token/--instance-id).
// ---------------------------------------------------------------------------

type doctorOpts struct {
	IO        cmdio.Options
	transport complianceTransport
	UserID    string
}

func newDoctorCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &doctorOpts{}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check whether your OnCall notification setup meets the org's compliance rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.transport.URL == "" {
				return errors.New("doctor requires --oncall-url (test-only) until the endpoint is exposed via the plugin proxy")
			}

			userID := opts.UserID
			if userID == "" {
				client, _, err := loader.LoadOnCallClient(cmd.Context())
				if err != nil {
					return fmt.Errorf("resolve current user: %w", err)
				}
				u, err := client.GetCurrentUser(cmd.Context())
				if err != nil {
					return fmt.Errorf("resolve current user: %w", err)
				}
				userID = u.PK
			}

			public := newPublicComplianceClient(opts.transport.URL, opts.transport.Token, opts.transport.InstanceID)
			eval, err := public.Evaluate(cmd.Context())
			if err != nil {
				return err
			}

			res := doctorResultFor(userID, eval)
			if users, uerr := public.ListUsers(cmd.Context()); uerr == nil {
				if info, ok := users[userID]; ok {
					res.Email = info.Email
					res.Username = info.Username
				}
			}
			return opts.IO.Encode(cmd.OutOrStdout(), res)
		},
	}
	registerComplianceCodecs(&opts.IO, cmd.Flags(), &doctorTextCodec{})
	opts.transport.bind(cmd.Flags())
	cmd.Flags().StringVar(&opts.UserID, "user", "", "OnCall user ID to check (defaults to the current user)")
	return cmd
}

// DoctorResult is one user's compliance verdict.
type DoctorResult struct {
	UserID     string   `json:"user_id"`
	Email      string   `json:"email,omitempty"`
	Username   string   `json:"username,omitempty"`
	Compliant  bool     `json:"compliant"`
	Found      bool     `json:"found"`
	Violations []string `json:"violations,omitempty"`
}

// label renders "name (user_id)", preferring email, then username, then just the ID.
func (r DoctorResult) label() string {
	switch {
	case r.Email != "":
		return fmt.Sprintf("%s (%s)", r.Email, r.UserID)
	case r.Username != "":
		return fmt.Sprintf("%s (%s)", r.Username, r.UserID)
	default:
		return r.UserID
	}
}

// doctorResultFor finds userID in the evaluation report. A user absent from both lists
// is reported as not found (the backend may omit users with no notification setup).
func doctorResultFor(userID string, e *ComplianceEvaluation) DoctorResult {
	for _, id := range e.Compliant {
		if id == userID {
			return DoctorResult{UserID: userID, Compliant: true, Found: true}
		}
	}
	for _, u := range e.NonCompliant {
		if u.UserID == userID {
			return DoctorResult{UserID: userID, Compliant: false, Found: true, Violations: u.Violations}
		}
	}
	return DoctorResult{UserID: userID, Compliant: false, Found: false}
}

// doctorTextCodec renders a DoctorResult as a human verdict.
type doctorTextCodec struct{}

func (c *doctorTextCodec) Format() format.Format { return format.Format("text") }

func (c *doctorTextCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(DoctorResult)
	if !ok {
		return fmt.Errorf("text codec: unsupported value type %T (expected DoctorResult)", v)
	}
	switch {
	case !r.Found:
		_, err := fmt.Fprintf(w, "? %s was not found in the org evaluation report (no notification setup?).\n", r.label())
		return err
	case r.Compliant:
		_, err := fmt.Fprintf(w, "✓ %s is ready to be paged — compliant with the org's notification rules.\n", r.label())
		return err
	default:
		if _, err := fmt.Fprintf(w, "✗ %s is NOT compliant with the org's notification rules:\n", r.label()); err != nil {
			return err
		}
		for _, v := range r.Violations {
			if _, err := fmt.Fprintf(w, "    - %s\n", friendlyViolation(v)); err != nil {
				return err
			}
		}
		return nil
	}
}

func (c *doctorTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// friendlyViolation replaces raw enum tokens (notify_by_*, channel names) inside a
// backend violation sentence with their friendly labels, leaving everything else intact.
func friendlyViolation(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		if l, ok := complianceLabels[f]; ok {
			fields[i] = l
		}
	}
	return strings.Join(fields, " ")
}
