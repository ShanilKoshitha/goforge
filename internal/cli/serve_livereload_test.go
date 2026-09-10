package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestServeLiveReloadUsesExactDeterministicRandomPaths(t *testing.T) {
	entropy := make([]byte, serveLiveReloadEntropyBytes)
	for index := range entropy {
		entropy[index] = byte(index)
	}
	reload, err := newServeLiveReloadWithReader(bytes.NewReader(entropy))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reload.Close)
	token := base64.RawURLEncoding.EncodeToString(entropy)
	if got, want := reload.ScriptPath(), "/.goforge/livereload/"+token+"/client.js"; got != want {
		t.Fatalf("script path = %q, want %q", got, want)
	}
	if got, want := reload.EventsPath(), "/.goforge/livereload/"+token+"/events"; got != want {
		t.Fatalf("events path = %q, want %q", got, want)
	}
	if strings.ContainsAny(token, "+/=") || len(token) != 43 {
		t.Fatalf("token is not a 256-bit unpadded URL-safe value: %q", token)
	}
}

func TestServeLiveReloadEntropyFailures(t *testing.T) {
	if _, err := newServeLiveReloadWithReader(nil); err == nil {
		t.Fatal("nil entropy reader unexpectedly succeeded")
	}
	if _, err := newServeLiveReloadWithReader(bytes.NewReader(make([]byte, serveLiveReloadEntropyBytes-1))); err == nil {
		t.Fatal("short entropy unexpectedly succeeded")
	}
	first, err := newServeLiveReloadWithReader(bytes.NewReader(bytes.Repeat([]byte{1}, serveLiveReloadEntropyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	second, err := newServeLiveReloadWithReader(bytes.NewReader(bytes.Repeat([]byte{2}, serveLiveReloadEntropyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	if first.ScriptPath() == second.ScriptPath() || first.EventsPath() == second.EventsPath() {
		t.Fatal("distinct entropy produced colliding endpoints")
	}
}

func TestServeLiveReloadHandlerClaimsOnlyExactPaths(t *testing.T) {
	reload := newTestServeLiveReload(t)
	var forwarded []string
	handler := reload.Handler(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		forwarded = append(forwarded, request.URL.RequestURI())
		response.Header().Set("X-Application", "yes")
		response.WriteHeader(http.StatusTeapot)
	}))

	for _, target := range []string{
		"/",
		reload.ScriptPath() + "/",
		reload.EventsPath() + "/child",
		reload.ScriptPath()[:len(reload.ScriptPath())-1],
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusTeapot || response.Header().Get("X-Application") != "yes" {
			t.Fatalf("application path %q was claimed: status=%d", target, response.Code)
		}
	}
	encoded := strings.Replace(reload.ScriptPath(), "client.js", "client%2Ejs", 1)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, encoded, nil))
	if response.Code != http.StatusTeapot {
		t.Fatalf("non-canonical encoded path was claimed: %d", response.Code)
	}
	if len(forwarded) != 5 {
		t.Fatalf("forwarded %d requests, want 5", len(forwarded))
	}
}

func TestServeLiveReloadScriptIsExternalSameOriginClient(t *testing.T) {
	reload := newTestServeLiveReload(t)
	response := httptest.NewRecorder()
	reload.Handler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, reload.ScriptPath(), nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/javascript; charset=utf-8" ||
		response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Cross-Origin-Resource-Policy") != "same-origin" ||
		response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected script response: status=%d headers=%v", response.Code, response.Header())
	}
	for _, required := range []string{
		"document.currentScript",
		"script.dataset.goforgeGeneration",
		"new EventSource(\"" + reload.EventsPath() + "\"",
		"source.addEventListener(\"reload\"",
		"source.close();",
		"window.location.reload();",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("client script is missing %q:\n%s", required, body)
		}
	}
	if strings.Index(body, "source.close();") > strings.Index(body, "window.location.reload();") {
		t.Error("client reloads before closing its EventSource")
	}
	if got := response.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length = %q, want %d", got, len(body))
	}
}

func TestServeLiveReloadEndpointsRequireGETAndValidSince(t *testing.T) {
	reload := newTestServeLiveReload(t)
	for _, path := range []string{reload.ScriptPath(), reload.EventsPath() + "?since=0"} {
		for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut} {
			response := httptest.NewRecorder()
			reload.Handler(nil).ServeHTTP(response, httptest.NewRequest(method, path, nil))
			if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
				t.Errorf("%s %s: status=%d Allow=%q", method, path, response.Code, response.Header().Get("Allow"))
			}
		}
	}
	for _, rawQuery := range []string{
		"", "since=", "other=0", "since=0&other=1", "since=0&since=1",
		"since=-1", "since=+1", "since=1.0", "since=nope", "since=%zz",
		"since=18446744073709551616",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, reload.EventsPath()+"?"+rawQuery, nil)
		reload.Handler(nil).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("query %q: status=%d, want 400", rawQuery, response.Code)
		}
	}
	future := httptest.NewRecorder()
	reload.Handler(nil).ServeHTTP(future, httptest.NewRequest(http.MethodGet, reload.EventsPath()+"?since=1", nil))
	if future.Code != http.StatusBadRequest {
		t.Fatalf("future generation status = %d, want 400", future.Code)
	}
}

func TestServeLiveReloadMissedGenerationRespondsImmediately(t *testing.T) {
	reload := newTestServeLiveReload(t)
	reload.NotifyReload()
	reload.NotifyReload()
	response := httptest.NewRecorder()
	reload.Handler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, reload.EventsPath()+"?since=0", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" ||
		response.Header().Get("Cross-Origin-Resource-Policy") != "same-origin" || response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected missed-generation response: status=%d headers=%v", response.Code, response.Header())
	}
	if got, want := response.Body.String(), "id: 2\nevent: reload\ndata: 2\n\n"; got != want {
		t.Fatalf("event body = %q, want %q", got, want)
	}
	if count := testServeLiveReloadSubscriberCount(reload); count != 0 {
		t.Fatalf("missed event consumed subscriber capacity: %d", count)
	}
}

func TestServeLiveReloadNotifiesConcurrentSubscribersOnce(t *testing.T) {
	reload := newTestServeLiveReload(t)
	reload.heartbeatInterval = time.Hour
	server := httptest.NewServer(reload.Handler(nil))
	t.Cleanup(server.Close)

	const clients = 8
	responses := make([]*http.Response, clients)
	for index := range responses {
		response, err := http.Get(server.URL + reload.EventsPath() + "?since=0")
		if err != nil {
			t.Fatal(err)
		}
		responses[index] = response
	}
	if count := testServeLiveReloadSubscriberCount(reload); count != clients {
		t.Fatalf("subscriber count = %d, want %d", count, clients)
	}
	reload.NotifyReload()
	for index, response := range responses {
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatalf("client %d: %v", index, err)
		}
		if got, want := string(body), ": connected\n\nid: 1\nevent: reload\ndata: 1\n\n"; got != want {
			t.Errorf("client %d body = %q, want %q", index, got, want)
		}
	}
	waitForServeLiveReloadSubscribers(t, reload, 0)
	if reload.Generation() != 1 {
		t.Fatalf("generation = %d, want 1", reload.Generation())
	}
}

func TestServeLiveReloadBoundsSubscribersAndCleansUpCancellation(t *testing.T) {
	reload := newTestServeLiveReload(t)
	reload.maxSubscribers = 1
	reload.heartbeatInterval = time.Hour
	server := httptest.NewServer(reload.Handler(nil))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	firstRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+reload.EventsPath()+"?since=0", nil)
	first, err := http.DefaultClient.Do(firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Body.Close()
	if count := testServeLiveReloadSubscriberCount(reload); count != 1 {
		t.Fatalf("subscriber count = %d, want 1", count)
	}
	second, err := http.Get(server.URL + reload.EventsPath() + "?since=0")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	if second.StatusCode != http.StatusServiceUnavailable || second.Header.Get("Retry-After") != "1" {
		t.Fatalf("overflow status=%d headers=%v", second.StatusCode, second.Header)
	}
	cancel()
	waitForServeLiveReloadSubscribers(t, reload, 0)
}

func TestServeLiveReloadHeartbeatsAndShutdown(t *testing.T) {
	reload := newTestServeLiveReload(t)
	reload.heartbeatInterval = 2 * time.Millisecond
	server := httptest.NewServer(reload.Handler(nil))
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + reload.EventsPath() + "?since=0")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	for _, want := range []string{": connected\n", "\n", ": heartbeat\n", "\n"} {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line != want {
			t.Fatalf("SSE line = %q, want %q", line, want)
		}
	}
	reload.Close()
	remaining, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("unexpected data after shutdown: %q", remaining)
	}
	_ = response.Body.Close()
	waitForServeLiveReloadSubscribers(t, reload, 0)
	reload.Close()
	reload.NotifyReload()
	if reload.Generation() != 0 {
		t.Fatalf("generation advanced after close: %d", reload.Generation())
	}
	after, err := http.Get(server.URL + reload.EventsPath() + "?since=0")
	if err != nil {
		t.Fatal(err)
	}
	defer after.Body.Close()
	if after.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("post-close subscription status = %d", after.StatusCode)
	}
}

func TestServeLiveReloadInjectsEligibleDocumentAndPreservesApplicationSemantics(t *testing.T) {
	reload := newTestServeLiveReload(t)
	request := stampedServeLiveReloadRequest(t, reload, http.MethodGet, "/app")
	reload.NotifyReload()
	original := &trackingReadCloser{Reader: strings.NewReader("<!doctype html><HTML><body>hello</BODY >\n</HTML>  ")}
	response := &http.Response{
		Status:           "200 Wonderful",
		StatusCode:       http.StatusOK,
		Proto:            "HTTP/1.1",
		ProtoMajor:       1,
		ProtoMinor:       1,
		Body:             original,
		ContentLength:    51,
		TransferEncoding: nil,
		Request:          request,
		Header: http.Header{
			"Content-Type":            {"text/html; charset=utf-8"},
			"Content-Encoding":        {"identity"},
			"Content-Disposition":     {"inline; filename=page.html"},
			"Content-Length":          {"51"},
			"Cache-Control":           {"private, max-age=10"},
			"Content-Security-Policy": {"default-src 'self'; script-src 'self'; connect-src 'self'"},
			"Set-Cookie":              {"one=1; HttpOnly", "two=2; Secure"},
			"X-Application":           {"kept"},
			"ETag":                    {`"old"`},
			"Age":                     {"20"},
			"Expires":                 {"Wed, 10 Sep 2025 10:01:00 GMT"},
			"Content-MD5":             {"old"},
			"Last-Modified":           {"Wed, 10 Sep 2025 10:00:00 GMT"},
			"Accept-Ranges":           {"bytes"},
			"Digest":                  {"sha-256=old"},
		},
	}
	if err := reload.ModifyResponse(response); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	wantTag := `<script src="` + reload.ScriptPath() + `" data-goforge-generation="0" defer></script>`
	if !strings.Contains(string(body), "hello"+wantTag+"</BODY >") {
		t.Fatalf("client was not inserted before closing body with pinned generation:\n%s", body)
	}
	if !original.closed {
		t.Error("original upstream body was not closed")
	}
	if response.Status != "200 Wonderful" || response.StatusCode != http.StatusOK || response.Proto != "HTTP/1.1" {
		t.Errorf("response status/protocol changed: %#v", response)
	}
	if response.ContentLength != int64(len(body)) || response.Header.Get("Content-Length") != strconv.Itoa(len(body)) || response.TransferEncoding != nil {
		t.Errorf("rewritten length/framing is invalid: field=%d header=%q transfer=%v", response.ContentLength, response.Header.Get("Content-Length"), response.TransferEncoding)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", response.Header.Get("Cache-Control"))
	}
	if got := response.Header.Values("Set-Cookie"); len(got) != 2 || got[0] != "one=1; HttpOnly" || got[1] != "two=2; Secure" {
		t.Errorf("cookies changed: %v", got)
	}
	if response.Header.Get("Content-Security-Policy") != "default-src 'self'; script-src 'self'; connect-src 'self'" ||
		response.Header.Get("X-Application") != "kept" || response.Header.Get("Content-Type") != "text/html; charset=utf-8" ||
		response.Header.Get("Content-Disposition") != "inline; filename=page.html" || response.Header.Get("Content-Encoding") != "identity" {
		t.Errorf("application headers changed: %v", response.Header)
	}
	for _, removed := range []string{"ETag", "Last-Modified", "Accept-Ranges", "Content-Range", "Digest", "Age", "Expires", "Content-MD5"} {
		if response.Header.Get(removed) != "" {
			t.Errorf("invalidated header %s remains %q", removed, response.Header.Get(removed))
		}
	}
}

func TestServeLiveReloadInjectsBeforeTerminalHTMLWhenBodyCloseIsOmitted(t *testing.T) {
	reload := newTestServeLiveReload(t)
	response := testServeLiveReloadResponse("<html><main>hello</main></HTML \t>")
	request := testServeLiveReloadDocumentRequest(http.MethodGet, "/")
	if err := reload.Inject(request, response); err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), `</main><script src="`+reload.ScriptPath()) || !strings.HasSuffix(string(body), "</HTML \t>") {
		t.Fatalf("HTML fallback injection failed: %s", body)
	}
}

func TestServeLiveReloadMetadataExclusionsAreNeverReadOrChanged(t *testing.T) {
	reload := newTestServeLiveReload(t)
	tests := []struct {
		name   string
		method string
		mutate func(*http.Request, *http.Response)
	}{
		{name: "HEAD", method: http.MethodHead},
		{name: "POST", method: http.MethodPost},
		{name: "non-200", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) { response.StatusCode = http.StatusCreated }},
		{name: "partial status", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) { response.StatusCode = http.StatusPartialContent }},
		{name: "JSON", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.Header.Set("Content-Type", "application/json")
		}},
		{name: "XHTML", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.Header.Set("Content-Type", "application/xhtml+xml")
		}},
		{name: "malformed type", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.Header.Set("Content-Type", `text/html; charset="`)
		}},
		{name: "duplicate type", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.Header["Content-Type"] = []string{"text/html", "text/html"}
		}},
		{name: "gzip", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) { response.Header.Set("Content-Encoding", "gzip") }},
		{name: "multiple encodings", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.Header["Content-Encoding"] = []string{"identity", "identity"}
		}},
		{name: "auto decompressed", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) { response.Uncompressed = true }},
		{name: "attachment", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.Header.Set("Content-Disposition", `attachment; filename="page.html"`)
		}},
		{name: "malformed disposition", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.Header.Set("Content-Disposition", `inline; filename="`)
		}},
		{name: "range request", method: http.MethodGet, mutate: func(request *http.Request, _ *http.Response) { request.Header.Set("Range", "bytes=0-10") }},
		{name: "range response", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) { response.Header.Set("Content-Range", "bytes 0-10/20") }},
		{name: "missing destination", method: http.MethodGet, mutate: func(request *http.Request, _ *http.Response) { request.Header.Del("Sec-Fetch-Dest") }},
		{name: "non-document", method: http.MethodGet, mutate: func(request *http.Request, _ *http.Response) { request.Header.Set("Sec-Fetch-Dest", "empty") }},
		{name: "unknown length", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.ContentLength = -1
			response.Header.Del("Content-Length")
		}},
		{name: "chunked stream", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.ContentLength = -1
			response.Header.Del("Content-Length")
			response.TransferEncoding = []string{"chunked"}
		}},
		{name: "trailers", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.Trailer = http.Header{"Digest": {"sha-256=stale"}}
		}},
		{name: "response no-transform", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.Header["Cache-Control"] = []string{"private", "max-age=0, No-Transform"}
		}},
		{name: "request no-transform", method: http.MethodGet, mutate: func(request *http.Request, _ *http.Response) {
			request.Header.Set("Cache-Control", "no-cache, no-transform")
		}},
		{name: "non-UTF-8", method: http.MethodGet, mutate: func(_ *http.Request, response *http.Response) {
			response.Header.Set("Content-Type", "text/html; charset=iso-8859-1")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := testServeLiveReloadDocumentRequest(test.method, "/")
			spy := &trackingReadCloser{Reader: strings.NewReader("<html><body>unchanged</body></html>")}
			response := testServeLiveReloadResponseWithBody(spy)
			if test.mutate != nil {
				test.mutate(request, response)
			}
			headers := response.Header.Clone()
			length := response.ContentLength
			if err := reload.Inject(request, response); err != nil {
				t.Fatal(err)
			}
			if spy.reads != 0 || spy.closed {
				t.Fatalf("excluded body was touched: reads=%d closed=%v", spy.reads, spy.closed)
			}
			if !equalServeLiveReloadHeaders(response.Header, headers) || response.ContentLength != length || response.Body != spy {
				t.Fatalf("excluded response changed: headers=%v body=%T length=%d", response.Header, response.Body, response.ContentLength)
			}
		})
	}
}

func TestServeLiveReloadFragmentAndOversizeAreReconstructedExactly(t *testing.T) {
	reload := newTestServeLiveReload(t)
	fragment := `<main>literal "</body>" is not a terminal document close</main>`
	for _, body := range []string{
		fragment,
		"<script>var closing = '</body>';</script>",
		strings.Repeat("x", serveLiveReloadMaxDocumentBytes+1) + "tail",
	} {
		response := testServeLiveReloadResponse(body)
		headers := response.Header.Clone()
		length := response.ContentLength
		if err := reload.Inject(testServeLiveReloadDocumentRequest(http.MethodGet, "/"), response); err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body || !equalServeLiveReloadHeaders(response.Header, headers) || response.ContentLength != length {
			t.Fatalf("non-document changed: got length=%d want=%d headers=%v", len(got), len(body), response.Header)
		}
	}

	exact := strings.Repeat("x", serveLiveReloadMaxDocumentBytes-len("</html>")) + "</html>"
	response := testServeLiveReloadResponse(exact)
	if err := reload.Inject(testServeLiveReloadDocumentRequest(http.MethodGet, "/"), response); err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(response.Body)
	if len(got) <= serveLiveReloadMaxDocumentBytes || !strings.Contains(string(got), reload.ScriptPath()) {
		t.Fatalf("exactly bounded document was not injected: length=%d", len(got))
	}
}

func TestServeLiveReloadDoesNotInjectDuplicateClient(t *testing.T) {
	reload := newTestServeLiveReload(t)
	body := `<html><body>kept<script data-goforge-generation="7"></script></body></html>`
	response := testServeLiveReloadResponse(body)
	headers := response.Header.Clone()
	if err := reload.Inject(testServeLiveReloadDocumentRequest(http.MethodGet, "/"), response); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body || !equalServeLiveReloadHeaders(response.Header, headers) {
		t.Fatalf("duplicate client response changed: body=%q headers=%v", got, response.Header)
	}
}

func TestServeLiveReloadReadAndCloseErrorsLeaveSafeOriginalRepresentation(t *testing.T) {
	reload := newTestServeLiveReload(t)
	readFailure := errors.New("read failure")
	staged := &stagedErrorBody{first: []byte("<html><bo"), rest: []byte("dy>safe</body></html>"), err: readFailure}
	response := testServeLiveReloadResponseWithBody(staged)
	headers := response.Header.Clone()
	if err := reload.Inject(testServeLiveReloadDocumentRequest(http.MethodGet, "/"), response); !errors.Is(err, readFailure) {
		t.Fatalf("read error = %v, want %v", err, readFailure)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), "<html><body>safe</body></html>"; got != want || !equalServeLiveReloadHeaders(response.Header, headers) {
		t.Fatalf("reconstructed read failure = %q headers=%v", got, response.Header)
	}
	_ = response.Body.Close()
	if !staged.closed {
		t.Error("reconstructed body did not forward Close")
	}

	closeFailure := errors.New("close failure")
	closing := &trackingReadCloser{Reader: strings.NewReader("<html><body>safe</body></html>"), closeErr: closeFailure}
	response = testServeLiveReloadResponseWithBody(closing)
	headers = response.Header.Clone()
	if err := reload.Inject(testServeLiveReloadDocumentRequest(http.MethodGet, "/"), response); err != nil {
		t.Fatalf("close error turned a complete response into a proxy error: %v", err)
	}
	body, err = io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(body), reload.ScriptPath()) || equalServeLiveReloadHeaders(response.Header, headers) {
		t.Fatalf("complete response was not safely injected after close failure: body=%q err=%v headers=%v", body, err, response.Header)
	}
}

func newTestServeLiveReload(t *testing.T) *serveLiveReload {
	t.Helper()
	reload, err := newServeLiveReloadWithReader(bytes.NewReader(bytes.Repeat([]byte{0x5a}, serveLiveReloadEntropyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reload.Close)
	return reload
}

func stampedServeLiveReloadRequest(t *testing.T, reload *serveLiveReload, method, target string) *http.Request {
	t.Helper()
	var result *http.Request
	request := testServeLiveReloadDocumentRequest(method, target)
	reload.Handler(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		result = request
	})).ServeHTTP(httptest.NewRecorder(), request)
	if result == nil {
		t.Fatal("application request was not forwarded")
	}
	return result
}

func testServeLiveReloadDocumentRequest(method, target string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Sec-Fetch-Dest", "document")
	return request
}

func testServeLiveReloadSubscriberCount(reload *serveLiveReload) int {
	reload.mu.Lock()
	defer reload.mu.Unlock()
	return len(reload.subscribers)
}

func waitForServeLiveReloadSubscribers(t *testing.T, reload *serveLiveReload, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if testServeLiveReloadSubscriberCount(reload) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("subscriber count = %d, want %d", testServeLiveReloadSubscriberCount(reload), want)
}

type trackingReadCloser struct {
	io.Reader
	reads    int
	closed   bool
	closeErr error
}

func (body *trackingReadCloser) Read(target []byte) (int, error) {
	body.reads++
	return body.Reader.Read(target)
}

func (body *trackingReadCloser) Close() error {
	body.closed = true
	return body.closeErr
}

type stagedErrorBody struct {
	first  []byte
	rest   []byte
	err    error
	failed bool
	closed bool
}

func (body *stagedErrorBody) Read(target []byte) (int, error) {
	if !body.failed {
		body.failed = true
		n := copy(target, body.first)
		return n, body.err
	}
	if len(body.rest) == 0 {
		return 0, io.EOF
	}
	n := copy(target, body.rest)
	body.rest = body.rest[n:]
	return n, nil
}

func (body *stagedErrorBody) Close() error {
	body.closed = true
	return nil
}

func testServeLiveReloadResponse(body string) *http.Response {
	return testServeLiveReloadResponseWithBody(io.NopCloser(strings.NewReader(body)))
}

func testServeLiveReloadResponseWithBody(body io.ReadCloser) *http.Response {
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Body:          body,
		ContentLength: 35,
		Header: http.Header{
			"Content-Type":   {"text/html; charset=utf-8"},
			"Content-Length": {"35"},
			"X-Unrelated":    {"preserved"},
		},
	}
}

func equalServeLiveReloadHeaders(left, right http.Header) bool {
	if len(left) != len(right) {
		return false
	}
	for name, leftValues := range left {
		rightValues, ok := right[name]
		if !ok || len(leftValues) != len(rightValues) {
			return false
		}
		for index := range leftValues {
			if leftValues[index] != rightValues[index] {
				return false
			}
		}
	}
	return true
}
