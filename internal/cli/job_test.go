package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMakeJobCreatesOwnedHandlerMetadataAndDeterministicRegistry(t *testing.T) {
	directory := jobProject(t, "6")
	t.Chdir(directory)
	var output bytes.Buffer
	if err := makeJob("SendEmailJob", &output); err != nil {
		t.Fatal(err)
	}
	if err := makeJob("ArchiveAccount", &output); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"internal/jobs/send_email.go", "internal/jobs/send_email_test.go",
		"internal/jobs/archive_account.go", "internal/jobs/archive_account_test.go",
		generatedJobRegistryPath, jobStatePath,
	} {
		if _, err := os.Stat(filepath.FromSlash(path)); err != nil {
			t.Errorf("expected %s: %v", path, err)
		}
	}
	handler, err := os.ReadFile(filepath.FromSlash("internal/jobs/send_email.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`type SendEmail struct{}`,
		`job.MustDefine[SendEmail]("send_email.v1", job.Policy{})`,
		`type SendEmailHandler struct`,
		`func DispatchSendEmail(`,
		`delivered at least once`,
	} {
		if !strings.Contains(string(handler), want) {
			t.Errorf("handler is missing %q:\n%s", want, handler)
		}
	}
	registry, err := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(registry), "ArchiveAccountDefinition") > strings.Index(string(registry), "SendEmailDefinition") {
		t.Fatalf("registry is not sorted:\n%s", registry)
	}
	state, err := loadJobState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Jobs) != 2 || state.Jobs[0].Name != "ArchiveAccount" || state.Jobs[1].Name != "SendEmail" {
		t.Fatalf("job metadata = %#v", state.Jobs)
	}
}

func TestConcurrentMakeJobProcessesPreserveEveryRegistryEntry(t *testing.T) {
	directory := jobProject(t, "6")
	type runningGenerator struct {
		name    string
		command *exec.Cmd
		output  *bytes.Buffer
	}
	generators := make([]runningGenerator, 12)
	for index := range generators {
		name := "ConcurrentJob" + strconv.Itoa(index+1)
		output := &bytes.Buffer{}
		command := exec.Command(os.Args[0], "-test.run=^TestMakeJobHelperProcess$")
		command.Env = append(os.Environ(),
			"GOFORGE_MAKE_JOB_HELPER=1",
			"GOFORGE_MAKE_JOB_DIRECTORY="+directory,
			"GOFORGE_MAKE_JOB_NAME="+name,
		)
		command.Stdout = output
		command.Stderr = output
		if err := command.Start(); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
		generators[index] = runningGenerator{name: name, command: command, output: output}
	}
	for _, generator := range generators {
		if err := generator.command.Wait(); err != nil {
			t.Errorf("%s failed: %v\n%s", generator.name, err, generator.output.String())
		}
	}
	if t.Failed() {
		return
	}

	metadata, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(jobStatePath)))
	if err != nil {
		t.Fatal(err)
	}
	var state jobState
	if err := json.Unmarshal(metadata, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Jobs) != len(generators) {
		t.Fatalf("job metadata retained %d of %d concurrent jobs: %#v", len(state.Jobs), len(generators), state.Jobs)
	}
	registry, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(generatedJobRegistryPath)))
	if err != nil {
		t.Fatal(err)
	}
	for _, generator := range generators {
		if !bytes.Contains(registry, []byte(generator.name+"Definition")) {
			t.Errorf("registry omitted %s", generator.name)
		}
		fileName, _ := snake(generator.name)
		if _, err := os.Stat(filepath.Join(directory, "internal", "jobs", fileName+".go")); err != nil {
			t.Errorf("%s source missing: %v", generator.name, err)
		}
	}
}

func TestMakeJobHelperProcess(t *testing.T) {
	if os.Getenv("GOFORGE_MAKE_JOB_HELPER") != "1" {
		return
	}
	if err := os.Chdir(os.Getenv("GOFORGE_MAKE_JOB_DIRECTORY")); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"make:job", os.Getenv("GOFORGE_MAKE_JOB_NAME")}, &output, &output); err != nil {
		t.Fatalf("make job: %v\n%s", err, output.String())
	}
}

func TestMakeJobRefusesInvalidDuplicateAndCaseCollisionBeforeWrites(t *testing.T) {
	directory := jobProject(t, "6")
	t.Chdir(directory)
	for _, name := range []string{"---", "123", "Dependencies", "Registry", "NewRegistry", "NewDispatcher"} {
		if err := makeJob(name, &bytes.Buffer{}); err == nil {
			t.Fatalf("invalid job %q was accepted", name)
		}
	}
	if err := makeJob("SendEmail", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	registryBefore, _ := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
	stateBefore, _ := os.ReadFile(filepath.FromSlash(jobStatePath))
	for _, name := range []string{"send_email", "SEND EMAIL", "SendEmailJob"} {
		if err := makeJob(name, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Errorf("duplicate job %q error = %v", name, err)
		}
	}
	registryAfter, _ := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
	stateAfter, _ := os.ReadFile(filepath.FromSlash(jobStatePath))
	if !bytes.Equal(registryBefore, registryAfter) || !bytes.Equal(stateBefore, stateAfter) {
		t.Fatal("duplicate job generation changed managed state")
	}
}

func TestMakeJobRejectsEveryGeneratedDeclarationCollisionBeforeWrites(t *testing.T) {
	directory := jobProject(t, "6")
	t.Chdir(directory)
	if err := makeJob("SendEmail", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	registryBefore, _ := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
	stateBefore, _ := os.ReadFile(filepath.FromSlash(jobStatePath))
	for _, name := range []string{"SendEmailDefinition", "SendEmailHandler", "DispatchSendEmail", "TestSendEmailHandler"} {
		err := makeJob(name, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "conflicts") {
			t.Errorf("declaration collision %q error = %v", name, err)
		}
	}
	registryAfter, _ := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
	stateAfter, _ := os.ReadFile(filepath.FromSlash(jobStatePath))
	if !bytes.Equal(registryBefore, registryAfter) || !bytes.Equal(stateBefore, stateAfter) {
		t.Fatal("declaration collision changed managed state")
	}
}

func TestMakeJobRejectsFormatNineBuiltinDeclarationsBeforeWrites(t *testing.T) {
	for _, name := range []string{
		"Outbox", "DeliverMail", "DeliverMailDefinition", "DeliverMailHandler", "DispatchDeliverMail",
		"CleanupMail", "CleanupMailDefinition", "CleanupMailHandler", "DispatchCleanupMail", "MailOutboxRetention",
	} {
		t.Run(name, func(t *testing.T) {
			directory := jobProject(t, "9")
			t.Chdir(directory)
			registryBefore, _ := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
			stateBefore, _ := os.ReadFile(filepath.FromSlash(jobStatePath))
			err := makeJob(name, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "built-in declaration") {
				t.Fatalf("built-in collision %q error = %v", name, err)
			}
			registryAfter, _ := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
			stateAfter, _ := os.ReadFile(filepath.FromSlash(jobStatePath))
			if !bytes.Equal(registryBefore, registryAfter) || !bytes.Equal(stateBefore, stateAfter) {
				t.Fatal("built-in collision changed managed state")
			}
			fileName, _ := snake(name)
			if _, statErr := os.Stat(filepath.Join("internal", "jobs", fileName+".go")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("built-in collision wrote handler: %v", statErr)
			}
		})
	}
}

func TestMakeJobRejectsCorruptMetadataWithoutWrites(t *testing.T) {
	directory := jobProject(t, "6")
	t.Chdir(directory)
	if err := os.WriteFile(filepath.FromSlash(jobStatePath), []byte(`{"jobs":[{"name":"One","file":"one","definition":"one.v1"},{"name":"one","file":"two","definition":"two.v1"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registryBefore, _ := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
	err := makeJob("SendEmail", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("corrupt metadata error = %v", err)
	}
	registryAfter, _ := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
	if !bytes.Equal(registryBefore, registryAfter) {
		t.Fatal("corrupt metadata changed registry")
	}
	if _, err := os.Stat(filepath.Join("internal", "jobs", "send_email.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt metadata wrote handler: %v", err)
	}
}

func TestMakeJobRollsBackEveryWrite(t *testing.T) {
	for _, failure := range []struct {
		kind string
		at   int
	}{{"exclusive", 1}, {"exclusive", 2}, {"managed", 1}, {"managed", 2}} {
		t.Run(failure.kind+"_"+strconv.Itoa(failure.at), func(t *testing.T) {
			directory := jobProject(t, "6")
			t.Chdir(directory)
			registryBefore, _ := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
			stateBefore, _ := os.ReadFile(filepath.FromSlash(jobStatePath))
			sentinel := errors.New("injected job generation failure")
			exclusiveCalls, managedCalls := 0, 0
			exclusive := func(path, content string) error {
				exclusiveCalls++
				if failure.kind == "exclusive" && exclusiveCalls == failure.at {
					return sentinel
				}
				return writeExclusive(path, content)
			}
			managed := func(path string, content []byte) error {
				managedCalls++
				if failure.kind == "managed" && managedCalls == failure.at {
					if err := writeManagedFile(path, []byte("incomplete\n")); err != nil {
						return err
					}
					return sentinel
				}
				return writeManagedFile(path, content)
			}
			err := makeJobWithWriters("SendEmail", &bytes.Buffer{}, exclusive, managed)
			if !errors.Is(err, sentinel) {
				t.Fatalf("rollback error = %v", err)
			}
			registryAfter, _ := os.ReadFile(filepath.FromSlash(generatedJobRegistryPath))
			stateAfter, _ := os.ReadFile(filepath.FromSlash(jobStatePath))
			if !bytes.Equal(registryBefore, registryAfter) || !bytes.Equal(stateBefore, stateAfter) {
				t.Fatal("failed generation did not restore managed state")
			}
			for _, path := range []string{"internal/jobs/send_email.go", "internal/jobs/send_email_test.go"} {
				if _, err := os.Stat(filepath.FromSlash(path)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("rollback left %s: %v", path, err)
				}
			}
		})
	}
}

func TestMakeJobRollsBackInitiallyMissingManagedArtifactsAndDirectories(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.WriteFile("forge.yaml", []byte("version: 6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("go.mod", []byte("module example.com/jobs\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("stop after registry")
	calls := 0
	managed := func(path string, content []byte) error {
		calls++
		if calls == 2 {
			if err := writeManagedFile(path, []byte("incomplete\n")); err != nil {
				return err
			}
			return sentinel
		}
		return writeManagedFile(path, content)
	}
	err := makeJobWithWriters("SendEmail", &bytes.Buffer{}, writeExclusive, managed)
	if !errors.Is(err, sentinel) {
		t.Fatalf("rollback error = %v", err)
	}
	for _, path := range []string{generatedJobRegistryPath, jobStatePath, "internal/jobs/send_email.go", "internal/jobs/send_email_test.go", "internal", ".forge"} {
		if _, err := os.Stat(filepath.FromSlash(path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rollback left initially missing %s: %v", path, err)
		}
	}
}

func TestMakeJobRequiresSupportedProjectFormat(t *testing.T) {
	for _, version := range []string{"5", "11"} {
		t.Run(version, func(t *testing.T) {
			directory := jobProject(t, version)
			t.Chdir(directory)
			err := makeJob("SendEmail", &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "format") {
				t.Fatalf("format %s error = %v", version, err)
			}
			if _, err := os.Stat(filepath.Join("internal", "jobs", "send_email.go")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("format refusal wrote handler: %v", err)
			}
		})
	}
}

func jobProject(t *testing.T, version string) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: "+version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com/jobs\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".forge", "jobs.json"), []byte("{\"jobs\":[]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := generatedJobRegistry("example.com/jobs", jobState{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, filepath.FromSlash(generatedJobRegistryPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}
