package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	netmail "net/mail"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recoveryAcceptanceResponse struct {
	Status int
	Body   string
	Header http.Header
}

func recoveryAcceptanceJSON(client *http.Client, method, target, body, source string) (recoveryAcceptanceResponse, error) {
	request, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		return recoveryAcceptanceResponse{}, err
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recoveryAcceptanceHostileHeaders(request, source)
	return recoveryAcceptanceDo(client, request)
}

func recoveryAcceptanceBrowser(client *http.Client, method, target string, values url.Values, source string) (recoveryAcceptanceResponse, error) {
	var body io.Reader
	if values != nil {
		body = strings.NewReader(values.Encode())
	}
	request, err := http.NewRequest(method, target, body)
	if err != nil {
		return recoveryAcceptanceResponse{}, err
	}
	if values != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	recoveryAcceptanceHostileHeaders(request, source)
	return recoveryAcceptanceDo(client, request)
}

func recoveryAcceptanceHostileHeaders(request *http.Request, source string) {
	request.Host = "host-header.attacker.invalid"
	request.Header.Set("Forwarded", "host=forwarded.attacker.invalid;proto=https")
	request.Header.Set("X-Forwarded-Host", "x-forwarded.attacker.invalid")
	request.Header.Set("X-Forwarded-Proto", "https")
	if source != "" {
		request.Header.Set("X-Forwarded-For", source)
	}
}

func recoveryAcceptanceDo(client *http.Client, request *http.Request) (recoveryAcceptanceResponse, error) {
	response, err := client.Do(request)
	if err != nil {
		return recoveryAcceptanceResponse{}, err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		return recoveryAcceptanceResponse{}, err
	}
	return recoveryAcceptanceResponse{Status: response.StatusCode, Body: string(contents), Header: response.Header.Clone()}, nil
}

func recoveryAcceptanceMustJSON(t *testing.T, client *http.Client, method, target, body, source string) recoveryAcceptanceResponse {
	t.Helper()
	response, err := recoveryAcceptanceJSON(client, method, target, body, source)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func recoveryAcceptanceMustBrowser(t *testing.T, client *http.Client, method, target string, values url.Values, source string) recoveryAcceptanceResponse {
	t.Helper()
	response, err := recoveryAcceptanceBrowser(client, method, target, values, source)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func recoveryAcceptanceAssertEquivalent(t *testing.T, known, unknown recoveryAcceptanceResponse, headers ...string) {
	t.Helper()
	if known.Status != unknown.Status || known.Body != unknown.Body {
		t.Fatalf("known and unknown recovery responses differ: known=%d %q unknown=%d %q", known.Status, known.Body, unknown.Status, unknown.Body)
	}
	for _, name := range headers {
		if known.Header.Get(name) != unknown.Header.Get(name) {
			t.Fatalf("known and unknown recovery %s headers differ: known=%q unknown=%q", name, known.Header.Get(name), unknown.Header.Get(name))
		}
	}
}

func recoveryAcceptanceAssertPrivateHeaders(t *testing.T, response recoveryAcceptanceResponse) {
	t.Helper()
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Referrer-Policy") != "no-referrer" || response.Header.Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("password recovery response omitted private headers: cache=%q referrer=%q robots=%q", response.Header.Get("Cache-Control"), response.Header.Get("Referrer-Policy"), response.Header.Get("X-Robots-Tag"))
	}
}

type recoverySMTPMessage struct {
	EnvelopeFrom string
	Recipients   []string
	Raw          []byte
	Text         string
	HTML         string
}

type recoverySMTPServer struct {
	listener net.Listener
	accepted chan recoverySMTPMessage
	rejected chan recoverySMTPMessage
	stop     chan struct{}
	wait     sync.WaitGroup
	reject   atomic.Bool
}

func startRecoverySMTPServer(t *testing.T, rejectFirst bool) *recoverySMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &recoverySMTPServer{
		listener: listener,
		accepted: make(chan recoverySMTPMessage, 16),
		rejected: make(chan recoverySMTPMessage, 4),
		stop:     make(chan struct{}),
	}
	server.reject.Store(rejectFirst)
	server.wait.Add(1)
	go server.serve()
	t.Cleanup(server.close)
	return server
}

func (server *recoverySMTPServer) address() string { return server.listener.Addr().String() }

func (server *recoverySMTPServer) close() {
	select {
	case <-server.stop:
		return
	default:
		close(server.stop)
		_ = server.listener.Close()
		server.wait.Wait()
	}
}

func (server *recoverySMTPServer) serve() {
	defer server.wait.Done()
	for {
		connection, err := server.listener.Accept()
		if err != nil {
			select {
			case <-server.stop:
				return
			default:
				continue
			}
		}
		server.wait.Add(1)
		go func() {
			defer server.wait.Done()
			server.handle(connection)
		}()
	}
}

func (server *recoverySMTPServer) handle(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	write := func(line string) bool {
		if _, err := writer.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if !write("220 localhost GoForge acceptance SMTP") {
		return
	}
	var envelopeFrom string
	var recipients []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		command, argument, _ := strings.Cut(line, " ")
		switch strings.ToUpper(command) {
		case "EHLO":
			if _, err := writer.WriteString("250-localhost\r\n250 PIPELINING\r\n"); err != nil || writer.Flush() != nil {
				return
			}
		case "HELO":
			if !write("250 localhost") {
				return
			}
		case "MAIL":
			envelopeFrom = recoverySMTPPath(argument)
			if !write("250 sender accepted") {
				return
			}
		case "RCPT":
			recipients = append(recipients, recoverySMTPPath(argument))
			if !write("250 recipient accepted") {
				return
			}
		case "DATA":
			if !write("354 end with <CRLF>.<CRLF>") {
				return
			}
			raw, err := textproto.NewReader(reader).ReadDotBytes()
			if err != nil {
				return
			}
			message, err := recoverySMTPDecode(envelopeFrom, recipients, raw)
			if err != nil {
				_ = write("554 malformed message")
				return
			}
			if server.reject.CompareAndSwap(true, false) {
				server.rejected <- message
				_ = write("451 temporary acceptance failure")
				return
			}
			server.accepted <- message
			if !write("250 message accepted") {
				return
			}
		case "RSET":
			envelopeFrom = ""
			recipients = nil
			if !write("250 reset") {
				return
			}
		case "NOOP":
			if !write("250 ok") {
				return
			}
		case "QUIT":
			_ = write("221 closing connection")
			return
		default:
			if !write("502 command not implemented") {
				return
			}
		}
	}
}

func recoverySMTPPath(argument string) string {
	_, value, found := strings.Cut(argument, ":")
	if !found {
		return ""
	}
	value = strings.TrimSpace(value)
	if option := strings.IndexByte(value, ' '); option >= 0 {
		value = value[:option]
	}
	return strings.Trim(value, "<>")
}

func recoverySMTPDecode(from string, recipients []string, raw []byte) (recoverySMTPMessage, error) {
	message := recoverySMTPMessage{EnvelopeFrom: from, Recipients: append([]string(nil), recipients...), Raw: append([]byte(nil), raw...)}
	parsed, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return recoverySMTPMessage{}, err
	}
	mediaType, parameters, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		return recoverySMTPMessage{}, err
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		contents, readErr := recoverySMTPPart(parsed.Header.Get("Content-Transfer-Encoding"), parsed.Body)
		message.Text = string(contents)
		return message, readErr
	}
	parts := multipart.NewReader(parsed.Body, parameters["boundary"])
	for {
		part, nextErr := parts.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return recoverySMTPMessage{}, nextErr
		}
		contents, readErr := recoverySMTPPart(part.Header.Get("Content-Transfer-Encoding"), part)
		_ = part.Close()
		if readErr != nil {
			return recoverySMTPMessage{}, readErr
		}
		partType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		switch partType {
		case "text/plain":
			message.Text = string(contents)
		case "text/html":
			message.HTML = string(contents)
		}
	}
	if message.Text == "" || message.HTML == "" {
		return recoverySMTPMessage{}, fmt.Errorf("SMTP message lacks text/plain or text/html alternative")
	}
	return message, nil
}

func recoverySMTPPart(encoding string, source io.Reader) ([]byte, error) {
	if strings.EqualFold(encoding, "quoted-printable") {
		source = quotedprintable.NewReader(source)
	}
	return io.ReadAll(source)
}

func recoverySMTPWait(t *testing.T, messages <-chan recoverySMTPMessage, timeout time.Duration) recoverySMTPMessage {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-time.After(timeout):
		t.Fatalf("timed out waiting %s for SMTP message", timeout)
		return recoverySMTPMessage{}
	}
}
