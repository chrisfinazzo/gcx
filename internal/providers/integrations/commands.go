package integrations

import (
	"strings"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listOpts struct {
	IO       cmdio.Options
	Category string
	Platform string
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &integrationsTableCodec{})
	o.IO.RegisterCustomCodec("wide", &integrationsTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Category, "category", "", "Filter by category (case-insensitive substring match)")
	flags.StringVar(&o.Platform, "platform", "", "Filter by supported platform: linux, windows, darwin, or kubernetes")
}

func newListCommand() *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available Grafana Cloud integrations.",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "List the curated catalog of available Grafana Cloud integrations. Use --category to filter by category and --platform (linux/windows/darwin/kubernetes) to filter by supported platform.",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			integrations := curatedCatalog()
			if opts.Category != "" {
				integrations = filterByCategory(integrations, opts.Category)
			}
			if opts.Platform != "" {
				integrations = filterByPlatform(integrations, opts.Platform)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), integrations)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// filterByCategory returns the integrations whose categories contain the given
// substring (case-insensitive).
func filterByCategory(in []Integration, category string) []Integration {
	needle := strings.ToLower(strings.TrimSpace(category))
	out := make([]Integration, 0, len(in))
	for _, i := range in {
		for _, c := range i.Categories {
			if strings.Contains(strings.ToLower(c), needle) {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// filterByPlatform returns the integrations that support the given platform
// (exact, case-insensitive match against the platforms list).
func filterByPlatform(in []Integration, platform string) []Integration {
	want := strings.TrimSpace(platform)
	out := make([]Integration, 0, len(in))
	for _, i := range in {
		for _, p := range i.Platforms {
			if strings.EqualFold(p, want) {
				out = append(out, i)
				break
			}
		}
	}
	return out
}
