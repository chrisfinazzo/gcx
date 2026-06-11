package integrations

import (
	"errors"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

// integrationsTableCodec renders []Integration as a table.
type integrationsTableCodec struct {
	Wide bool
}

func (c *integrationsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *integrationsTableCodec) Encode(w io.Writer, v any) error {
	integrations, ok := v.([]Integration)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Integration")
	}

	var tbl *style.TableBuilder
	if c.Wide {
		tbl = style.NewTable("SLUG", "NAME", "VERSION", "TYPE", "CATEGORIES")
	} else {
		tbl = style.NewTable("SLUG", "NAME", "VERSION")
	}

	for _, in := range integrations {
		if c.Wide {
			tbl.Row(in.Slug, in.Name, in.Version, in.Type, strings.Join(in.Categories, ", "))
		} else {
			tbl.Row(in.Slug, in.Name, in.Version)
		}
	}

	return tbl.Render(w)
}

func (c *integrationsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
