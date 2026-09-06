//go:build plan9 || js || wasip1

package cli

import "fmt"

func processOwnsDevelopmentAddress(int, string) (bool, error) {
	return false, fmt.Errorf("development listener ownership verification is unsupported on this operating system")
}
