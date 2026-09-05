package ratelimit

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

var ErrInvalidForwardedChain = errors.New("rate limit: invalid forwarded address chain")

const (
	maximumTrustedProxyPrefixes = 256
	maximumForwardedHeaderBytes = 8 << 10
	maximumForwardedHops        = 64
	maximumForwardedParameters  = 32
)

// TrustedProxies is an explicit allowlist of network peers whose forwarding
// metadata may participate in client address resolution. Its zero value trusts
// no peer. Prefixes are immutable after construction and safe for concurrent
// use.
type TrustedProxies struct {
	prefixes []netip.Prefix
}

// ParseTrustedProxies validates a list of CIDR prefixes. Each argument may
// contain a comma-separated list, which makes the function convenient for an
// environment setting. An empty list is valid and trusts no peer. Bare IP
// addresses are rejected so the configured trust boundary remains conspicuous.
func ParseTrustedProxies(values ...string) (TrustedProxies, error) {
	var prefixes []netip.Prefix
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		for _, encoded := range strings.Split(value, ",") {
			encoded = strings.TrimSpace(encoded)
			if encoded == "" {
				return TrustedProxies{}, fmt.Errorf("rate limit: trusted proxy CIDR contains an empty entry")
			}
			prefix, err := netip.ParsePrefix(encoded)
			if err != nil || prefix.Addr().Zone() != "" {
				return TrustedProxies{}, fmt.Errorf("rate limit: invalid trusted proxy CIDR %q", encoded)
			}
			if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
				prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
			}
			prefixes = append(prefixes, prefix.Masked())
			if len(prefixes) > maximumTrustedProxyPrefixes {
				return TrustedProxies{}, fmt.Errorf("rate limit: trusted proxy CIDRs exceed %d entries", maximumTrustedProxyPrefixes)
			}
		}
	}
	return TrustedProxies{prefixes: prefixes}, nil
}

// Contains reports whether address belongs to an explicitly configured proxy
// prefix. IPv4-mapped IPv6 addresses are normalized before comparison.
func (trusted TrustedProxies) Contains(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	for _, prefix := range trusted.prefixes {
		if prefix.Addr().Unmap().BitLen() == address.BitLen() && prefix.Contains(address) {
			return true
		}
	}
	return false
}

// ClientAddress resolves the effective client without ever trusting forwarding
// metadata automatically. Forwarded or X-Forwarded-For is considered only when
// the socket peer belongs to trusted. A malformed or ambiguous chain safely
// falls back to RemoteAddress.
func (trusted TrustedProxies) ClientAddress(request *http.Request) string {
	address, err := trusted.ResolveClientAddress(request)
	if err != nil {
		return RemoteAddress(request)
	}
	return address
}

// ResolveClientAddress is ClientAddress with a diagnostic error for malformed
// forwarding data. It still returns the direct peer as its address on error so
// callers can log the problem without accidentally trusting attacker input.
func (trusted TrustedProxies) ResolveClientAddress(request *http.Request) (string, error) {
	directText := RemoteAddress(request)
	direct, err := parseDirectAddress(directText)
	if err != nil || !trusted.Contains(direct) {
		return directText, nil
	}

	forwardedValues := nonemptyHeaderValues(request, "Forwarded")
	legacyValues := nonemptyHeaderValues(request, "X-Forwarded-For")
	if len(forwardedValues) == 0 && len(legacyValues) == 0 {
		return direct.String(), nil
	}
	if len(forwardedValues) != 0 && len(legacyValues) != 0 {
		return directText, fmt.Errorf("%w: both Forwarded and X-Forwarded-For are present", ErrInvalidForwardedChain)
	}
	// Repeated fields can have different combination behavior across proxies.
	// Reject them rather than guessing which hop appended which value.
	if len(forwardedValues) > 1 || len(legacyValues) > 1 {
		return directText, fmt.Errorf("%w: repeated forwarding header", ErrInvalidForwardedChain)
	}

	var chain []netip.Addr
	if len(forwardedValues) == 1 {
		chain, err = parseForwarded(forwardedValues[0])
	} else {
		chain, err = parseXForwardedFor(legacyValues[0])
	}
	if err != nil || len(chain) == 0 {
		if err == nil {
			err = errors.New("empty chain")
		}
		return directText, fmt.Errorf("%w: %v", ErrInvalidForwardedChain, err)
	}

	current := direct.Unmap()
	for index := len(chain) - 1; index >= 0; index-- {
		if !trusted.Contains(current) {
			return current.String(), nil
		}
		current = chain[index].Unmap()
	}
	return current.String(), nil
}

func nonemptyHeaderValues(request *http.Request, name string) []string {
	if request == nil {
		return nil
	}
	values := request.Header.Values(name)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func parseDirectAddress(value string) (netip.Addr, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || address.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("invalid direct peer")
	}
	return address.Unmap(), nil
}

func parseXForwardedFor(value string) ([]netip.Addr, error) {
	if len(value) > maximumForwardedHeaderBytes {
		return nil, errors.New("X-Forwarded-For exceeds size limit")
	}
	parts := strings.Split(value, ",")
	if len(parts) > maximumForwardedHops {
		return nil, errors.New("X-Forwarded-For exceeds hop limit")
	}
	chain := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		address, err := parseForwardedNode(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		chain = append(chain, address)
	}
	return chain, nil
}

func parseForwarded(value string) ([]netip.Addr, error) {
	if len(value) > maximumForwardedHeaderBytes {
		return nil, errors.New("Forwarded exceeds size limit")
	}
	elements, err := splitForwarded(value, ',')
	if err != nil {
		return nil, err
	}
	if len(elements) > maximumForwardedHops {
		return nil, errors.New("Forwarded exceeds hop limit")
	}
	chain := make([]netip.Addr, 0, len(elements))
	for _, element := range elements {
		parameters, err := splitForwarded(element, ';')
		if err != nil {
			return nil, err
		}
		if len(parameters) > maximumForwardedParameters {
			return nil, errors.New("Forwarded element exceeds parameter limit")
		}
		found := false
		for _, parameter := range parameters {
			name, encoded, ok := strings.Cut(parameter, "=")
			name = strings.TrimSpace(name)
			encoded = strings.TrimSpace(encoded)
			if !ok || !isForwardedToken(name) {
				return nil, errors.New("malformed Forwarded parameter")
			}
			if !strings.EqualFold(name, "for") {
				if _, err := decodeForwardedValue(encoded); err != nil {
					return nil, err
				}
				continue
			}
			if found {
				return nil, errors.New("duplicate Forwarded for parameter")
			}
			found = true
			decoded, err := decodeForwardedValue(encoded)
			if err != nil {
				return nil, err
			}
			address, err := parseForwardedNode(decoded)
			if err != nil {
				return nil, err
			}
			chain = append(chain, address)
		}
		if !found {
			return nil, errors.New("Forwarded element has no for parameter")
		}
	}
	return chain, nil
}

func splitForwarded(value string, separator byte) ([]string, error) {
	var result []string
	start := 0
	quoted := false
	escaped := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if escaped {
			escaped = false
			continue
		}
		if quoted && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			continue
		}
		if !quoted && character == separator {
			part := strings.TrimSpace(value[start:index])
			if part == "" {
				return nil, errors.New("empty Forwarded element")
			}
			result = append(result, part)
			start = index + 1
		}
	}
	if quoted || escaped {
		return nil, errors.New("unterminated Forwarded quoted string")
	}
	part := strings.TrimSpace(value[start:])
	if part == "" {
		return nil, errors.New("empty Forwarded element")
	}
	return append(result, part), nil
}

func decodeForwardedValue(value string) (string, error) {
	if value == "" {
		return "", errors.New("empty Forwarded value")
	}
	if value[0] != '"' {
		if strings.ContainsAny(value, "\"\\ ") || hasInvalidForwardedRune(value) {
			return "", errors.New("invalid unquoted Forwarded value")
		}
		return value, nil
	}
	decoded, err := strconv.Unquote(value)
	if err != nil || hasInvalidForwardedRune(decoded) {
		return "", errors.New("invalid Forwarded quoted value")
	}
	return decoded, nil
}

func isForwardedToken(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", character) {
			continue
		}
		return false
	}
	return true
}

func hasInvalidForwardedRune(value string) bool {
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return true
		}
	}
	return false
}

func parseForwardedNode(value string) (netip.Addr, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "unknown") || strings.HasPrefix(value, "_") {
		return netip.Addr{}, errors.New("forwarded node is not an IP address")
	}
	if address, err := netip.ParseAddr(value); err == nil && address.Zone() == "" {
		return address.Unmap(), nil
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		if address, err := netip.ParseAddr(value[1 : len(value)-1]); err == nil && address.Zone() == "" {
			return address.Unmap(), nil
		}
	}
	if addressPort, err := netip.ParseAddrPort(value); err == nil && addressPort.Addr().Zone() == "" {
		return addressPort.Addr().Unmap(), nil
	}
	// netip intentionally requires brackets around IPv6 with a port. Retain
	// net.SplitHostPort only for conventional IPv4 host:port input.
	host, port, err := net.SplitHostPort(value)
	if err == nil && port != "" {
		address, parseErr := netip.ParseAddr(host)
		if parseErr == nil && address.Zone() == "" {
			return address.Unmap(), nil
		}
	}
	return netip.Addr{}, errors.New("forwarded node is not a valid IP address")
}
