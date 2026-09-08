package smtp_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"

	forgemail "github.com/ShanilKoshitha/goforge/mail"
	forgesmtp "github.com/ShanilKoshitha/goforge/mail/smtp"
)

type serverOptions struct {
	implicitTLS   bool
	startTLS      bool
	auth          bool
	reject        string
	stallGreeting bool
}

type serverResult struct {
	events  []string
	message string
	err     error
}

func TestSenderRequiresSTARTTLSAndAuthenticatesOnlyAfterTLS(t *testing.T) {
	address, tlsConfig, result := startServer(t, serverOptions{startTLS: true, auth: true})
	sender, err := forgesmtp.New(forgesmtp.Config{
		Address: address, TLSConfig: tlsConfig, Username: "smtp-user", Password: "smtp-secret",
		LocalName: "app.test", DialTimeout: time.Second, OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	message := testMessage(t, []mail.Address{{Name: "Ada", Address: "ada@example.com"}, {Address: "grace@example.com"}})
	if err := sender.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	completed := awaitResult(t, result)
	if completed.err != nil {
		t.Fatal(completed.err)
	}
	events := strings.Join(completed.events, ",")
	if !strings.Contains(events, "starttls,hello:tls,auth:tls,mail:tls,recipient:tls,recipient:tls,data:tls") {
		t.Fatalf("SMTP events = %v", completed.events)
	}
	if !strings.Contains(completed.message, "Content-Type: multipart/alternative") ||
		!strings.Contains(completed.message, "Subject: SMTP delivery") {
		t.Fatalf("message = %s", completed.message)
	}
}

func TestSenderSupportsImplicitTLS(t *testing.T) {
	address, tlsConfig, result := startServer(t, serverOptions{implicitTLS: true})
	sender, err := forgesmtp.New(forgesmtp.Config{
		Address: address, TLSMode: forgesmtp.TLSImplicit, TLSConfig: tlsConfig,
		DialTimeout: time.Second, OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), testMessage(t, []mail.Address{{Address: "ada@example.com"}})); err != nil {
		t.Fatal(err)
	}
	completed := awaitResult(t, result)
	if completed.err != nil || !contains(completed.events, "hello:tls") || contains(completed.events, "starttls") {
		t.Fatalf("result = %#v", completed)
	}
}

func TestSenderRefusesTLSDowngradeAndPlaintextAuthentication(t *testing.T) {
	address, tlsConfig, result := startServer(t, serverOptions{})
	sender, err := forgesmtp.New(forgesmtp.Config{Address: address, TLSConfig: tlsConfig, OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), testMessage(t, []mail.Address{{Address: "private@example.com"}}))
	var delivery *forgesmtp.DeliveryError
	if !errors.Is(err, forgesmtp.ErrDelivery) || !errors.As(err, &delivery) || delivery.Stage() != "tls" {
		t.Fatalf("error = %#v", err)
	}
	if strings.Contains(err.Error(), "private@example.com") {
		t.Fatalf("delivery error disclosed a recipient: %v", err)
	}
	completed := awaitResult(t, result)
	if contains(completed.events, "mail:plain") || contains(completed.events, "data:plain") {
		t.Fatalf("message proceeded after TLS downgrade: %v", completed.events)
	}

	if _, err := forgesmtp.New(forgesmtp.Config{
		Address: "127.0.0.1:25", UnsafeAllowPlaintext: true, Username: "user", Password: "secret",
	}); !errors.Is(err, forgesmtp.ErrInvalidConfiguration) {
		t.Fatalf("plaintext AUTH configuration error = %v", err)
	}
	if _, err := forgesmtp.New(forgesmtp.Config{
		Address: "smtp.example.com:25", UnsafeAllowPlaintext: true,
	}); !errors.Is(err, forgesmtp.ErrInvalidConfiguration) {
		t.Fatalf("remote plaintext configuration error = %v", err)
	}
}

func TestSenderRejectsUntrustedTLSBeforeEnvelope(t *testing.T) {
	address, _, result := startServer(t, serverOptions{startTLS: true})
	sender, err := forgesmtp.New(forgesmtp.Config{Address: address, OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), testMessage(t, []mail.Address{{Address: "private@example.com"}}))
	var delivery *forgesmtp.DeliveryError
	if !errors.As(err, &delivery) || delivery.Stage() != "tls" {
		t.Fatalf("error = %#v", err)
	}
	completed := awaitResult(t, result)
	if containsPrefix(completed.events, "mail:") || containsPrefix(completed.events, "data:") {
		t.Fatalf("untrusted TLS reached the envelope: %v", completed.events)
	}
}

func TestSenderPlaintextRequiresConspicuousOptIn(t *testing.T) {
	address, _, result := startServer(t, serverOptions{})
	sender, err := forgesmtp.New(forgesmtp.Config{
		Address: address, UnsafeAllowPlaintext: true, DialTimeout: time.Second, OperationTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), testMessage(t, []mail.Address{{Address: "ada@example.com"}})); err != nil {
		t.Fatal(err)
	}
	completed := awaitResult(t, result)
	if completed.err != nil || !contains(completed.events, "data:plain") || contains(completed.events, "starttls") {
		t.Fatalf("result = %#v", completed)
	}
}

func TestSenderCancellationInterruptsBlockedGreeting(t *testing.T) {
	address, _, result := startServer(t, serverOptions{stallGreeting: true})
	sender, err := forgesmtp.New(forgesmtp.Config{Address: address, UnsafeAllowPlaintext: true, OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sender.Send(ctx, testMessage(t, []mail.Address{{Address: "ada@example.com"}})) }()
	time.Sleep(25 * time.Millisecond)
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancellation did not interrupt the SMTP greeting")
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("cancellation exceeded bound")
	}
	_ = awaitResult(t, result)
}

func TestSenderOperationDeadlineBoundsBlockedGreeting(t *testing.T) {
	address, _, result := startServer(t, serverOptions{stallGreeting: true})
	sender, err := forgesmtp.New(forgesmtp.Config{
		Address: address, UnsafeAllowPlaintext: true, DialTimeout: time.Second, OperationTimeout: 40 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = sender.Send(context.Background(), testMessage(t, []mail.Address{{Address: "ada@example.com"}}))
	if !errors.Is(err, forgesmtp.ErrDelivery) || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("deadline error = %v after %s", err, time.Since(started))
	}
	_ = awaitResult(t, result)
}

func TestSenderAbortsBeforeDATAWhenOneRecipientIsRejected(t *testing.T) {
	address, tlsConfig, result := startServer(t, serverOptions{startTLS: true, reject: "rejected@example.com"})
	sender, err := forgesmtp.New(forgesmtp.Config{Address: address, TLSConfig: tlsConfig, OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), testMessage(t, []mail.Address{
		{Address: "accepted@example.com"}, {Address: "rejected@example.com"},
	}))
	var delivery *forgesmtp.DeliveryError
	if !errors.Is(err, forgesmtp.ErrDelivery) || !errors.As(err, &delivery) || delivery.Stage() != "recipient" {
		t.Fatalf("error = %#v", err)
	}
	if strings.Contains(err.Error(), "rejected@example.com") || strings.Contains(err.Error(), "550") {
		t.Fatalf("error disclosed recipient/server response: %v", err)
	}
	completed := awaitResult(t, result)
	if containsPrefix(completed.events, "data:") || !contains(completed.events, "reset:tls") {
		t.Fatalf("partial recipient events = %v", completed.events)
	}
}

func testMessage(t *testing.T, recipients []mail.Address) forgemail.Message {
	t.Helper()
	message, err := forgemail.NewMessage(forgemail.MessageOptions{
		From: mail.Address{Name: "Application", Address: "sender@example.com"}, To: recipients,
		Subject: "SMTP delivery", Text: "plain body", HTML: "<p>HTML body</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func startServer(t *testing.T, options serverOptions) (string, *tls.Config, <-chan serverResult) {
	t.Helper()
	certificate, roots := testCertificate(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if options.implicitTLS {
		listener = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	}
	results := make(chan serverResult, 1)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			results <- serverResult{err: err}
			return
		}
		defer connection.Close()
		result := runServer(connection, certificate, options)
		results <- result
	}()
	return listener.Addr().String(), &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, results
}

func runServer(connection net.Conn, certificate tls.Certificate, options serverOptions) serverResult {
	result := serverResult{}
	tlsActive := options.implicitTLS
	if options.stallGreeting {
		_, _ = io.Copy(io.Discard, connection)
		return result
	}
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	if err := reply(writer, "220 smtp.test ESMTP ready\r\n"); err != nil {
		result.err = err
		return result
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				result.err = err
			}
			return result
		}
		command := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		upper := strings.ToUpper(command)
		suffix := "plain"
		if tlsActive {
			suffix = "tls"
		}
		switch {
		case strings.HasPrefix(upper, "EHLO ") || strings.HasPrefix(upper, "HELO "):
			result.events = append(result.events, "hello:"+suffix)
			capabilities := []string{"250-smtp.test"}
			if options.startTLS && !tlsActive {
				capabilities = append(capabilities, "250-STARTTLS")
			}
			if options.auth && tlsActive {
				capabilities = append(capabilities, "250-AUTH PLAIN")
			}
			capabilities = append(capabilities, "250 OK")
			if err := reply(writer, strings.Join(capabilities, "\r\n")+"\r\n"); err != nil {
				result.err = err
				return result
			}
		case upper == "STARTTLS":
			result.events = append(result.events, "starttls")
			if !options.startTLS || tlsActive {
				_ = reply(writer, "454 TLS unavailable\r\n")
				continue
			}
			if err := reply(writer, "220 begin TLS\r\n"); err != nil {
				result.err = err
				return result
			}
			secured := tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
			if err := secured.Handshake(); err != nil {
				result.err = err
				return result
			}
			connection, reader, writer = secured, bufio.NewReader(secured), bufio.NewWriter(secured)
			tlsActive = true
		case strings.HasPrefix(upper, "AUTH "):
			result.events = append(result.events, "auth:"+suffix)
			if !tlsActive || !options.auth {
				_ = reply(writer, "535 authentication rejected\r\n")
				continue
			}
			if err := reply(writer, "235 authenticated\r\n"); err != nil {
				result.err = err
				return result
			}
		case strings.HasPrefix(upper, "MAIL FROM:"):
			result.events = append(result.events, "mail:"+suffix)
			if err := reply(writer, "250 sender accepted\r\n"); err != nil {
				result.err = err
				return result
			}
		case strings.HasPrefix(upper, "RCPT TO:"):
			result.events = append(result.events, "recipient:"+suffix)
			if options.reject != "" && strings.Contains(command, options.reject) {
				_ = reply(writer, "550 recipient rejected\r\n")
				continue
			}
			if err := reply(writer, "250 recipient accepted\r\n"); err != nil {
				result.err = err
				return result
			}
		case upper == "RSET":
			result.events = append(result.events, "reset:"+suffix)
			if err := reply(writer, "250 reset\r\n"); err != nil {
				result.err = err
				return result
			}
		case upper == "DATA":
			result.events = append(result.events, "data:"+suffix)
			if err := reply(writer, "354 send message\r\n"); err != nil {
				result.err = err
				return result
			}
			var message strings.Builder
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					result.err = err
					return result
				}
				if line == ".\r\n" {
					break
				}
				if strings.HasPrefix(line, "..") {
					line = line[1:]
				}
				message.WriteString(line)
			}
			result.message = message.String()
			if err := reply(writer, "250 queued\r\n"); err != nil {
				result.err = err
				return result
			}
		case upper == "QUIT":
			result.events = append(result.events, "quit:"+suffix)
			_ = reply(writer, "221 bye\r\n")
			return result
		default:
			_ = reply(writer, "500 unsupported\r\n")
		}
	}
}

func reply(writer *bufio.Writer, response string) error {
	if _, err := writer.WriteString(response); err != nil {
		return err
	}
	return writer.Flush()
}

func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return certificate, roots
}

func awaitResult(t *testing.T, result <-chan serverResult) serverResult {
	t.Helper()
	select {
	case completed := <-result:
		return completed
	case <-time.After(2 * time.Second):
		t.Fatal("fake SMTP server did not stop")
		return serverResult{}
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsPrefix(values []string, wanted string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, wanted) {
			return true
		}
	}
	return false
}
