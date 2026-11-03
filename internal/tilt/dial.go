package tilt

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// resolver is swapped in tests.
var resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
} = net.DefaultResolver

// resolveAllowed resolves host once. Without allowRemote, every address
// must be loopback, or it returns an error wrapping ErrRemoteHost.
func resolveAllowed(ctx context.Context, host string, port int, allowRemote bool) ([]net.IP, error) {
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if !allowRemote && !a.IP.IsLoopback() {
			return nil, remoteHostError(host, a.IP, port)
		}
		ips = append(ips, a.IP)
	}
	return ips, nil
}

func remoteHostError(host string, ip net.IP, port int) error {
	return fmt.Errorf("%w: %s resolves to %s. Forward the port instead: ssh -L %d:localhost:%d %s, then run tilt-tui without --host",
		ErrRemoteHost, host, ip, port, port, host)
}

// newTransport returns the only transport the client uses. Its dialer
// resolves each connection's host once, enforces the loopback rule on the
// result, and dials the validated IPs in order.
func newTransport(allowRemote bool) *http.Transport {
	d := &net.Dialer{Timeout: 5 * time.Second}
	t := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, portStr, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				return nil, err
			}
			ips, err := resolveAllowed(ctx, host, port, allowRemote)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), portStr))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        4,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	if allowRemote {
		t.Proxy = http.ProxyFromEnvironment
	}
	return t
}
