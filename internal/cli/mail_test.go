package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMakeMailCreatesInspectableDualTemplatesAndCompilesViews(t *testing.T) {
	directory := mailProject(t, 9)
	t.Chdir(directory)
	process := &recordedProcess{}
	var output bytes.Buffer
	if err := makeMail(context.Background(), "AuditNotice", nil, &output, io.Discard, process); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]string{
		"internal/mail/audit_notice.go":                 "type AuditNotice struct",
		"internal/mail/audit_notice_test.go":            "TestAuditNoticeBuildsTextAndHTML",
		"internal/mail/templates/audit_notice.txt.tmpl": "{{.Message}}",
		"resources/views/mail/audit_notice.forge.html":  "{{.Message}}",
	} {
		contents, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(contents), expected) {
			t.Errorf("%s omits %q:\n%s", path, expected, contents)
		}
		if !strings.Contains(output.String(), path) {
			t.Errorf("output omits %s: %q", path, output.String())
		}
	}
	if process.name != "go" || strings.Join(process.args, " ") != "run ./cmd/views" {
		t.Fatalf("mail compiler command = %s %v", process.name, process.args)
	}
}

func TestMakeMailPreflightsAndRollsBackCompilerFailure(t *testing.T) {
	directory := mailProject(t, 9)
	t.Chdir(directory)
	existing := filepath.FromSlash("resources/views/mail/audit_notice.forge.html")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := makeMail(context.Background(), "AuditNotice", nil, io.Discard, io.Discard, &recordedProcess{})
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("collision error = %v", err)
	}
	if _, err := os.Stat(filepath.FromSlash("internal/mail/audit_notice.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight wrote source: %v", err)
	}

	if err := os.Remove(existing); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Dir(existing)); err != nil {
		t.Fatal(err)
	}
	want := errors.New("compile failed")
	err = makeMail(context.Background(), "AuditNotice", nil, io.Discard, io.Discard, &recordedProcess{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("compiler error = %v", err)
	}
	for _, path := range []string{
		"internal/mail/audit_notice.go", "internal/mail/audit_notice_test.go",
		"internal/mail/templates/audit_notice.txt.tmpl", "resources/views/mail/audit_notice.forge.html",
	} {
		if _, err := os.Stat(filepath.FromSlash(path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed generation retained %s: %v", path, err)
		}
	}
	for _, path := range []string{"internal/mail/templates", "resources/views/mail"} {
		if _, err := os.Stat(filepath.FromSlash(path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed generation retained directory %s: %v", path, err)
		}
	}
}

func TestMakeMailRequiresFormatNine(t *testing.T) {
	directory := mailProject(t, 8)
	t.Chdir(directory)
	err := makeMail(context.Background(), "AuditNotice", nil, io.Discard, io.Discard, &recordedProcess{})
	if err == nil || !strings.Contains(err.Error(), "upgrade the project to format 9") {
		t.Fatalf("format error = %v", err)
	}
}

func TestMakeMailRejectsPackageDeclarationCollisionBeforeWrites(t *testing.T) {
	directory := mailProject(t, 9)
	t.Chdir(directory)
	if err := os.MkdirAll(filepath.FromSlash("internal/mail"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.FromSlash("internal/mail/builtin.go"), []byte("package mail\n\nfunc NewAuditNotice() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := makeMail(context.Background(), "AuditNotice", nil, io.Discard, io.Discard, &recordedProcess{})
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("declaration collision error = %v", err)
	}
	for _, path := range []string{
		"internal/mail/audit_notice.go", "internal/mail/audit_notice_test.go",
		"internal/mail/templates/audit_notice.txt.tmpl", "resources/views/mail/audit_notice.forge.html",
	} {
		if _, statErr := os.Stat(filepath.FromSlash(path)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("declaration collision wrote %s: %v", path, statErr)
		}
	}
}

func mailProject(t *testing.T, version int) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: "+strconv.Itoa(version)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com/application\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}
