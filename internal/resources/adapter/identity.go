package adapter

import "strconv"

// ResourceIdentity provides self-describing identity for provider domain types.
// Every domain type used in a ResourceAdapter must implement this interface so
// that TypedCRUD can extract and restore resource names without function pointers.
//
// Pointer types (*Slo, *Probe, etc.) satisfy this full interface because
// SetResourceName requires a pointer receiver to mutate the identity field.
// Use compile-time assertions (var _ ResourceIdentity = &MyType{}) to verify.
type ResourceIdentity interface {
	// GetResourceName returns the resource's identity as a string.
	// For types with string identifiers, return the field directly.
	// For types with numeric identifiers, convert via strconv.
	GetResourceName() string

	// SetResourceName restores the identity from a string (e.g., after K8s
	// round-trip via metadata.name). For numeric types, parse errors are
	// silently ignored.
	SetResourceName(name string)
}

// ResourceNamer is the value-type-compatible subset of ResourceIdentity used
// as the TypedCRUD type constraint. Go generics cannot enforce pointer-receiver
// methods (SetResourceName) on value types, so TypedCRUD constrains on
// GetResourceName() only. SetResourceName is accessed via type assertion on *T.
//
// All domain types that implement ResourceIdentity also satisfy ResourceNamer.
type ResourceNamer interface {
	GetResourceName() string
}

// Named is an embeddable identity for domain types identified by a plain
// string name. Embedding it gives a struct GetResourceName/SetResourceName
// for free (satisfying ResourceIdentity), removing the repeated pair every
// name-identified provider type otherwise hand-writes. The field serializes
// as "name" in the domain type's spec.
//
//nolint:recvcheck // Mixed receivers are intentional: GetResourceName is a value method (satisfies ResourceNamer on value types too); SetResourceName needs a pointer receiver to mutate the field.
type Named struct {
	Name string `json:"name"`
}

// GetResourceName returns Name.
func (n Named) GetResourceName() string { return n.Name }

// SetResourceName restores Name (e.g. after a K8s round-trip via metadata.name).
func (n *Named) SetResourceName(name string) { n.Name = name }

// Compile-time check that Named satisfies ResourceIdentity (pointer receiver).
var _ ResourceIdentity = &Named{}

// IDNamed is an embeddable identity for domain types identified by a numeric
// ID, composing a "slug-id" resource name (e.g. "web-check-8127") from a
// human-readable Name and the ID via slug.go's SlugifyName/ComposeName.
// Embed it for providers whose API identifies resources by numeric ID
// (Synthetic Monitoring, Fleet) but that want human-readable K8s names.
//
//nolint:recvcheck // Mixed receivers are intentional: GetResourceName is a value method (satisfies ResourceNamer on value types too); SetResourceName needs a pointer receiver to mutate the field.
type IDNamed struct {
	ID   int64  `json:"id"`
	Name string `json:"name,omitempty"`
}

// GetResourceName composes the "slug-id" name from Name and ID.
func (n IDNamed) GetResourceName() string {
	return ComposeName(SlugifyName(n.Name), strconv.FormatInt(n.ID, 10))
}

// SetResourceName restores ID from a "slug-id" (or bare numeric) name.
// Parse failures are silently ignored, matching ResourceIdentity's contract
// for numeric identifiers.
func (n *IDNamed) SetResourceName(name string) {
	if id, ok := ExtractInt64IDFromSlug(name); ok {
		n.ID = id
	}
}

// Compile-time check that IDNamed satisfies ResourceIdentity (pointer receiver).
var _ ResourceIdentity = &IDNamed{}
