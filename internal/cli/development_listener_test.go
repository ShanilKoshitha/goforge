package cli

import (
	"net"
	"os"
	"strings"
	"testing"
)

func TestProcessOwnsDevelopmentAddressIdentifiesListenerPID(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	owned, err := processOwnsDevelopmentAddress(os.Getpid(), listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if !owned {
		t.Fatalf("process %d was not identified as owner of %s", os.Getpid(), listener.Addr())
	}
	wrongAddress := strings.Replace(listener.Addr().String(), "127.0.0.1", "127.0.0.2", 1)
	owned, err = processOwnsDevelopmentAddress(os.Getpid(), wrongAddress)
	if err != nil {
		t.Fatal(err)
	}
	if owned {
		t.Fatalf("process listener on %s was incorrectly matched to %s", listener.Addr(), wrongAddress)
	}
}
