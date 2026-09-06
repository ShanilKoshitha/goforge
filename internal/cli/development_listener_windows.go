package cli

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"unsafe"
)

var getExtendedTCPTable = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

func processOwnsDevelopmentAddress(pid int, address string) (bool, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return false, err
	}
	expectedIP := net.ParseIP(host).To4()
	if expectedIP == nil {
		return false, fmt.Errorf("development listener address %q is not IPv4", host)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return false, err
	}
	var size uint32
	result, _, _ := getExtendedTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, syscall.AF_INET, 3, 0)
	if result != 122 {
		return false, fmt.Errorf("size Windows TCP owner table: status %d", result)
	}
	var buffer []byte
	for attempt := 0; attempt < 4; attempt++ {
		if size == 0 {
			return false, fmt.Errorf("Windows TCP owner table reported an empty buffer")
		}
		buffer = make([]byte, size)
		result, _, _ = getExtendedTCPTable.Call(
			uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), 0, syscall.AF_INET, 3, 0,
		)
		if result == 0 {
			break
		}
		if result != 122 {
			return false, fmt.Errorf("read Windows TCP owner table: status %d", result)
		}
	}
	if result != 0 {
		return false, fmt.Errorf("Windows TCP owner table kept growing during ownership verification")
	}
	if len(buffer) < 4 {
		return false, fmt.Errorf("Windows TCP owner table is truncated")
	}
	rows := int(binary.LittleEndian.Uint32(buffer[:4]))
	const rowSize = 24
	for row := 0; row < rows; row++ {
		offset := 4 + row*rowSize
		if offset+rowSize > len(buffer) {
			return false, fmt.Errorf("Windows TCP owner row is truncated")
		}
		localPort := binary.BigEndian.Uint16(buffer[offset+8 : offset+10])
		ownerPID := binary.LittleEndian.Uint32(buffer[offset+20 : offset+24])
		localIP := net.IP(buffer[offset+4 : offset+8])
		addressMatches := localIP.Equal(expectedIP) || localIP.Equal(net.IPv4zero)
		if localPort == uint16(port) && ownerPID == uint32(pid) && addressMatches {
			return true, nil
		}
	}
	return false, nil
}
