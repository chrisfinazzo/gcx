package integrations

import (
	_ "embed"
	"sort"
	"strings"
)

// Integration is one entry in the curated Grafana Cloud integrations catalog.
type Integration struct {
	Name       string   `json:"name"`
	Slug       string   `json:"slug"`
	Version    string   `json:"version"`
	Type       string   `json:"type"`
	Categories []string `json:"categories,omitempty"`
}

// curatedData is the embedded curated list of available Grafana Cloud
// integrations. Edit curated-integrations.txt to change the catalog.
//
//go:embed curated-integrations.txt
var curatedData string

// curatedCatalog parses the embedded curated list into integrations sorted by
// slug. Each entry is a "slug | name | version | categories" line; blank lines
// and lines starting with '#' are ignored.
func curatedCatalog() []Integration {
	var out []Integration
	for raw := range strings.SplitSeq(curatedData, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, "|")
		if len(fields) < 2 {
			continue
		}
		in := Integration{
			Slug: strings.TrimSpace(fields[0]),
			Name: strings.TrimSpace(fields[1]),
			Type: "agent",
		}
		if in.Slug == "" || in.Name == "" {
			continue
		}
		if len(fields) >= 3 {
			in.Version = strings.TrimSpace(fields[2])
		}
		if len(fields) >= 4 {
			for c := range strings.SplitSeq(fields[3], ",") {
				if c = strings.TrimSpace(c); c != "" {
					in.Categories = append(in.Categories, c)
				}
			}
		}
		out = append(out, in)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}
