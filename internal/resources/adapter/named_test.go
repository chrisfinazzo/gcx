package adapter_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
)

func TestNamed_ResourceIdentity(t *testing.T) {
	n := &adapter.Named{Name: "my-foo"}

	assert.Equal(t, "my-foo", n.GetResourceName())

	n.SetResourceName("renamed-foo")
	assert.Equal(t, "renamed-foo", n.GetResourceName())
}

func TestIDNamed_ResourceIdentity(t *testing.T) {
	tests := []struct {
		name      string
		idNamed   adapter.IDNamed
		wantName  string
		setName   string
		wantSetID int64
		wantNoop  bool
	}{
		{
			name:      "composes slug-id from Name and ID",
			idNamed:   adapter.IDNamed{ID: 8127, Name: "web check"},
			wantName:  "web-check-8127",
			setName:   "web-check-9001",
			wantSetID: 9001,
		},
		{
			name:      "empty name falls back to slug.go's default slug",
			idNamed:   adapter.IDNamed{ID: 1, Name: ""},
			wantName:  "resource-1",
			setName:   "resource-42",
			wantSetID: 42,
		},
		{
			name:     "unparseable name is silently ignored",
			idNamed:  adapter.IDNamed{ID: 5, Name: "probe"},
			wantName: "probe-5",
			setName:  "not-a-number-suffix-abc",
			wantNoop: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := tc.idNamed
			assert.Equal(t, tc.wantName, n.GetResourceName())

			originalID := n.ID
			n.SetResourceName(tc.setName)
			if tc.wantNoop {
				assert.Equal(t, originalID, n.ID, "unparseable name must leave ID unchanged")
			} else {
				assert.Equal(t, tc.wantSetID, n.ID)
			}
		})
	}
}
