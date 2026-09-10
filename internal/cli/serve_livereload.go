package cli

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	serveLiveReloadEntropyBytes      = 32
	serveLiveReloadMaxDocumentBytes  = 1 << 20
	serveLiveReloadMaxSubscribers    = 64
	serveLiveReloadHeartbeatInterval = 15 * time.Second
	serveLiveReloadWriteTimeout      = time.Second
)

type serveLiveReloadGenerationKey struct{}

// serveLiveReload owns the development proxy's process-local browser client.
// Nothing in the generated application or production runtime depends on it.
type serveLiveReload struct {
	scriptPath string
	eventsPath string
	script     []byte

	heartbeatInterval time.Duration
	maxSubscribers    int

	mu          sync.Mutex
	generation  uint64
	subscribers map[chan uint64]struct{}
	closed      bool
	done        chan struct{}
	closeOnce   sync.Once
}

// newServeLiveReload creates unpredictable endpoints from crypto/rand.
func newServeLiveReload() (*serveLiveReload, error) {
	return newServeLiveReloadWithReader(cryptorand.Reader)
}

// newServeLiveReloadWithReader is the deterministic entropy seam used by
// tests. Production callers use newServeLiveReload.
func newServeLiveReloadWithReader(random io.Reader) (*serveLiveReload, error) {
	if random == nil {
		return nil, errors.New("live reload entropy reader is required")
	}
	entropy := make([]byte, serveLiveReloadEntropyBytes)
	if _, err := io.ReadFull(random, entropy); err != nil {
		return nil, fmt.Errorf("read live reload endpoint entropy: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(entropy)
	basePath := "/.goforge/livereload/" + token
	result := &serveLiveReload{
		scriptPath:        basePath + "/client.js",
		eventsPath:        basePath + "/events",
		heartbeatInterval: serveLiveReloadHeartbeatInterval,
		maxSubscribers:    serveLiveReloadMaxSubscribers,
		subscribers:       make(map[chan uint64]struct{}),
		done:              make(chan struct{}),
	}
	result.script = []byte(fmt.Sprintf(`(function () {
  "use strict";
  var script = document.currentScript;
  if (!script) return;
  var source = new EventSource(%q + "?since=" + encodeURIComponent(script.dataset.goforgeGeneration));
  var reloading = false;
  source.addEventListener("reload", function () {
    if (reloading) return;
    reloading = true;
    source.close();
    window.location.reload();
  });
}());
`, result.eventsPath))
	return result, nil
}

func (reload *serveLiveReload) ScriptPath() string {
	return reload.scriptPath
}

func (reload *serveLiveReload) EventsPath() string {
	return reload.eventsPath
}

// Generation returns the generation that a page served now should observe.
func (reload *serveLiveReload) Generation() uint64 {
	reload.mu.Lock()
	defer reload.mu.Unlock()
	return reload.generation
}

// NotifyReload advances the one process-local generation and wakes all current
// subscribers. A subscriber needs only one notification because the browser
// performs a full navigation after receiving it.
func (reload *serveLiveReload) NotifyReload() {
	reload.mu.Lock()
	defer reload.mu.Unlock()
	if reload.closed {
		return
	}
	if reload.generation < math.MaxUint64 {
		reload.generation++
	}
	for subscriber := range reload.subscribers {
		select {
		case subscriber <- reload.generation:
		default:
		}
	}
}

// Close terminates every current subscription and prevents new ones. It is
// idempotent; notification after shutdown is intentionally a no-op.
func (reload *serveLiveReload) Close() {
	reload.closeOnce.Do(func() {
		reload.mu.Lock()
		reload.closed = true
		close(reload.done)
		reload.mu.Unlock()
	})
}

// Handler wraps the application proxy. It snapshots the page generation
// before forwarding so a response from a target pinned before promotion keeps
// the older generation and will immediately recover through SSE.
func (reload *serveLiveReload) Handler(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if reload.Handle(response, request) {
			return
		}
		generation := reload.Generation()
		ctx := context.WithValue(request.Context(), serveLiveReloadGenerationKey{}, generation)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

// Handle serves only the two exact, unescaped process-local paths. It returns
// false for every application route so the caller can forward it unchanged.
func (reload *serveLiveReload) Handle(response http.ResponseWriter, request *http.Request) bool {
	if request == nil || request.URL == nil {
		return false
	}
	switch {
	case exactServeLiveReloadPath(request.URL, reload.scriptPath):
		reload.serveScript(response, request)
		return true
	case exactServeLiveReloadPath(request.URL, reload.eventsPath):
		reload.serveEvents(response, request)
		return true
	default:
		return false
	}
}

func exactServeLiveReloadPath(target *url.URL, expected string) bool {
	return target.Path == expected && target.EscapedPath() == expected
}

func (reload *serveLiveReload) serveScript(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	response.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	response.Header().Set("Content-Length", strconv.Itoa(len(reload.script)))
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(reload.script)
}

func (reload *serveLiveReload) serveEvents(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	since, err := parseServeLiveReloadSince(request.URL.RawQuery)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	if _, ok := response.(http.Flusher); !ok {
		http.Error(response, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	subscriber, current, subscribeStatus := reload.subscribe(since)
	if subscribeStatus != http.StatusOK {
		if subscribeStatus == http.StatusServiceUnavailable {
			response.Header().Set("Retry-After", "1")
		}
		http.Error(response, http.StatusText(subscribeStatus), subscribeStatus)
		return
	}
	if subscriber != nil {
		defer reload.unsubscribe(subscriber)
	}

	response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	if subscriber == nil {
		flushServeLiveReload(response, func(writer io.Writer) error {
			return writeServeLiveReloadEvent(writer, current)
		})
		return
	}
	if !flushServeLiveReload(response, func(writer io.Writer) error {
		_, err := io.WriteString(writer, ": connected\n\n")
		return err
	}) {
		return
	}

	heartbeat := time.NewTicker(reload.heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case generation := <-subscriber:
			flushServeLiveReload(response, func(writer io.Writer) error {
				return writeServeLiveReloadEvent(writer, generation)
			})
			return
		case <-heartbeat.C:
			if !flushServeLiveReload(response, func(writer io.Writer) error {
				_, err := io.WriteString(writer, ": heartbeat\n\n")
				return err
			}) {
				return
			}
		case <-request.Context().Done():
			return
		case <-reload.done:
			return
		}
	}
}

func parseServeLiveReloadSince(rawQuery string) (uint64, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return 0, errors.New("invalid live reload generation")
	}
	values, present := query["since"]
	if !present || len(query) != 1 || len(values) != 1 || values[0] == "" {
		return 0, errors.New("exactly one live reload generation is required")
	}
	for _, character := range values[0] {
		if character < '0' || character > '9' {
			return 0, errors.New("invalid live reload generation")
		}
	}
	generation, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil {
		return 0, errors.New("invalid live reload generation")
	}
	return generation, nil
}

// subscribe returns a nil subscriber when the requested generation has already
// been missed. The caller then emits an immediate reload without consuming a
// subscriber slot.
func (reload *serveLiveReload) subscribe(since uint64) (chan uint64, uint64, int) {
	reload.mu.Lock()
	defer reload.mu.Unlock()
	if reload.closed {
		return nil, reload.generation, http.StatusServiceUnavailable
	}
	if since > reload.generation {
		return nil, reload.generation, http.StatusBadRequest
	}
	if since < reload.generation {
		return nil, reload.generation, http.StatusOK
	}
	if len(reload.subscribers) >= reload.maxSubscribers {
		return nil, reload.generation, http.StatusServiceUnavailable
	}
	subscriber := make(chan uint64, 1)
	reload.subscribers[subscriber] = struct{}{}
	return subscriber, reload.generation, http.StatusOK
}

func (reload *serveLiveReload) unsubscribe(subscriber chan uint64) {
	reload.mu.Lock()
	delete(reload.subscribers, subscriber)
	reload.mu.Unlock()
}

func writeServeLiveReloadEvent(response io.Writer, generation uint64) error {
	_, err := fmt.Fprintf(response, "id: %d\nevent: reload\ndata: %d\n\n", generation, generation)
	return err
}

func flushServeLiveReload(response http.ResponseWriter, write func(io.Writer) error) bool {
	controller := http.NewResponseController(response)
	if err := controller.SetWriteDeadline(time.Now().Add(serveLiveReloadWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return false
	}
	defer func() {
		// Deadlines are connection state. Clear ours after every bounded write so
		// idle SSE time and a later keep-alive response remain unaffected.
		_ = controller.SetWriteDeadline(time.Time{})
	}()
	if err := write(response); err != nil {
		return false
	}
	return controller.Flush() == nil
}

// ModifyResponse is the direct httputil.ReverseProxy hook.
func (reload *serveLiveReload) ModifyResponse(response *http.Response) error {
	if response == nil {
		return nil
	}
	return reload.Inject(response.Request, response)
}

// Inject adds the external client to a narrowly eligible browser document.
// Every metadata exclusion is checked before reading Body. Once reading starts,
// all non-rewrite and error paths rebuild the consumed stream.
func (reload *serveLiveReload) Inject(request *http.Request, response *http.Response) error {
	if !eligibleServeLiveReloadResponse(request, response) {
		return nil
	}

	original := response.Body
	body, err := io.ReadAll(io.LimitReader(original, serveLiveReloadMaxDocumentBytes+1))
	if err != nil {
		response.Body = prependServeLiveReloadBody(body, original)
		return fmt.Errorf("read HTML response for live reload: %w", err)
	}
	if len(body) > serveLiveReloadMaxDocumentBytes {
		response.Body = prependServeLiveReloadBody(body, original)
		return nil
	}
	if bytes.Contains(body, []byte(`<script src="`+reload.scriptPath+`"`)) {
		response.Body = prependServeLiveReloadBody(body, original)
		return nil
	}
	closing := findServeLiveReloadClosingTag(body)
	if closing < 0 {
		response.Body = prependServeLiveReloadBody(body, original)
		return nil
	}

	generation := reload.Generation()
	if requestGeneration, ok := request.Context().Value(serveLiveReloadGenerationKey{}).(uint64); ok {
		generation = requestGeneration
	}
	tag := []byte(fmt.Sprintf(`<script src="%s" data-goforge-generation="%d" defer></script>`, reload.scriptPath, generation))
	injected := make([]byte, 0, len(body)+len(tag))
	injected = append(injected, body[:closing]...)
	injected = append(injected, tag...)
	injected = append(injected, body[closing:]...)
	// EOF has already yielded the complete representation. A transport Close
	// error must not turn that valid upstream response into ReverseProxy's 502.
	_ = original.Close()

	response.Body = io.NopCloser(bytes.NewReader(injected))
	response.ContentLength = int64(len(injected))
	response.TransferEncoding = nil
	response.Header.Set("Cache-Control", "no-store")
	response.Header.Set("Content-Length", strconv.Itoa(len(injected)))
	for _, name := range []string{
		"Accept-Ranges",
		"Age",
		"Content-Digest",
		"Content-MD5",
		"Content-Range",
		"Digest",
		"ETag",
		"Expires",
		"Last-Modified",
		"Repr-Digest",
	} {
		response.Header.Del(name)
	}
	return nil
}

func eligibleServeLiveReloadResponse(request *http.Request, response *http.Response) bool {
	if request == nil || response == nil || request.Method != http.MethodGet ||
		response.StatusCode != http.StatusOK || response.Body == nil || response.Uncompressed ||
		response.ContentLength < 0 || response.ContentLength > serveLiveReloadMaxDocumentBytes ||
		len(response.TransferEncoding) != 0 || len(response.Trailer) != 0 {
		return false
	}
	if destinations := request.Header.Values("Sec-Fetch-Dest"); len(destinations) != 1 || !strings.EqualFold(strings.TrimSpace(destinations[0]), "document") {
		return false
	}
	if request.Header.Get("Range") != "" || response.Header.Get("Content-Range") != "" {
		return false
	}
	if request.Header.Get("Upgrade") != "" || response.Header.Get("Upgrade") != "" ||
		headerHasServeLiveReloadDirective(request.Header, "Cache-Control", "no-transform") ||
		headerHasServeLiveReloadDirective(response.Header, "Cache-Control", "no-transform") {
		return false
	}
	contentTypes := response.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return false
	}
	mediaType, parameters, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || !strings.EqualFold(mediaType, "text/html") {
		return false
	}
	if charset, present := parameters["charset"]; present && !strings.EqualFold(strings.TrimSpace(charset), "utf-8") {
		return false
	}
	encodings := response.Header.Values("Content-Encoding")
	if len(encodings) > 1 || (len(encodings) == 1 && !strings.EqualFold(strings.TrimSpace(encodings[0]), "identity")) {
		return false
	}
	dispositions := response.Header.Values("Content-Disposition")
	if len(dispositions) > 1 {
		return false
	}
	if len(dispositions) == 1 && strings.TrimSpace(dispositions[0]) != "" {
		disposition, _, err := mime.ParseMediaType(dispositions[0])
		if err != nil || strings.EqualFold(disposition, "attachment") {
			return false
		}
	}
	return true
}

func headerHasServeLiveReloadDirective(header http.Header, name, directive string) bool {
	for _, value := range header.Values(name) {
		for _, item := range strings.Split(value, ",") {
			token := strings.TrimSpace(strings.SplitN(item, "=", 2)[0])
			if strings.EqualFold(token, directive) {
				return true
			}
		}
	}
	return false
}

func findServeLiveReloadClosingTag(body []byte) int {
	lower := bytes.ToLower(body)
	if start, end := lastServeLiveReloadClosingTag(lower, []byte("body")); start >= 0 {
		tail := bytes.TrimSpace(lower[end:])
		if len(tail) == 0 {
			return start
		}
		if htmlStart, htmlEnd := firstServeLiveReloadClosingTag(tail, []byte("html")); htmlStart == 0 && htmlEnd >= 0 && len(bytes.TrimSpace(tail[htmlEnd:])) == 0 {
			return start
		}
	}
	if start, end := lastServeLiveReloadClosingTag(lower, []byte("html")); start >= 0 && len(bytes.TrimSpace(lower[end:])) == 0 {
		return start
	}
	return -1
}

func lastServeLiveReloadClosingTag(body, name []byte) (int, int) {
	prefix := append([]byte("</"), name...)
	for searchEnd := len(body); searchEnd > 0; {
		start := bytes.LastIndex(body[:searchEnd], prefix)
		if start < 0 {
			return -1, -1
		}
		if end := serveLiveReloadClosingTagEnd(body, start+len(prefix)); end >= 0 {
			return start, end
		}
		searchEnd = start
	}
	return -1, -1
}

func firstServeLiveReloadClosingTag(body, name []byte) (int, int) {
	prefix := append([]byte("</"), name...)
	start := bytes.Index(body, prefix)
	if start < 0 {
		return -1, -1
	}
	return start, serveLiveReloadClosingTagEnd(body, start+len(prefix))
}

func serveLiveReloadClosingTagEnd(body []byte, position int) int {
	for position < len(body) {
		switch body[position] {
		case ' ', '\t', '\r', '\n', '\f':
			position++
		case '>':
			return position + 1
		default:
			return -1
		}
	}
	return -1
}

type serveLiveReloadPrependedBody struct {
	io.Reader
	closer io.Closer
}

func (body *serveLiveReloadPrependedBody) Close() error {
	return body.closer.Close()
}

func prependServeLiveReloadBody(prefix []byte, remainder io.ReadCloser) io.ReadCloser {
	return &serveLiveReloadPrependedBody{
		Reader: io.MultiReader(bytes.NewReader(prefix), remainder),
		closer: remainder,
	}
}
