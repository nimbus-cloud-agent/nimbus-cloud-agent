package executor

import (
	"errors"
	"strings"
	"testing"
)

type failingMarshaler struct{}

func (failingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("boom")
}

func TestMustMarshalEncodes(t *testing.T) {
	if got := string(mustMarshal(struct {
		Name string `json:"name"`
	}{Name: "x"})); got != `{"name":"x"}` {
		t.Fatalf("mustMarshal encoded %s", got)
	}
	if got := string(mustMarshal(nil)); got != "null" {
		t.Fatalf("mustMarshal(nil) = %s", got)
	}
}

func TestMustMarshalPanics(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("mustMarshal did not panic on a failing marshaler")
		}
		if err, ok := recovered.(error); !ok || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("mustMarshal panicked with %v", recovered)
		}
	}()
	mustMarshal(failingMarshaler{})
}
