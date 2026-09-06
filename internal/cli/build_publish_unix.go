//go:build aix || android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris

package cli

import "os"

func publishBuildArtifact(source, destination string) error {
	return os.Rename(source, destination)
}
