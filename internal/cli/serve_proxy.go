package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	serveProxyReadHeaderTimeout = 5 * time.Second
	serveProxyIdleTimeout       = 2 * time.Minute
	serveProxyMaxHeaderBytes    = 1 << 20
)

// serveProxy keeps one public development address while its backend changes.
// Application routes pass through unchanged; eligible browser documents gain
// the development-only LiveReload client at this proxy boundary.
type serveProxy struct {
	address string
	server  *http.Server
	target  atomic.Pointer[url.URL]
	done    chan error
	reload  *serveLiveReload

	closeOnce sync.Once
	closeErr  error
}

// startServeProxy starts a reverse proxy on address. Only the target origin is
// used; the incoming Host, path, and query remain the application's request.
func startServeProxy(address string, target *url.URL) (*serveProxy, error) {
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("serve proxy address is required")
	}
	initial, err := normalizeServeProxyTarget(target)
	if err != nil {
		return nil, err
	}
	reload, err := newServeLiveReload()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		reload.Close()
		return nil, fmt.Errorf("listen for development proxy on %s: %w", address, err)
	}

	proxy := &serveProxy{
		address: listener.Addr().String(),
		done:    make(chan error, 1),
		reload:  reload,
	}
	proxy.target.Store(initial)
	reverse := &httputil.ReverseProxy{
		Rewrite:        proxy.rewrite,
		ModifyResponse: reload.ModifyResponse,
	}
	proxy.server = &http.Server{
		Handler:           reload.Handler(reverse),
		ReadHeaderTimeout: serveProxyReadHeaderTimeout,
		IdleTimeout:       serveProxyIdleTimeout,
		MaxHeaderBytes:    serveProxyMaxHeaderBytes,
	}
	go func() {
		err := proxy.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		proxy.done <- err
		close(proxy.done)
	}()
	return proxy, nil
}

func (proxy *serveProxy) rewrite(request *httputil.ProxyRequest) {
	target := proxy.target.Load()
	request.Out.URL.Scheme = target.Scheme
	request.Out.URL.Host = target.Host
	request.Out.URL.User = target.User
	request.Out.URL.Path = request.In.URL.Path
	request.Out.URL.RawPath = request.In.URL.RawPath
	request.Out.URL.RawQuery = request.In.URL.RawQuery
	request.Out.URL.ForceQuery = request.In.URL.ForceQuery
	request.Out.Host = request.In.Host
	request.SetXForwarded()
}

// Address returns the actual listener address, including its allocated port
// when startServeProxy was called with port zero.
func (proxy *serveProxy) Address() string {
	return proxy.address
}

// SwapTarget directs new requests to target. Requests already in flight retain
// the target selected when they were rewritten.
func (proxy *serveProxy) SwapTarget(target *url.URL) error {
	normalized, err := normalizeServeProxyTarget(target)
	if err != nil {
		return err
	}
	proxy.target.Store(normalized)
	return nil
}

// NotifyReload publishes one accepted development generation. The supervisor
// calls it only after the candidate and compiled views are fully committed.
func (proxy *serveProxy) NotifyReload() {
	proxy.reload.NotifyReload()
}

// Done reports the terminal Serve result. A graceful Close reports nil.
func (proxy *serveProxy) Done() <-chan error {
	return proxy.done
}

// Close gracefully stops the listener. Repeated calls return the result of the
// first call and do not begin another shutdown.
func (proxy *serveProxy) Close(ctx context.Context) error {
	proxy.closeOnce.Do(func() {
		// SSE handlers are intentionally long lived. Stop them before Shutdown so
		// graceful cancellation does not wait for the outer shutdown timeout.
		proxy.reload.Close()
		proxy.closeErr = proxy.server.Shutdown(ctx)
		if errors.Is(proxy.closeErr, http.ErrServerClosed) {
			proxy.closeErr = nil
		}
	})
	return proxy.closeErr
}

func normalizeServeProxyTarget(target *url.URL) (*url.URL, error) {
	if target == nil {
		return nil, errors.New("serve proxy target is required")
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("serve proxy target scheme must be http or https, got %q", target.Scheme)
	}
	if target.Host == "" {
		return nil, errors.New("serve proxy target host is required")
	}
	result := *target
	// The target represents an origin, not a path prefix. Keeping request paths
	// intact also makes target promotion independent of application routing.
	result.Path = ""
	result.RawPath = ""
	result.RawQuery = ""
	result.ForceQuery = false
	result.Fragment = ""
	return &result, nil
}
