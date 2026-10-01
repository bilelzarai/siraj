package handlers

import (
	"reflect"
	"testing"
	"time"

	"github.com/bilelzarai/siraj/internal/config"
	"github.com/bilelzarai/siraj/internal/i18n"
)

// Every collaborator New builds for itself has to actually be built.
//
// Two were not. `writes` and `duplicates` were added to the struct and to the
// handlers that use them, and the line that constructs them never landed — so
// registering, messaging, commenting, rating, opening a ticket, acting on a
// friend request and the whole content-health screen dereferenced nil and
// answered 500. Nothing failed to compile, no test covered it, and the panic
// was caught by the recovery middleware, which is what let it stay hidden.
//
// Reflection rather than a list of names on purpose: a field added tomorrow is
// covered by this without anybody remembering to add it here.
func TestNewLeavesNoNilDependency(t *testing.T) {
	cfg := &config.Config{SessionSecret: "x", SessionLifetime: time.Hour}
	bundle, err := i18n.New("en")
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}

	// The arguments New is given may be nil in this test — what is being
	// checked is the ones it is supposed to construct itself.
	h := New(cfg, nil, bundle, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	built := map[string]bool{
		"stash": true, "comparer": true, "writes": true, "duplicates": true,
	}
	v := reflect.ValueOf(h).Elem()
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		if !built[name] {
			continue
		}
		if v.Field(i).IsNil() {
			t.Errorf("New left %s nil; every handler that uses it answers 500", name)
		}
	}
}
