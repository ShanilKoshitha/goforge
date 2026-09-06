package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestServeProxyRoutesWithoutRewritingRequestIdentity(t *testing.T) {
	requests := make(chan string, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests <- fmt.Sprintf("%s %s %s", request.Host, request.URL.EscapedPath(), request.URL.RawQuery)
		response.Header().Set("X-Backend", "one")
		_, _ = io.WriteString(response, "proxied")
	}))
	defer backend.Close()

	proxy := newTestServeProxy(t, backend.URL)
	request, err := http.NewRequest(http.MethodGet, "http://"+proxy.Address()+"/items/a%2Fb?sort=name&tag=one&tag=two", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "app.example.test:8080"
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Backend") != "one" || string(body) != "proxied" {
		t.Fatalf("proxy response = %d, %q, %q", response.StatusCode, response.Header.Get("X-Backend"), body)
	}
	select {
	case got := <-requests:
		want := "app.example.test:8080 /items/a%2Fb sort=name&tag=one&tag=two"
		if got != want {
			t.Fatalf("backend request = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("backend did not receive request")
	}
}

func TestServeProxyAtomicallySwapsBackend(t *testing.T) {
	backend := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(response, name)
		}))
	}
	first := backend("first")
	defer first.Close()
	second := backend("second")
	defer second.Close()

	proxy := newTestServeProxy(t, first.URL)
	if got := requestServeProxy(t, proxy); got != "first" {
		t.Fatalf("initial backend response = %q", got)
	}
	secondURL, err := url.Parse(second.URL + "/ignored-prefix?ignored=query")
	if err != nil {
		t.Fatal(err)
	}
	if err := proxy.SwapTarget(secondURL); err != nil {
		t.Fatal(err)
	}
	if got := requestServeProxy(t, proxy); got != "second" {
		t.Fatalf("promoted backend response = %q", got)
	}
}

func TestServeProxyReportsAddressCollision(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	target, err := url.Parse("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := startServeProxy(listener.Addr().String(), target)
	if err == nil {
		_ = proxy.Close(context.Background())
		t.Fatal("proxy unexpectedly acquired occupied address")
	}
	if !strings.Contains(err.Error(), "listen for development proxy") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestServeProxyCloseIsIdempotentAndCompletesServe(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	proxy := newTestServeProxy(t, backend.URL)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := proxy.Close(ctx); err != nil {
		t.Fatal(err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if err := proxy.Close(cancelled); err != nil {
		t.Fatalf("second close = %v", err)
	}
	select {
	case err := <-proxy.Done():
		if err != nil {
			t.Fatalf("serve completion = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy Serve did not complete")
	}
}

func TestServeProxyRejectsInvalidStartupTarget(t *testing.T) {
	for _, target := range []*url.URL{nil, {}, {Scheme: "ftp", Host: "example.test"}, {Scheme: "http"}} {
		if proxy, err := startServeProxy("127.0.0.1:0", target); err == nil {
			_ = proxy.Close(context.Background())
			t.Fatalf("startServeProxy(%v) succeeded", target)
		}
	}
}

func newTestServeProxy(t *testing.T, backend string) *serveProxy {
	t.Helper()
	target, err := url.Parse(backend)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := startServeProxy("127.0.0.1:0", target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := proxy.Close(ctx); err != nil {
			t.Errorf("close serve proxy: %v", err)
		}
	})
	return proxy
}

func requestServeProxy(t *testing.T, proxy *serveProxy) string {
	t.Helper()
	response, err := http.Get("http://" + proxy.Address() + "/kept?kept=true")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
