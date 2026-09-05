package httpx

import (
	"bufio"
	"net"
	"net/http"
)

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (writer *statusWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		writer.status = status
		writer.ResponseWriter.WriteHeader(status)
		return
	}
	writer.ResponseWriter.WriteHeader(status)
	writer.status = status
	writer.wroteHeader = true
}

func (writer *statusWriter) Write(body []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(body)
}

func (writer *statusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// Written reports whether a final response header has been committed. It lets
// components such as the session manager reject header mutations that would
// otherwise be silently ignored by net/http.
func (writer *statusWriter) Written() bool { return writer.wroteHeader }

// newStatusWriter preserves the optional HTTP interfaces most commonly used by
// SSE, WebSocket, and HTTP/2 handlers. A wrapper must not claim an interface the
// underlying writer lacks (notably Flusher, whose method cannot return an
// unsupported error), so the concrete wrapper is selected from the capability
// set at runtime. ResponseController also remains compatible through Unwrap.
func newStatusWriter(response http.ResponseWriter) (*statusWriter, http.ResponseWriter) {
	writer := &statusWriter{ResponseWriter: response, status: http.StatusOK}
	_, flushes := response.(http.Flusher)
	_, hijacks := response.(http.Hijacker)
	_, pushes := response.(http.Pusher)
	switch {
	case flushes && hijacks && pushes:
		return writer, &statusFlushHijackPushWriter{statusWriter: writer}
	case flushes && hijacks:
		return writer, &statusFlushHijackWriter{statusWriter: writer}
	case flushes && pushes:
		return writer, &statusFlushPushWriter{statusWriter: writer}
	case hijacks && pushes:
		return writer, &statusHijackPushWriter{statusWriter: writer}
	case flushes:
		return writer, &statusFlushWriter{statusWriter: writer}
	case hijacks:
		return writer, &statusHijackWriter{statusWriter: writer}
	case pushes:
		return writer, &statusPushWriter{statusWriter: writer}
	default:
		return writer, writer
	}
}

type statusFlushWriter struct{ *statusWriter }
type statusHijackWriter struct{ *statusWriter }
type statusPushWriter struct{ *statusWriter }
type statusFlushHijackWriter struct{ *statusWriter }
type statusFlushPushWriter struct{ *statusWriter }
type statusHijackPushWriter struct{ *statusWriter }
type statusFlushHijackPushWriter struct{ *statusWriter }

func flush(writer *statusWriter) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	writer.ResponseWriter.(http.Flusher).Flush()
}

func hijack(writer *statusWriter) (net.Conn, *bufio.ReadWriter, error) {
	connection, buffer, err := writer.ResponseWriter.(http.Hijacker).Hijack()
	if err == nil && !writer.wroteHeader {
		writer.status = http.StatusSwitchingProtocols
		writer.wroteHeader = true
	}
	return connection, buffer, err
}

func push(writer *statusWriter, target string, options *http.PushOptions) error {
	return writer.ResponseWriter.(http.Pusher).Push(target, options)
}

func (writer *statusFlushWriter) Flush() { flush(writer.statusWriter) }
func (writer *statusHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return hijack(writer.statusWriter)
}
func (writer *statusPushWriter) Push(target string, options *http.PushOptions) error {
	return push(writer.statusWriter, target, options)
}
func (writer *statusFlushHijackWriter) Flush() { flush(writer.statusWriter) }
func (writer *statusFlushHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return hijack(writer.statusWriter)
}
func (writer *statusFlushPushWriter) Flush() { flush(writer.statusWriter) }
func (writer *statusFlushPushWriter) Push(target string, options *http.PushOptions) error {
	return push(writer.statusWriter, target, options)
}
func (writer *statusHijackPushWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return hijack(writer.statusWriter)
}
func (writer *statusHijackPushWriter) Push(target string, options *http.PushOptions) error {
	return push(writer.statusWriter, target, options)
}
func (writer *statusFlushHijackPushWriter) Flush() { flush(writer.statusWriter) }
func (writer *statusFlushHijackPushWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return hijack(writer.statusWriter)
}
func (writer *statusFlushHijackPushWriter) Push(target string, options *http.PushOptions) error {
	return push(writer.statusWriter, target, options)
}
