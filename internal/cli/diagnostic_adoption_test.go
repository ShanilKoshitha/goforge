// The adopted public CLI must suppress real compiler errors and stop active
// go-run process trees without modifying application files. Controlled process
// artifacts and all build outputs remain outside the generated application.
package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAdoptedDoctorSuppressesActualCompilerFailureWithoutMutation(t *testing.T) {
	directory := adoptionDiagnosticProject(t)
	external := t.TempDir()
	const marker = "privateCompilerMarkerSecret"
	broken := filepath.Join(directory, "cmd", "diagnostics", "private_compile_proof.go")
	if err := os.WriteFile(broken, []byte("package main\nvar _ = "+marker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := diagnosticSnapshot(t, directory)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	environment, err := diagnosticProcessEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	var compilerStdout, compilerStderr bytes.Buffer
	err = (execProcessRunner{}).RunWithEnvironment(ctx, nil, &compilerStdout, &compilerStderr,
		environment, "go", "build", "-mod=readonly", "-o", filepath.Join(external, "must-not-build"), "./cmd/diagnostics")
	if err == nil || !strings.Contains(compilerStderr.String(), marker) {
		t.Fatal("anti-vacuity failure: ordinary Go did not produce the expected private compiler diagnostic")
	}
	var stdout, stderr bytes.Buffer
	err = RunContext(ctx, []string{"doctor"}, nil, &stdout, &stderr)
	if err == nil {
		t.Fatal("actual compiler failure passed the public doctor command")
	}
	combined := stdout.String() + stderr.String() + err.Error()
	if strings.Contains(combined, marker) {
		t.Fatal("public doctor disclosed raw compiler output")
	}
	if !strings.Contains(stdout.String(), "fail\tapplication.diagnostics\t") {
		t.Fatal("compiler failure was not represented as a failed application check")
	}
	for _, id := range []string{"orm", "views", "assets"} {
		if !strings.Contains(stdout.String(), "pass\t"+id+"\t") {
			t.Errorf("independent %s check did not run after the actual compiler failure", id)
		}
	}
	if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
		t.Fatal("compiler failure handling changed application files or directories")
	}
}

type adoptionProofReady struct {
	PID       int `json:"pid"`
	ParentPID int `json:"parent_pid"`
	ChildPID  int `json:"child_pid"`
}

func TestAdoptedDoctorCancelsActiveGoRunAndItsDescendant(t *testing.T) {
	if !processTreeControlSupported {
		t.Skip("process-tree control is unsupported on this platform")
	}
	directory := adoptionDiagnosticProject(t)
	external := t.TempDir()
	if relative, err := filepath.Rel(directory, external); err == nil &&
		(relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		t.Fatal("controlled-process proof artifacts must be outside the application")
	}
	if err := os.WriteFile(filepath.Join(directory, "cmd", "diagnostics", "main.go"), []byte(adoptionDiagnosticTreeProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFORGE_ADOPTION_PROOF_DIR", external)
	before := diagnosticSnapshot(t, directory)
	ctx, cancel := context.WithCancel(context.Background())
	output := &developmentTestBuffer{}
	finished := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		finished <- RunContext(ctx, []string{"doctor"}, nil, output, output)
	}()
	returned := false
	var held []*os.Process
	var verifiedGone []bool
	// Cancellation and fallback termination target only this run's exact process
	// handles, captured as soon as the parent publishes its validated PID chain,
	// then proven live before cancellation. No shell process-name kill, broad PID
	// enumeration, or unrelated cleanup exists.
	defer func() {
		cancel()
		if !returned {
			select {
			case <-finished:
				returned = true
			case <-time.After(25 * time.Second):
				for index := len(held) - 1; index >= 0; index-- {
					_ = held[index].Kill()
				}
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Errorf("controlled doctor did not return after exact-handle fallback cleanup")
				}
			}
		}
		for index := len(held) - 1; index >= 0; index-- {
			if !verifiedGone[index] {
				_ = held[index].Kill()
			}
			_ = held[index].Release()
		}
	}()

	parent := waitAdoptionProofReady(t, filepath.Join(external, "parent.ready"), done)
	if parent.PID <= 0 || parent.ParentPID <= 0 || parent.ChildPID <= 0 ||
		parent.ParentPID == parent.PID || parent.PID == parent.ChildPID || parent.ParentPID == parent.ChildPID ||
		parent.PID == os.Getpid() || parent.ParentPID == os.Getpid() || parent.ChildPID == os.Getpid() {
		t.Fatal("parent readiness did not contain a valid distinct controlled PID chain")
	}
	// Hold every already-known process before waiting for child readiness. If the
	// child never publishes readiness, deferred failure cleanup still owns handles
	// for the wrapper, compiled diagnostic, and started descendant.
	pids := []int{parent.ParentPID, parent.PID, parent.ChildPID}
	var holdError error
	for _, pid := range pids {
		process, err := os.FindProcess(pid)
		if err != nil {
			holdError = errors.Join(holdError, fmt.Errorf("hold controlled process %d while parent is ready: %w", pid, err))
			continue
		}
		held = append(held, process)
		verifiedGone = append(verifiedGone, false)
	}
	if holdError != nil {
		t.Fatal(holdError)
	}
	child := waitAdoptionProofReady(t, filepath.Join(external, "child.ready"), done)
	if child.PID != parent.ChildPID || child.ParentPID != parent.PID {
		t.Fatal("child readiness did not confirm the held parent/descendant PID chain")
	}
	for _, pid := range pids {
		if running, err := adoptionProofPIDRunning(pid); err != nil || !running {
			t.Fatalf("anti-vacuity failure: controlled process %d is not live: %v", pid, err)
		}
	}
	// Both ready records are published only after signal-ignore setup. Heartbeat
	// changes additionally prove both application executables are actively running
	// before cancellation, rather than merely having left stale PID files.
	for _, role := range []string{"parent", "child"} {
		marker := filepath.Join(external, role+".heartbeat")
		first := readAdoptionProofHeartbeat(t, marker)
		deadline := time.Now().Add(5 * time.Second)
		for {
			if next := readAdoptionProofHeartbeat(t, marker); next != first {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("controlled %s did not remain active before cancellation", role)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	started := time.Now()
	cancel()
	select {
	case err := <-finished:
		returned = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("active public doctor returned %v instead of cancellation", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("active diagnostic process tree did not stop within its graceful/forced shutdown bounds")
	}
	if elapsed := time.Since(started); elapsed > 25*time.Second {
		t.Fatalf("active doctor cancellation took %s", elapsed)
	}
	for index, process := range held {
		if runtime.GOOS == "windows" {
			// Windows os.Process handles identify the original process even if its
			// numeric PID is reused. Wait works for a non-child process on Windows.
			waited := make(chan error, 1)
			go func() { _, err := process.Wait(); waited <- err }()
			select {
			case err := <-waited:
				if err != nil {
					t.Errorf("wait for controlled process %d: %v", pids[index], err)
				} else {
					verifiedGone[index] = true
				}
			case <-time.After(5 * time.Second):
				_ = process.Kill()
				t.Errorf("controlled process %d remained live after cancellation", pids[index])
			}
			continue
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			running, err := adoptionProofPIDRunning(pids[index])
			if err != nil {
				t.Fatalf("inspect controlled process %d: %v", pids[index], err)
			}
			if !running {
				verifiedGone[index] = true
				break
			}
			if time.Now().After(deadline) {
				_ = process.Kill()
				t.Errorf("controlled process %d remained running after cancellation", pids[index])
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
		t.Fatal("active diagnostic cancellation changed application files or directories")
	}
	if strings.Contains(output.String(), "private-cancellation-proof-secret") {
		t.Fatal("cancellation path disclosed raw controlled-process output")
	}
}

func waitAdoptionProofReady(t *testing.T, path string, done <-chan struct{}) adoptionProofReady {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		contents, err := os.ReadFile(path)
		if err == nil {
			var ready adoptionProofReady
			if err := json.Unmarshal(contents, &ready); err != nil {
				t.Fatalf("invalid atomically-published controlled readiness record: %v", err)
			}
			return ready
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case <-done:
			// The caller's deferred cancel still safely cleans its owned tree;
			// there is no reason to print raw subprocess data on startup failure.
			t.Fatal("controlled public doctor exited before becoming active")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("controlled diagnostic did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readAdoptionProofHeartbeat(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

// Numeric PID inspection is read-only. On Unix a zombie has exited and cannot
// perform application work; the operating system's reaper owns its final reap.
// Windows exit proof above uses held handles, not merely tasklist disappearance.
func adoptionProofPIDRunning(pid int) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if runtime.GOOS == "windows" {
		output, err := exec.CommandContext(ctx, "tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
		if err != nil {
			return false, err
		}
		records, err := csv.NewReader(bytes.NewReader(output)).ReadAll()
		if err != nil {
			return false, err
		}
		for _, record := range records {
			if len(record) < 2 {
				continue
			}
			found, err := strconv.Atoi(record[1])
			if err == nil && found == pid {
				return true, nil
			}
		}
		return false, nil
	}
	output, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	state := strings.TrimSpace(string(output))
	return state != "" && !strings.HasPrefix(state, "Z"), nil
}

const adoptionDiagnosticTreeProgram = `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

type ready struct {
	PID       int
	ParentPID int
	ChildPID  int
}

func main() {
	directory := os.Getenv("GOFORGE_ADOPTION_PROOF_DIR")
	if directory == "" {
		os.Exit(20)
	}
	signal.Ignore(os.Interrupt, syscall.SIGTERM)
	role := "parent"
	record := ready{PID: os.Getpid(), ParentPID: os.Getppid()}
	if len(os.Args) > 1 && os.Args[1] == "--proof-child" {
		role = "child"
	} else {
		child := exec.Command(os.Args[0], "--proof-child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(21)
		}
		record.ChildPID = child.Process.Pid
		go func() { _ = child.Wait() }()
	}
	// Seed the heartbeat before readiness so observing ready proves it exists.
	heartbeat := filepath.Join(directory, role+".heartbeat")
	if err := os.WriteFile(heartbeat, []byte("0"), 0o600); err != nil {
		os.Exit(22)
	}
	encoded, err := json.Marshal(map[string]int{"pid": record.PID, "parent_pid": record.ParentPID, "child_pid": record.ChildPID})
	if err != nil {
		os.Exit(23)
	}
	temporary := filepath.Join(directory, role+".ready.tmp")
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		os.Exit(24)
	}
	if err := os.Rename(temporary, filepath.Join(directory, role+".ready")); err != nil {
		os.Exit(25)
	}
	fmt.Fprintln(os.Stderr, "private-cancellation-proof-secret")
	for counter := 1; ; counter++ {
		time.Sleep(20 * time.Millisecond)
		if err := os.WriteFile(heartbeat, []byte(fmt.Sprint(counter)), 0o600); err != nil {
			os.Exit(26)
		}
	}
}
`
