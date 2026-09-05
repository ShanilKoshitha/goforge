package cli

import (
	"os"
	"testing"
)

func TestProjectModuleAcceptsConventionalGoDirectives(t *testing.T) {
	for _, declaration := range []string{
		"module example.com/app\n",
		"module\texample.com/app // local application\n",
		"module \"example.com/app\"\n",
	} {
		t.Run(declaration, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile("go.mod", []byte(declaration), 0o644); err != nil {
				t.Fatal(err)
			}
			if module, err := projectModule(); err != nil || module != "example.com/app" {
				t.Fatalf("module = %q, %v", module, err)
			}
		})
	}
}
