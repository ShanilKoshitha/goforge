package views

import (
	"reflect"
	"testing"

	"github.com/ShanilKoshitha/goforge/view"

	"example.com/format6/resources/views/viewfuncs"
)

func TestCompiledViewsAreCurrent(t *testing.T) {
	compiled, err := view.CompileForge(Files)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(compiled, Compiled) {
		t.Fatal("compiled views are stale; run forge views:compile")
	}
	if _, err := view.ParseCompiled(compiled, viewfuncs.Functions()); err != nil {
		t.Fatalf("compiled views do not match application functions: %v", err)
	}
}
