package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestResourceHelpDocumentsExplicitChoiceLabelCapability(t *testing.T) {
	var output bytes.Buffer
	if err := Run([]string{"help"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	for _, syntax := range []string{"forge make resource <name>", "forge make:resource <name>"} {
		found := false
		for _, line := range strings.Split(output.String(), "\n") {
			if strings.Contains(line, syntax) && strings.Contains(line, "[--choice-label <relationship>:<field>]...") {
				found = true
			}
		}
		if !found {
			t.Errorf("help omits choice-label option for %q", syntax)
		}
	}
	for _, fragment := range []string{"exposes the target text field", "omitted labels remain ID-only", "Format-16"} {
		if !strings.Contains(output.String(), fragment) {
			t.Errorf("help omits capability boundary %q", fragment)
		}
	}
}
