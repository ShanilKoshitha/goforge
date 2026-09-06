//go:build linux || android

package cli

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func processOwnsDevelopmentAddress(pid int, address string) (bool, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return false, err
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return false, err
	}
	inodes, err := processSocketInodes(pid)
	if err != nil {
		return false, err
	}
	expectedAddress, err := procTCPAddress(host)
	if err != nil {
		return false, err
	}
	owned, tableErr := processOwnsDevelopmentPort(
		filepath.Join("/proc", strconv.Itoa(pid), "net", "tcp"), expectedAddress, uint16(port), inodes,
	)
	if tableErr != nil && !os.IsNotExist(tableErr) {
		return false, tableErr
	}
	return owned, nil
}

func procTCPAddress(host string) (string, error) {
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return "", fmt.Errorf("development listener address %q is not IPv4", host)
	}
	return strings.ToUpper(hex.EncodeToString([]byte{ip[3], ip[2], ip[1], ip[0]})), nil
}

func processSocketInodes(pid int) (map[string]struct{}, error) {
	directory := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read process descriptors: %w", err)
	}
	inodes := make(map[string]struct{})
	for _, entry := range entries {
		target, readErr := os.Readlink(filepath.Join(directory, entry.Name()))
		if readErr != nil {
			continue
		}
		if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inodes[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = struct{}{}
		}
	}
	return inodes, nil
}

func processOwnsDevelopmentPort(path, expectedAddress string, port uint16, inodes map[string]struct{}) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[3] != "0A" {
			continue
		}
		addressHex, portHex, found := strings.Cut(fields[1], ":")
		if !found {
			continue
		}
		observed, parseErr := strconv.ParseUint(portHex, 16, 16)
		if parseErr != nil || uint16(observed) != port || (addressHex != expectedAddress && addressHex != "00000000") {
			continue
		}
		if _, owned := inodes[fields[9]]; owned {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read process TCP table: %w", err)
	}
	return false, nil
}
