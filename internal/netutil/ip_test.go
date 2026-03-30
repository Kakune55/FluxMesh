package netutil

import (
	"errors"
	"net"
	"testing"
)

type fakeAddr string

func (a fakeAddr) Network() string { return "fake" }
func (a fakeAddr) String() string  { return string(a) }

func TestDetectLANIPv4FromInterfaces(t *testing.T) {
	interfaces := []net.Interface{
		{Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		{Name: "eth-down", Flags: 0},
		{Name: "eth-err", Flags: net.FlagUp},
		{Name: "eth-v6", Flags: net.FlagUp},
		{Name: "eth-ok", Flags: net.FlagUp},
	}

	addrLookup := func(iface net.Interface) ([]net.Addr, error) {
		switch iface.Name {
		case "eth-err":
			return nil, errors.New("addr lookup failed")
		case "eth-v6":
			return []net.Addr{
				&net.IPNet{IP: net.ParseIP("::1")},
				fakeAddr("not-ipnet"),
			}, nil
		case "eth-ok":
			return []net.Addr{
				&net.IPNet{IP: net.ParseIP("10.0.0.23")},
			}, nil
		default:
			return nil, nil
		}
	}

	got, err := detectLANIPv4FromInterfaces(interfaces, addrLookup)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if got != "10.0.0.23" {
		t.Fatalf("expected 10.0.0.23, got %s", got)
	}
}

func TestDetectLANIPv4FromInterfacesNoMatch(t *testing.T) {
	interfaces := []net.Interface{
		{Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		{Name: "eth-v6", Flags: net.FlagUp},
	}

	addrLookup := func(iface net.Interface) ([]net.Addr, error) {
		switch iface.Name {
		case "eth-v6":
			return []net.Addr{&net.IPNet{IP: net.ParseIP("2001:db8::1")}}, nil
		default:
			return nil, nil
		}
	}

	_, err := detectLANIPv4FromInterfaces(interfaces, addrLookup)
	if err == nil {
		t.Fatalf("expected no IPv4 error")
	}
	if err.Error() != "no non-loopback IPv4 found" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetectLANIPv4Wrapper(t *testing.T) {
	ip, err := DetectLANIPv4()
	if err != nil {
		if err.Error() == "" {
			t.Fatalf("expected non-empty error when detection fails")
		}
		return
	}

	if net.ParseIP(ip) == nil {
		t.Fatalf("expected a valid ip string, got %q", ip)
	}
}
