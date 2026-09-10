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
		requests <- fmt.Sprintf("%s %s %s %s", request.Host, request.URL.EscapedPath(), request.URL.RawQuery, request.Header.Get("X-Forwarded-For"))
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
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
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
		wantPrefix := "app.example.test:8080 /items/a%2Fb sort=name&tag=one&tag=two "
		if !strings.HasPrefix(got, wantPrefix) || strings.Contains(got, "203.0.113.9") {
			t.Fatalf("backend request retained spoofed forwarding metadata: %q", got)
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

func TestServeProxyInjectsLiveReloadAndPublishesCommittedGeneration(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Content-Security-Policy", "default-src 'self'")
		http.SetCookie(response, &http.Cookie{Name: "session", Value: "kept", HttpOnly: true})
		_, _ = io.WriteString(response, "<!doctype html><html><body>page</body></html>")
	}))
	defer backend.Close()
	proxy := newTestServeProxy(t, backend.URL)

	page := requestServeProxyResponse(t, proxy, "/")
	defer page.Body.Close()
	body, err := io.ReadAll(page.Body)
	if err != nil {
		t.Fatal(err)
	}
	wantTag := `<script src="` + proxy.reload.ScriptPath() + `" data-goforge-generation="0" defer></script>`
	if !strings.Contains(string(body), "page"+wantTag+"</body>") {
		t.Fatalf("proxied page is missing LiveReload client:\n%s", body)
	}
	if page.Header.Get("Content-Security-Policy") != "default-src 'self'" || len(page.Cookies()) != 1 || page.Cookies()[0].Value != "kept" {
		t.Fatalf("application response semantics changed: headers=%v cookies=%v", page.Header, page.Cookies())
	}

	events, err := http.Get("http://" + proxy.Address() + proxy.reload.EventsPath() + "?since=0")
	if err != nil {
		t.Fatal(err)
	}
	defer events.Body.Close()
	proxy.NotifyReload()
	eventBody, err := io.ReadAll(events.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(eventBody), "event: reload\ndata: 1\n\n") {
		t.Fatalf("committed reload event = %q", eventBody)
	}

	accepted := requestServeProxyResponse(t, proxy, "/")
	defer accepted.Body.Close()
	acceptedBody, err := io.ReadAll(accepted.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(acceptedBody), `data-goforge-generation="1"`) {
		t.Fatalf("accepted page does not carry generation 1:\n%s", acceptedBody)
	}
}

func TestServeProxyLeavesTrailersAndStreamedHTMLUntouched(t *testing.T) {
	t.Run("trailers", func(t *testing.T) {
		backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			response.Header().Set("Trailer", "Digest")
			_, _ = io.WriteString(response, "<html><body>trailed</body></html>")
			response.Header().Set("Digest", "sha-256=upstream")
		}))
		defer backend.Close()
		proxy := newTestServeProxy(t, backend.URL)

		response := requestServeProxyResponse(t, proxy, "/")
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(body), "<html><body>trailed</body></html>"; got != want || strings.Contains(got, proxy.reload.ScriptPath()) {
			t.Fatalf("trailed response body = %q, want %q", got, want)
		}
		if response.Trailer.Get("Digest") != "sha-256=upstream" {
			t.Fatalf("application trailer changed: %v", response.Trailer)
		}
	})

	t.Run("stream", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		defer func() {
			select {
			case <-release:
			default:
				close(release)
			}
		}()
		backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(response, "<html><body>stream-")
			response.(http.Flusher).Flush()
			close(started)
			<-release
			_, _ = io.WriteString(response, "finished</body></html>")
		}))
		defer backend.Close()
		proxy := newTestServeProxy(t, backend.URL)

		response := requestServeProxyResponse(t, proxy, "/")
		defer response.Body.Close()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("backend did not flush streamed HTML")
		}
		prefix := make([]byte, len("<html><body>stream-"))
		read := make(chan error, 1)
		go func() {
			_, err := io.ReadFull(response.Body, prefix)
			read <- err
		}()
		select {
		case err := <-read:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("development proxy buffered streamed HTML")
		}
		if got, want := string(prefix), "<html><body>stream-"; got != want {
			t.Fatalf("stream prefix = %q, want %q", got, want)
		}
		close(release)
		remainder, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(remainder), "finished</body></html>"; got != want || strings.Contains(got, proxy.reload.ScriptPath()) {
			t.Fatalf("stream remainder = %q, want %q", got, want)
		}
	})
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

func TestServeProxyBoundsThePublicDevelopmentEdge(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer backend.Close()
	target, err := url.Parse(backend.URL)
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
	if proxy.server.ReadHeaderTimeout != serveProxyReadHeaderTimeout ||
		proxy.server.IdleTimeout != serveProxyIdleTimeout ||
		proxy.server.MaxHeaderBytes != serveProxyMaxHeaderBytes {
		t.Fatalf("unbounded proxy server: %+v", proxy.server)
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

func requestServeProxyResponse(t *testing.T, proxy *serveProxy, path string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "http://"+proxy.Address()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Sec-Fetch-Dest", "document")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
