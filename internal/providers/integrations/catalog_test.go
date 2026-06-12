package integrations

import (
	"slices"
	"strings"
	"testing"
)

func TestCuratedCatalog(t *testing.T) {
	got := curatedCatalog()

	bySlug := make(map[string]Integration, len(got))
	for i, in := range got {
		if strings.HasPrefix(in.Slug, "#") || strings.Contains(in.Slug, "|") {
			t.Errorf("non-data line leaked as slug: %q", in.Slug)
		}
		if in.Name == "" || in.Version == "" {
			t.Errorf("%q missing name/version: %+v", in.Slug, in)
		}
		if i > 0 && got[i-1].Slug > in.Slug {
			t.Errorf("catalog not sorted: %q before %q", got[i-1].Slug, in.Slug)
		}
		bySlug[in.Slug] = in
	}

	linux, ok := bySlug["linux-node"]
	if !ok {
		t.Fatal("expected linux-node in catalog")
	}
	if linux.Name != "Linux Server" || linux.Version != "1.6.2" {
		t.Errorf("linux-node parsed wrong: %+v", linux)
	}
	if len(linux.Categories) == 0 {
		t.Error("linux-node should have categories")
	}
	if !slices.Contains(linux.Platforms, "kubernetes") || !slices.Contains(linux.Platforms, "linux") {
		t.Errorf("linux-node platforms wrong: %v", linux.Platforms)
	}

	// An entry with no categories must still parse (e.g. discourse).
	if d, ok := bySlug["discourse"]; !ok || d.Name != "Discourse" {
		t.Errorf("discourse parsed wrong: %+v (present=%v)", d, ok)
	}

	// REMOVED entries must not leak into the catalog.
	for _, removed := range []string{"aws", "cloudwatch", "anthropic", "beyla", "ubiquiti-edgerouter"} {
		if _, bad := bySlug[removed]; bad {
			t.Errorf("removed integration %q leaked into catalog", removed)
		}
	}
}

func TestFilterByPlatform(t *testing.T) {
	got := filterByPlatform(curatedCatalog(), "kubernetes")
	if len(got) == 0 {
		t.Fatal("expected kubernetes integrations")
	}
	for _, in := range got {
		if !slices.Contains(in.Platforms, "kubernetes") {
			t.Errorf("%q has no kubernetes platform: %v", in.Slug, in.Platforms)
		}
	}
	// Case-insensitive.
	if len(filterByPlatform(curatedCatalog(), "Kubernetes")) != len(got) {
		t.Error("platform match should be case-insensitive")
	}
	// Integrations with no platforms never match.
	if out := filterByPlatform([]Integration{{Slug: "x"}}, "linux"); len(out) != 0 {
		t.Errorf("integration without platforms should not match, got %d", len(out))
	}
}

func TestFilterByCategory(t *testing.T) {
	got := filterByCategory(curatedCatalog(), "database")
	if len(got) == 0 {
		t.Fatal("expected database integrations")
	}
	for _, in := range got {
		hit := false
		for _, c := range in.Categories {
			if strings.Contains(strings.ToLower(c), "database") {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("%q has no database category: %v", in.Slug, in.Categories)
		}
	}
}
