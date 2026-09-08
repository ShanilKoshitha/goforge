//go:build aix || darwin || dragonfly || freebsd || illumos || ios || netbsd || openbsd || solaris

package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func processOwnsDevelopmentAddress(pid int, address string) (bool, error) {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return false, fmt.Errorf("locate lsof for development listener ownership: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	output, err := exec.CommandContext(
		ctx, lsof, "-nP", "-a", "-p", strconv.Itoa(pid), "-iTCP@"+address, "-sTCP:LISTEN", "-Fp",
	).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("inspect development listener owner: %w", err)
	}
	return strings.Contains(string(output), "p"+strconv.Itoa(pid)), nil
}
