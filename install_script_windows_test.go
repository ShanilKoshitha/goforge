//go:build windows

package forge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPowerShellInstallerReportsProgressAndVerifiesForge(t *testing.T) {
	powerShell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("Windows PowerShell is not available")
	}

	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	fakeBin := filepath.Join(temporary, "fake go")
	firstGoPath := filepath.Join(temporary, "first go path")
	installBin := filepath.Join(firstGoPath, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	helperSource := filepath.Join(temporary, "fake_go.go")
	if err := os.WriteFile(helperSource, []byte(fakeGoProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeGo := filepath.Join(fakeBin, "go.exe")
	build := exec.Command("go", "build", "-o", fakeGo, helperSource)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake go: %v\n%s", err, output)
	}

	logPath := filepath.Join(temporary, "go.log")
	script := filepath.Join(root, "scripts", "install.ps1")
	wrapper := filepath.Join(temporary, "run_installer.ps1")
	wrapperSource := `& '` + powerShellLiteral(script) + `' -Version v0.21.0 -SessionOnly
& '` + powerShellLiteral(script) + `' -Version v0.21.0 -SessionOnly
$resolved = (Get-Command forge -ErrorAction Stop).Source
$matches = @(($env:Path -split [IO.Path]::PathSeparator) | Where-Object { $_.Trim().TrimEnd('\', '/') -ieq '` + powerShellLiteral(installBin) + `' })
Write-Output "COMMAND_PATH=$resolved"
Write-Output "PATH_ENTRY_COUNT=$($matches.Count)"
$env:FAKE_FORGE_VERSION = 'forge 9.9.9'
try {
    & '` + powerShellLiteral(script) + `' -Version v0.21.0 -SkipPathUpdate
    Write-Output 'MISMATCH_ACCEPTED'
} catch {
    Write-Output "MISMATCH_REJECTED=$($_.Exception.Message)"
}
`
	if err := os.WriteFile(wrapper, []byte(wrapperSource), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(powerShell,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", wrapper,
	)
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GOBIN=",
		"FAKE_GOPATH="+firstGoPath+string(os.PathListSeparator)+filepath.Join(temporary, "second go path"),
		"FAKE_INSTALL_BIN="+installBin,
		"FAKE_GO_LOG="+logPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run installer: %v\n%s", err, output)
	}

	text := string(output)
	for _, want := range []string{
		"[1/4] Checking the Go toolchain...",
		"[2/4] Installing github.com/ShanilKoshitha/goforge/cmd/forge@v0.21.0...",
		"[3/4] Verifying the installation...",
		"[4/4] Making " + installBin + " available as forge...",
		"Success: forge 0.21.0 is installed at " + filepath.Join(installBin, "forge.exe"),
		"COMMAND_PATH=" + filepath.Join(installBin, "forge.exe"),
		"PATH_ENTRY_COUNT=1",
		"MISMATCH_REJECTED=Installed Forge reported 'forge 9.9.9'; expected 'forge 0.21.0'.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("installer output does not contain %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "Success: forge 0.21.0 is installed") != 2 {
		t.Errorf("installer reported success after a version mismatch:\n%s", text)
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(log); !strings.Contains(got, "install -v github.com/ShanilKoshitha/goforge/cmd/forge@v0.21.0") {
		t.Fatalf("go invocation log = %q", got)
	}
	if _, err := os.Stat(filepath.Join(installBin, "forge.exe")); err != nil {
		t.Fatalf("installed forge: %v", err)
	}
}

func powerShellLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

const fakeGoProgram = `package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if strings.EqualFold(filepath.Base(os.Args[0]), "forge.exe") {
		version := os.Getenv("FAKE_FORGE_VERSION")
		if version == "" { version = "forge 0.21.0" }
		fmt.Println(version)
		return
	}
	if logPath := os.Getenv("FAKE_GO_LOG"); logPath != "" {
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil { panic(err) }
		fmt.Fprintln(file, strings.Join(os.Args[1:], " "))
		if err := file.Close(); err != nil { panic(err) }
	}
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("go version go1.25.0 windows/amd64")
		return
	}
	if len(os.Args) == 6 && os.Args[1] == "list" && os.Args[2] == "-m" {
		fmt.Println("v0.21.0")
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "env" && os.Args[2] == "GOBIN" {
		fmt.Println(os.Getenv("FAKE_GOBIN"))
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "env" && os.Args[2] == "GOPATH" {
		fmt.Println(os.Getenv("FAKE_GOPATH"))
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "version" && os.Args[2] == "-m" {
		fmt.Printf("%s: go1.25.0\n", os.Args[3])
		fmt.Println("\tpath\tgithub.com/ShanilKoshitha/goforge/cmd/forge")
		fmt.Println("\tmod\tgithub.com/ShanilKoshitha/goforge\tv0.21.0\th1:test")
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "install" && os.Args[2] == "-v" {
		if err := os.MkdirAll(os.Getenv("FAKE_INSTALL_BIN"), 0o755); err != nil { panic(err) }
		binary, err := os.ReadFile(os.Args[0])
		if err != nil { panic(err) }
		if err := os.WriteFile(filepath.Join(os.Getenv("FAKE_INSTALL_BIN"), "forge.exe"), binary, 0o755); err != nil { panic(err) }
		fmt.Println(os.Args[3])
		return
	}
	fmt.Fprintln(os.Stderr, "unexpected fake go arguments:", strings.Join(os.Args[1:], " "))
	os.Exit(2)
}
`
