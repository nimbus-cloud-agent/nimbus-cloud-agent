package executor

import "encoding/json"

// mustMarshal encodes a value whose encoding cannot fail in practice: the
// callers pass only structs of strings, integers, booleans, slices, and
// DynamicJSON (whose MarshalJSON never returns an error). A failure here is
// therefore a broken invariant, not a runtime error the caller could handle,
// so it panics instead of adding an error branch no test can reach. The panic
// path is covered by TestMustMarshalPanics, using a deliberately failing
// marshaler passed as any.
func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
