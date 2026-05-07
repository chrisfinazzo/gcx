package irm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// Resource group command builder
// ---------------------------------------------------------------------------

type listOpts struct {
	IO       cmdio.Options
	Resource string
}

func (o *listOpts) setup(flags *pflag.FlagSet, resource string) {
	o.Resource = resource
	switch resource {
	case "integrations":
		o.IO.RegisterCustomCodec("table", &integrationTableCodec{})
		o.IO.RegisterCustomCodec("wide", &integrationTableCodec{Wide: true})
	case "escalation-chains":
		o.IO.RegisterCustomCodec("table", &escalationChainTableCodec{})
	case "escalation-policies":
		o.IO.RegisterCustomCodec("table", &escalationPolicyTableCodec{})
		o.IO.RegisterCustomCodec("wide", &escalationPolicyTableCodec{Wide: true})
	case "schedules":
		o.IO.RegisterCustomCodec("table", &scheduleTableCodec{})
		o.IO.RegisterCustomCodec("wide", &scheduleTableCodec{Wide: true})
	case "shifts":
		o.IO.RegisterCustomCodec("table", &shiftTableCodec{})
		o.IO.RegisterCustomCodec("wide", &shiftTableCodec{Wide: true})
	case "routes":
		o.IO.RegisterCustomCodec("table", &routeTableCodec{})
		o.IO.RegisterCustomCodec("wide", &routeTableCodec{Wide: true})
	case "webhooks":
		o.IO.RegisterCustomCodec("table", &webhookTableCodec{})
		o.IO.RegisterCustomCodec("wide", &webhookTableCodec{Wide: true})
	case "alert-groups":
		o.IO.RegisterCustomCodec("table", &alertGroupTableCodec{})
		o.IO.RegisterCustomCodec("wide", &alertGroupTableCodec{Wide: true})
	case "users":
		o.IO.RegisterCustomCodec("table", &userTableCodec{})
		o.IO.RegisterCustomCodec("wide", &userTableCodec{Wide: true})
	case "teams":
		o.IO.RegisterCustomCodec("table", &teamTableCodec{})
	case "user-groups":
		o.IO.RegisterCustomCodec("table", &userGroupTableCodec{})
	case "slack-channels":
		o.IO.RegisterCustomCodec("table", &slackChannelTableCodec{})
	case "alerts":
		o.IO.RegisterCustomCodec("table", &alertTableCodec{})
		o.IO.RegisterCustomCodec("wide", &alertTableCodec{Wide: true})
	case "organizations":
		o.IO.RegisterCustomCodec("table", &organizationTableCodec{})
	case "resolution-notes":
		o.IO.RegisterCustomCodec("table", &resolutionNoteTableCodec{})
		o.IO.RegisterCustomCodec("wide", &resolutionNoteTableCodec{Wide: true})
	case "shift-swaps":
		o.IO.RegisterCustomCodec("table", &shiftSwapTableCodec{})
		o.IO.RegisterCustomCodec("wide", &shiftSwapTableCodec{Wide: true})
	}
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

// newListSubcommand creates a "list" subcommand using TypedCRUD.
func newListSubcommand[T adapter.ResourceNamer](
	loader OnCallConfigLoader, resource, kind, short string, idField string,
	listFn func(ctx context.Context, client OnCallAPI) ([]T, error),
	getFn func(ctx context.Context, client OnCallAPI, name string) (*T, error),
	opts ...crudOption[T],
) *cobra.Command {
	lo := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := lo.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, namespace, err := newTypedCRUD(ctx, loader, listFn, getFn, opts...)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, 0)
			if err != nil {
				return err
			}

			specs := make([]T, len(typedObjs))
			for i, obj := range typedObjs {
				specs[i] = obj.Spec
			}
			objs, err := itemsToUnstructured(specs, kind, idField, namespace)
			if err != nil {
				return err
			}

			return lo.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	lo.setup(cmd.Flags(), resource)
	return cmd
}

// newGetSubcommand creates a "get <id>" subcommand using TypedCRUD.
func newGetSubcommand[T adapter.ResourceNamer](
	loader OnCallConfigLoader, short string,
	getFn func(ctx context.Context, client OnCallAPI, name string) (*T, error),
) *cobra.Command {
	go2 := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := go2.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := newTypedCRUD(ctx, loader, func(_ context.Context, _ OnCallAPI) ([]T, error) { return nil, nil }, getFn)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return err
			}

			return go2.IO.Encode(cmd.OutOrStdout(), typedObj.Spec)
		},
	}
	go2.setup(cmd.Flags())
	return cmd
}

// crudOption configures optional CRUD operations on a TypedCRUD instance.
type crudOption[T adapter.ResourceNamer] func(client OnCallAPI, crud *adapter.TypedCRUD[T])

func newTypedCRUD[T adapter.ResourceNamer](
	ctx context.Context,
	loader OnCallConfigLoader,
	listFn func(ctx context.Context, client OnCallAPI) ([]T, error),
	getFn func(ctx context.Context, client OnCallAPI, name string) (*T, error),
	opts ...crudOption[T],
) (*adapter.TypedCRUD[T], string, error) {
	client, namespace, err := loader.LoadOnCallClient(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load IRM OnCall config: %w", err)
	}

	crud := &adapter.TypedCRUD[T]{
		ListFn:      adapter.LimitedListFn(func(ctx context.Context) ([]T, error) { return listFn(ctx, client) }),
		StripFields: DefaultStripFields,
		Namespace:   namespace,
	}

	if getFn != nil {
		crud.GetFn = func(ctx context.Context, name string) (*T, error) { return getFn(ctx, client, name) }
	} else {
		crud.GetFn = func(_ context.Context, _ string) (*T, error) { return nil, errors.ErrUnsupported }
	}

	for _, opt := range opts {
		opt(client, crud)
	}

	return crud, namespace, nil
}

// ---------------------------------------------------------------------------
// Per-resource group commands: oncall <resource> list|get|...
// ---------------------------------------------------------------------------

func newIntegrationsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "integrations",
		Short:   "Manage OnCall integrations.",
		Aliases: []string{"integration"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "integrations", "Integration", "List OnCall integrations.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Integration, error) { return c.ListIntegrations(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*Integration, error) {
				return c.GetIntegration(ctx, name)
			}),
		newGetSubcommand(loader, "Get an integration by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Integration, error) {
				return c.GetIntegration(ctx, name)
			}),
	)
	return cmd
}

func newEscalationChainsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "escalation-chains",
		Short:   "Manage escalation chains.",
		Aliases: []string{"escalation-chain", "ec"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "escalation-chains", "EscalationChain", "List escalation chains.", "id",
			func(ctx context.Context, c OnCallAPI) ([]EscalationChain, error) {
				return c.ListEscalationChains(ctx)
			},
			func(ctx context.Context, c OnCallAPI, name string) (*EscalationChain, error) {
				return c.GetEscalationChain(ctx, name)
			}),
		newGetSubcommand(loader, "Get an escalation chain by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*EscalationChain, error) {
				return c.GetEscalationChain(ctx, name)
			}),
	)
	return cmd
}

func newEscalationPoliciesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "escalation-policies",
		Short:   "Manage escalation policies.",
		Aliases: []string{"escalation-policy", "ep"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "escalation-policies", "EscalationPolicy", "List escalation policies.", "id",
			func(ctx context.Context, c OnCallAPI) ([]EscalationPolicy, error) {
				return c.ListEscalationPolicies(ctx, "")
			},
			func(ctx context.Context, c OnCallAPI, name string) (*EscalationPolicy, error) {
				return c.GetEscalationPolicy(ctx, name)
			}),
		newGetSubcommand(loader, "Get an escalation policy by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*EscalationPolicy, error) {
				return c.GetEscalationPolicy(ctx, name)
			}),
	)
	return cmd
}

func newSchedulesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "schedules",
		Short:   "Manage OnCall schedules.",
		Aliases: []string{"schedule"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "schedules", "Schedule", "List OnCall schedules.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Schedule, error) { return c.ListSchedules(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*Schedule, error) {
				return c.GetSchedule(ctx, name)
			}),
		newGetSubcommand(loader, "Get a schedule by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Schedule, error) {
				return c.GetSchedule(ctx, name)
			}),
		newScheduleFinalShiftsCommand(loader),
	)
	return cmd
}

func newShiftsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "shifts",
		Short:   "Manage OnCall shifts.",
		Aliases: []string{"shift"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "shifts", "Shift", "List OnCall shifts.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Shift, error) { return c.ListShifts(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*Shift, error) { return c.GetShift(ctx, name) }),
		newGetSubcommand(loader, "Get a shift by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Shift, error) { return c.GetShift(ctx, name) }),
	)
	return cmd
}

func newRoutesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "routes",
		Short:   "Manage OnCall routes.",
		Aliases: []string{"route"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "routes", "Route", "List OnCall routes.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Route, error) { return c.ListRoutes(ctx, "") },
			func(ctx context.Context, c OnCallAPI, name string) (*Route, error) { return c.GetRoute(ctx, name) }),
		newGetSubcommand(loader, "Get a route by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Route, error) { return c.GetRoute(ctx, name) }),
	)
	return cmd
}

func newWebhooksCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "webhooks",
		Short:   "Manage outgoing webhooks.",
		Aliases: []string{"webhook"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "webhooks", "Webhook", "List outgoing webhooks.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Webhook, error) { return c.ListWebhooks(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*Webhook, error) {
				return c.GetWebhook(ctx, name)
			}),
		newGetSubcommand(loader, "Get an outgoing webhook by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Webhook, error) {
				return c.GetWebhook(ctx, name)
			}),
	)
	return cmd
}

func newTeamsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "teams",
		Short:   "Manage OnCall teams.",
		Aliases: []string{"team"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "teams", "Team", "List OnCall teams.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Team, error) { return c.ListTeams(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*Team, error) { return c.GetTeam(ctx, name) }),
		newGetSubcommand(loader, "Get a team by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Team, error) { return c.GetTeam(ctx, name) }),
	)
	return cmd
}

func newUserGroupsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "user-groups",
		Short:   "List user groups.",
		Aliases: []string{"user-group"},
	}
	cmd.AddCommand(
		newListSubcommand[UserGroup](loader, "user-groups", "UserGroup", "List user groups.", "id",
			func(ctx context.Context, c OnCallAPI) ([]UserGroup, error) { return c.ListUserGroups(ctx) },
			nil),
	)
	return cmd
}

func newSlackChannelsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "slack-channels",
		Short:   "List Slack channels.",
		Aliases: []string{"slack-channel"},
	}
	cmd.AddCommand(
		newListSubcommand[SlackChannel](loader, "slack-channels", "SlackChannel", "List Slack channels.", "id",
			func(ctx context.Context, c OnCallAPI) ([]SlackChannel, error) { return c.ListSlackChannels(ctx) },
			nil),
	)
	return cmd
}

func newOrganizationsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "organizations",
		Short:   "View organization info.",
		Aliases: []string{"organization", "org"},
	}
	opts := &getOpts{}
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get organization info.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}
			org, err := client.GetOrganization(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), org)
		},
	}
	opts.setup(getCmd.Flags())
	cmd.AddCommand(getCmd)
	return cmd
}

func newResolutionNotesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "resolution-notes",
		Short:   "Manage resolution notes.",
		Aliases: []string{"resolution-note", "rn"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "resolution-notes", "ResolutionNote", "List resolution notes.", "id",
			func(ctx context.Context, c OnCallAPI) ([]ResolutionNote, error) {
				return c.ListResolutionNotes(ctx, "")
			},
			func(ctx context.Context, c OnCallAPI, name string) (*ResolutionNote, error) {
				return c.GetResolutionNote(ctx, name)
			}),
		newGetSubcommand(loader, "Get a resolution note by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*ResolutionNote, error) {
				return c.GetResolutionNote(ctx, name)
			}),
	)
	return cmd
}

func newShiftSwapsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "shift-swaps",
		Short:   "Manage shift swaps.",
		Aliases: []string{"shift-swap", "ss"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "shift-swaps", "ShiftSwap", "List shift swaps.", "id",
			func(ctx context.Context, c OnCallAPI) ([]ShiftSwap, error) { return c.ListShiftSwaps(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*ShiftSwap, error) {
				return c.GetShiftSwap(ctx, name)
			}),
		newGetSubcommand(loader, "Get a shift swap by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*ShiftSwap, error) {
				return c.GetShiftSwap(ctx, name)
			}),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// Table codecs — accept []unstructured.Unstructured (Pattern 13 compliant)
// ---------------------------------------------------------------------------

// noDecodeCodec is embedded in all table codecs to provide the shared
// Decode stub — table format is output-only.
type noDecodeCodec struct{}

func (noDecodeCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func specStr(obj unstructured.Unstructured, key string) string {
	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		return ""
	}
	v, ok := spec[key]
	if !ok {
		return ""
	}
	return fmt.Sprint(v)
}

func specInt(obj unstructured.Unstructured, key string) int {
	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		return 0
	}
	v, ok := spec[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func specBool(obj unstructured.Unstructured, key string) bool {
	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		return false
	}
	v, _ := spec[key].(bool)
	return v
}

func toUnstructuredSlice(v any) ([]unstructured.Unstructured, error) {
	items, ok := v.([]unstructured.Unstructured)
	if !ok {
		return nil, errors.New("invalid data type for table codec: expected []unstructured.Unstructured")
	}
	return items, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

// --- Integration codec (internal: verbal_name, integration, team) ---

type integrationTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *integrationTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *integrationTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "TYPE", "TEAM", "URL")
	} else {
		t = style.NewTable("ID", "NAME", "TYPE")
	}
	for _, obj := range items {
		id := obj.GetName()
		name := specStr(obj, "verbal_name")
		if !c.Wide {
			name = truncate(name, 50)
		}
		if c.Wide {
			t.Row(id, name, specStr(obj, "integration"), orDash(specStr(obj, "team")), orDash(specStr(obj, "integration_url")))
		} else {
			t.Row(id, name, specStr(obj, "integration"))
		}
	}
	return t.Render(w)
}

// --- EscalationChain codec ---

type escalationChainTableCodec struct{ noDecodeCodec }

func (c *escalationChainTableCodec) Format() format.Format { return "table" }

func (c *escalationChainTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "NAME", "TEAM")
	for _, obj := range items {
		t.Row(obj.GetName(), specStr(obj, "name"), orDash(specStr(obj, "team")))
	}
	return t.Render(w)
}

// --- EscalationPolicy codec (internal: step, wait_delay, escalation_chain) ---

type escalationPolicyTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *escalationPolicyTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *escalationPolicyTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "CHAIN", "STEP", "WAIT-DELAY", "IMPORTANT", "NOTIFY-SCHEDULE")
	} else {
		t = style.NewTable("ID", "CHAIN", "STEP", "WAIT-DELAY")
	}
	for _, obj := range items {
		id := obj.GetName()
		waitDelay := orDash(specStr(obj, "wait_delay"))
		if c.Wide {
			important := "false"
			if specBool(obj, "important") {
				important = "true"
			}
			t.Row(id, specStr(obj, "escalation_chain"), specStr(obj, "step"), waitDelay, important, orDash(specStr(obj, "notify_schedule")))
		} else {
			t.Row(id, specStr(obj, "escalation_chain"), specStr(obj, "step"), waitDelay)
		}
	}
	return t.Render(w)
}

// --- Schedule codec ---

type scheduleTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *scheduleTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *scheduleTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "TYPE", "TIMEZONE", "TEAM")
	} else {
		t = style.NewTable("ID", "NAME", "TYPE", "TIMEZONE")
	}
	for _, obj := range items {
		id := obj.GetName()
		tz := orDash(specStr(obj, "time_zone"))
		if c.Wide {
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), tz, orDash(specStr(obj, "team")))
		} else {
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), tz)
		}
	}
	return t.Render(w)
}

// --- Shift codec (internal: shift_start, shift_end, priority_level) ---

type shiftTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *shiftTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *shiftTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "TYPE", "START", "END", "FREQUENCY", "INTERVAL")
	} else {
		t = style.NewTable("ID", "NAME", "TYPE", "START", "END")
	}
	for _, obj := range items {
		id := obj.GetName()
		start := orDash(specStr(obj, "shift_start"))
		end := orDash(specStr(obj, "shift_end"))
		if c.Wide {
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), start, end, orDash(specStr(obj, "frequency")), strconv.Itoa(specInt(obj, "interval")))
		} else {
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), start, end)
		}
	}
	return t.Render(w)
}

// --- Route codec (internal: alert_receive_channel, escalation_chain, filtering_term) ---

type routeTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *routeTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *routeTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "INTEGRATION", "CHAIN", "FILTER-TYPE", "FILTER", "DEFAULT")
	} else {
		t = style.NewTable("ID", "INTEGRATION", "CHAIN", "FILTER-TYPE")
	}
	for _, obj := range items {
		id := obj.GetName()
		if c.Wide {
			isDefault := "false"
			if specBool(obj, "is_default") {
				isDefault = "true"
			}
			filter := orDash(specStr(obj, "filtering_term"))
			if len(filter) > 40 {
				filter = filter[:37] + "..."
			}
			t.Row(id, specStr(obj, "alert_receive_channel"), orDash(specStr(obj, "escalation_chain")), orDash(specStr(obj, "filtering_term_type")), filter, isDefault)
		} else {
			t.Row(id, specStr(obj, "alert_receive_channel"), orDash(specStr(obj, "escalation_chain")), orDash(specStr(obj, "filtering_term_type")))
		}
	}
	return t.Render(w)
}

// --- Webhook codec ---

type webhookTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *webhookTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *webhookTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "URL", "METHOD", "TRIGGER", "ENABLED")
	} else {
		t = style.NewTable("ID", "NAME", "TRIGGER", "ENABLED")
	}
	for _, obj := range items {
		id := obj.GetName()
		enabled := "false"
		if specBool(obj, "is_webhook_enabled") {
			enabled = "true"
		}
		if c.Wide {
			t.Row(id, specStr(obj, "name"), orDash(specStr(obj, "url")), orDash(specStr(obj, "http_method")), specStr(obj, "trigger_type"), enabled)
		} else {
			t.Row(id, specStr(obj, "name"), specStr(obj, "trigger_type"), enabled)
		}
	}
	return t.Render(w)
}

// --- AlertGroup codec (typed: accepts []alertGroupEnvelope) ---
//
// The list path emits typed envelopes (not unstructured.Unstructured) so JSON
// and YAML output preserve the deliberate field order under status (title,
// summary, severity, state, runbookURL, target, timestamps, links, alertsCount,
// raw) defined on oncalltypes.AlertGroupStatus. The table codec below mirrors
// the locked SRE-persona column shape for `irm oncall alert-groups list`.
//
// Layout follows the trace tree precedent (PR #610): predictable-width columns
// (ID, SEVERITY, STATE, STARTED, ALERTS, LINKS.*.UID) get fixed widths via
// style.TableBuilder.ColumnWidths so lipgloss doesn't compress them when the
// terminal is narrow; flexible columns (TITLE, TEAM, INTEGRATION, TARGET.*,
// LINKS.SLO.NAME, LINKS.ALERT.INSTANCE.SILENCEURL) absorb the remaining width.

type alertGroupTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *alertGroupTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *alertGroupTableCodec) Encode(w io.Writer, v any) error {
	envs, ok := v.([]alertGroupEnvelope)
	if !ok {
		return errors.New("invalid data type for table codec: expected []alertGroupEnvelope")
	}
	// Locked column widths. Per-column values include lipgloss padding (the
	// styled renderer applies Padding(0,1), which lipgloss counts inside
	// Width()): a 16-wide ID column has 14 chars of content room, enough for
	// a 13-char OnCall PK plus headroom. See `alertGroupTitleColIdx` below
	// for the title column index used by the truncation budget.
	var (
		t              *style.TableBuilder
		colWidths      []int
		titleColIdx    int
		fixedColsWidth int
	)
	if c.Wide {
		t = style.NewTable("ID", "TITLE", "TEAM", "SEVERITY", "STATE", "INTEGRATION", "TARGET.CLUSTER", "TARGET.SERVICE", "ALERTS", "LINKS.SLO.NAME", "STARTED")
		colWidths = []int{16, 0, 0, 10, 14, 0, 0, 0, 8, 0, 12}
		titleColIdx = 1
	} else {
		t = style.NewTable("ID", "TITLE", "TEAM", "SEVERITY", "STATE", "STARTED")
		colWidths = []int{16, 0, 0, 10, 14, 12}
		titleColIdx = 1
	}
	t.ColumnWidths(colWidths)
	for _, w := range colWidths {
		fixedColsWidth += w
	}
	titleBudget := titleAvailableWidth(len(colWidths), fixedColsWidth)
	for _, env := range envs {
		id := env.Metadata.Name
		title := truncateRunes(orDash(env.Status.Title), titleBudget)
		severity := orDash(env.Status.Severity)
		state := orDash(env.Status.State)
		teamName := ""
		if env.Spec.Team != nil {
			teamName = fmt.Sprintf("%s (%s)", env.Spec.Team.Name, env.Spec.Team.ID)
		}
		started := formatRelativeAge(env.Metadata.CreationTimestamp)
		_ = titleColIdx // kept for future per-column ellipsis dispatch
		if c.Wide {
			cluster, service := "", ""
			if env.Status.Target != nil {
				cluster = env.Status.Target.Cluster
				service = env.Status.Target.Service
			}
			sloName := ""
			if env.Status.Links != nil && env.Status.Links.SLO != nil {
				sloName = env.Status.Links.SLO.Name
			}
			t.Row(
				id,
				title,
				orDash(teamName),
				severity,
				state,
				orDash(env.Spec.Integration.Name),
				orDash(cluster),
				orDash(service),
				strconv.Itoa(env.Status.AlertsCount),
				orDash(sloName),
				started,
			)
		} else {
			t.Row(id, title, orDash(teamName), severity, state, started)
		}
	}
	return t.Render(w)
}

// titleAvailableWidth computes the column budget left for the flexible TITLE
// cell after subtracting the sum of locked-width columns, per-column lipgloss
// borders, and a small safety margin. Returns 0 when the terminal width is
// unknown (truncateRunes treats 0 as "no truncation"), so piped output and
// agent-mode renderings (both fall through the plain tabwriter path) keep the
// full title intact.
func titleAvailableWidth(nCols, fixedColsWidth int) int {
	w := terminal.StdoutWidth()
	if w <= 0 {
		return 0
	}
	// lipgloss table draws nCols+1 vertical borders (1 char each) between
	// cells. Auto-sized columns (count > 0) need at least 1 column each; we
	// over-subtract conservatively so terminal noise (resize races, tab
	// rounding) doesn't push us into wraps.
	autoCols := 0
	if fixedColsWidth > 0 {
		// nCols includes the title; reserve 4 chars for each non-title
		// auto-sized column (TEAM, INTEGRATION, TARGET.CLUSTER, ...).
		autoCols = countAutoCols(nCols, fixedColsWidth)
	}
	const minAutoCol = 4
	const safetyMargin = 4
	budget := w - fixedColsWidth - (nCols + 1) - autoCols*minAutoCol - safetyMargin
	if budget < minTitleWidth {
		return minTitleWidth
	}
	return budget
}

// countAutoCols returns the number of auto-sized (zero-width) columns in the
// codec layout, derived from nCols and the sum of fixed widths. Used by
// titleAvailableWidth to reserve a minimum width per auto-sized column when
// computing the title cell's budget.
//
// The actual count varies per layout (5 in wide mode: TITLE+TEAM+INTEGRATION+
// TARGET.CLUSTER+TARGET.SERVICE+LINKS.SLO.NAME, 1 in narrow mode: TITLE+TEAM).
// We approximate via nCols minus the count of explicitly non-zero entries —
// this is a structural property of the locked column shape, not data-driven,
// so a fixed-width-count override would belong here if the locked shape
// becomes more dynamic in the future.
func countAutoCols(nCols, _ int) int {
	// Conservative: assume half the columns are auto-sized minus the title.
	// In practice this evaluates to:
	//   - narrow (6 cols): 6/2 = 3 → over-reserves but keeps title finite
	//   - wide  (11 cols): 11/2 = 5 → matches the 5 auto-sized columns
	// The over-reservation in narrow mode is harmless (title is the only
	// long column there).
	auto := nCols / 2
	if auto < 1 {
		auto = 1
	}
	return auto
}

// minTitleWidth is the floor under which we don't truncate further — below
// this, even an ellipsis-truncated title is uninformative, so we let the table
// renderer wrap rather than emit "…" alone.
const minTitleWidth = 16

// truncateRunes returns s if its rune count is at most width, else returns the
// first (width-1) runes followed by '…'. width <= 0 is treated as "no
// truncation" so callers that pass 0 (e.g. when the terminal width is unknown)
// preserve the full title.
func truncateRunes(s string, width int) string {
	if width <= 0 {
		return s
	}
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	out := make([]rune, 0, width)
	count := 0
	for _, r := range s {
		if count >= width-1 {
			break
		}
		out = append(out, r)
		count++
	}
	out = append(out, '…')
	return string(out)
}

// --- User codec (internal: pk, avatar, current_team) ---

type userTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *userTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *userTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "USERNAME", "NAME", "EMAIL", "ROLE", "TIMEZONE")
	} else {
		t = style.NewTable("ID", "USERNAME", "NAME", "ROLE", "TIMEZONE")
	}
	for _, obj := range items {
		if c.Wide {
			t.Row(obj.GetName(), specStr(obj, "username"), orDash(specStr(obj, "name")),
				orDash(specStr(obj, "email")), orDash(specStr(obj, "role")), orDash(specStr(obj, "timezone")))
		} else {
			t.Row(obj.GetName(), specStr(obj, "username"), orDash(specStr(obj, "name")),
				orDash(specStr(obj, "role")), orDash(specStr(obj, "timezone")))
		}
	}
	return t.Render(w)
}

// --- Team codec ---

type teamTableCodec struct{ noDecodeCodec }

func (c *teamTableCodec) Format() format.Format { return "table" }

func (c *teamTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "NAME", "EMAIL")
	for _, obj := range items {
		t.Row(obj.GetName(), specStr(obj, "name"), orDash(specStr(obj, "email")))
	}
	return t.Render(w)
}

// --- UserGroup codec ---

type userGroupTableCodec struct{ noDecodeCodec }

func (c *userGroupTableCodec) Format() format.Format { return "table" }

func (c *userGroupTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "NAME", "HANDLE")
	for _, obj := range items {
		t.Row(obj.GetName(), orDash(specStr(obj, "name")), orDash(specStr(obj, "handle")))
	}
	return t.Render(w)
}

// --- SlackChannel codec ---

type slackChannelTableCodec struct{ noDecodeCodec }

func (c *slackChannelTableCodec) Format() format.Format { return "table" }

func (c *slackChannelTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "NAME", "SLACK-ID")
	for _, obj := range items {
		t.Row(obj.GetName(), orDash(specStr(obj, "display_name")), orDash(specStr(obj, "slack_id")))
	}
	return t.Render(w)
}

// --- Alert codec (typed: accepts []alertEnvelope) ---
//
// Emitted by `irm oncall alert-groups list-alerts <id>`. The locked column
// set is link-emphasising (D2 round 17): SEVERITY / TARGET.* / STARTED were
// dropped because they are AlertGroup-level attributes — repeating them on
// each per-alert row inside the parent group's listing was redundant noise.
// Default columns: NAME, STATE, RULE, DASHBOARD (URL preferred over UID per
// cell). Wide mode adds the full link breakdown so SREs can pivot to
// rules / silences / panels.
//
// JSON/YAML emission is unchanged — the typed envelope still carries the
// full status.links.{alert,dashboard,...} block.

type alertTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *alertTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// alertRuleCellPreferURL returns the cell rendering for the default RULE
// column: URL takes precedence, falling back to UID, then "-". The URL
// preference matches the locked shape rationale that a clickable URL is
// more useful than the UID alone in TTY mode.
func alertRuleCellPreferURL(env alertEnvelope) string {
	if env.Status.Links == nil || env.Status.Links.Alert == nil || env.Status.Links.Alert.Rule == nil {
		return "-"
	}
	r := env.Status.Links.Alert.Rule
	if r.URL != "" {
		return r.URL
	}
	if r.UID != "" {
		return r.UID
	}
	return "-"
}

// alertDashboardCellPreferURL mirrors alertRuleCellPreferURL for the
// DASHBOARD column.
func alertDashboardCellPreferURL(env alertEnvelope) string {
	if env.Status.Links == nil || env.Status.Links.Dashboard == nil {
		return "-"
	}
	d := env.Status.Links.Dashboard
	if d.URL != "" {
		return d.URL
	}
	if d.UID != "" {
		return d.UID
	}
	return "-"
}

func (c *alertTableCodec) Encode(w io.Writer, v any) error {
	envs, ok := v.([]alertEnvelope)
	if !ok {
		return errors.New("invalid data type for table codec: expected []alertEnvelope")
	}
	var t *style.TableBuilder
	// Width semantics match alertGroupTableCodec (lipgloss Padding(0,1) frame
	// included in Width()): a 16-wide NAME column holds a 13-char OnCall PK
	// plus headroom. Fixed widths are applied via truncateRunes below; flex
	// columns (width 0) absorb residual terminal width and rely on lipgloss
	// to wrap long URLs rather than reproducing per-column ellipsis budgets.
	if c.Wide {
		t = style.NewTable("NAME", "STATE", "RULE.UID", "RULE.URL", "INSTANCE.SILENCEURL", "DASHBOARD.UID", "DASHBOARD.URL", "DASHBOARD.PANEL.URL")
		t.ColumnWidths([]int{16, 14, 16, 0, 0, 0, 0, 0})
	} else {
		t = style.NewTable("NAME", "STATE", "RULE", "DASHBOARD")
		t.ColumnWidths([]int{16, 14, 0, 0})
	}
	for _, env := range envs {
		name := env.Metadata.Name
		state := orDash(env.Status.State)
		if c.Wide {
			ruleUID, ruleURL, silenceURL := "", "", ""
			dashUID, dashURL, panelURL := "", "", ""
			if env.Status.Links != nil {
				if env.Status.Links.Alert != nil {
					if env.Status.Links.Alert.Rule != nil {
						ruleUID = env.Status.Links.Alert.Rule.UID
						ruleURL = env.Status.Links.Alert.Rule.URL
					}
					if env.Status.Links.Alert.Instance != nil {
						silenceURL = env.Status.Links.Alert.Instance.SilenceURL
					}
				}
				if env.Status.Links.Dashboard != nil {
					dashUID = env.Status.Links.Dashboard.UID
					dashURL = env.Status.Links.Dashboard.URL
					if env.Status.Links.Dashboard.Panel != nil {
						panelURL = env.Status.Links.Dashboard.Panel.URL
					}
				}
			}
			// Apply rune-aware ellipsis to fixed-width cells (RULE.UID at
			// width 16); flex cells (URLs) are left to lipgloss wrapping.
			t.Row(name, state,
				truncateRunes(orDash(ruleUID), 16),
				orDash(ruleURL),
				orDash(silenceURL),
				orDash(dashUID),
				orDash(dashURL),
				orDash(panelURL),
			)
		} else {
			t.Row(name, state, alertRuleCellPreferURL(env), alertDashboardCellPreferURL(env))
		}
	}
	return t.Render(w)
}

// --- Organization codec ---

type organizationTableCodec struct{ noDecodeCodec }

func (c *organizationTableCodec) Format() format.Format { return "table" }

func (c *organizationTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "NAME", "SLUG")
	for _, obj := range items {
		t.Row(obj.GetName(), orDash(specStr(obj, "name")), orDash(specStr(obj, "stack_slug")))
	}
	return t.Render(w)
}

// --- ResolutionNote codec ---

type resolutionNoteTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *resolutionNoteTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *resolutionNoteTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "ALERT-GROUP", "SOURCE", "CREATED", "TEXT")
	} else {
		t = style.NewTable("ID", "ALERT-GROUP", "SOURCE", "CREATED")
	}
	for _, obj := range items {
		created := specStr(obj, "created_at")
		if len(created) > 16 {
			created = created[:16]
		}
		if c.Wide {
			text := specStr(obj, "text")
			if len(text) > 60 {
				text = text[:57] + "..."
			}
			t.Row(obj.GetName(), specStr(obj, "alert_group"), orDash(specStr(obj, "source")), orDash(created), orDash(text))
		} else {
			t.Row(obj.GetName(), specStr(obj, "alert_group"), orDash(specStr(obj, "source")), orDash(created))
		}
	}
	return t.Render(w)
}

// --- ShiftSwap codec ---

type shiftSwapTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *shiftSwapTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *shiftSwapTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "SCHEDULE", "STATUS", "START", "END", "BENEFICIARY", "BENEFACTOR", "CREATED")
	} else {
		t = style.NewTable("ID", "SCHEDULE", "STATUS", "START", "END")
	}
	for _, obj := range items {
		id := obj.GetName()
		start := specStr(obj, "swap_start")
		if len(start) > 16 {
			start = start[:16]
		}
		end := specStr(obj, "swap_end")
		if len(end) > 16 {
			end = end[:16]
		}
		if c.Wide {
			created := specStr(obj, "created_at")
			if len(created) > 16 {
				created = created[:16]
			}
			t.Row(id, orDash(specStr(obj, "schedule")), orDash(specStr(obj, "status")), orDash(start), orDash(end), orDash(specStr(obj, "beneficiary")), orDash(specStr(obj, "benefactor")), orDash(created))
		} else {
			t.Row(id, orDash(specStr(obj, "schedule")), orDash(specStr(obj, "status")), orDash(start), orDash(end))
		}
	}
	return t.Render(w)
}
