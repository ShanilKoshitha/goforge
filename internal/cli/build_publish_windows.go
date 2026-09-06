//go:build windows

package cli

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x00000001
	moveFileWriteThrough    = 0x00000008
)

var moveBuildFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func publishBuildArtifact(source, destination string) error {
	sourcePointer, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return fmt.Errorf("encode temporary build path: %w", err)
	}
	destinationPointer, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return fmt.Errorf("encode build destination: %w", err)
	}
	result, _, callErr := moveBuildFileEx.Call(
		uintptr(unsafe.Pointer(sourcePointer)),
		uintptr(unsafe.Pointer(destinationPointer)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if result == 0 {
		return fmt.Errorf("replace build destination: %w", callErr)
	}
	return nil
}
