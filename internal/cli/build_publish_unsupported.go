//go:build plan9 || js || wasip1

package cli

import "os"

func publishBuildArtifact(source, destination string) error {
	return os.Rename(source, destination)
}
