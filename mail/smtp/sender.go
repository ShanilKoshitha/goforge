// Package smtp delivers GoForge mail messages through SMTP.
package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	netsmtp "net/smtp"
	"strconv"
	"strings"
	"time"

	forgemail "github.com/ShanilKoshitha/goforge/mail"
)

const (
	defaultDialTimeout      = 10 * time.Second
	defaultOperationTimeout = 10 * time.Second
)

var (
	ErrInvalidConfiguration = errors.New("mail/smtp: invalid configuration")
	ErrDelivery             = errors.New("mail/smtp: delivery failed")
)

// TLSMode selects the mandatory TLS negotiation style.
type TLSMode uint8

const (
	// TLSSTARTTLS is the secure default and requires the server to advertise
	// STARTTLS before credentials or message data are sent.
	TLSSTARTTLS TLSMode = iota
	// TLSImplicit establishes TLS before reading the SMTP greeting.
	TLSImplicit
)

type Config struct {
	Address          string
	TLSMode          TLSMode
	TLSConfig        *tls.Config
	Username         string
	Password         string
	LocalName        string
	DialTimeout      time.Duration
	OperationTimeout time.Duration

	// UnsafeAllowPlaintext conspicuously opts local development into SMTP
	// without TLS. Authentication remains forbidden in this mode.
	UnsafeAllowPlaintext bool
}

// Sender is immutable after construction and safe for concurrent use. Each
// delivery owns one SMTP connection and has no package-global state.
type Sender struct {
	address          string
	host             string
	tlsMode          TLSMode
	tlsConfig        *tls.Config
	username         string
	password         string
	localName        string
	dialTimeout      time.Duration
	operationTimeout time.Duration
	plaintext        bool
}

// DeliveryError identifies the failed protocol stage without including server
// replies, credentials, recipients, or message content in Error().
type DeliveryError struct {
	stage string
	cause error
}

func (err *DeliveryError) Error() string { return "mail/smtp: delivery failed during " + err.stage }
func (err *DeliveryError) Unwrap() error { return err.cause }
func (err *DeliveryError) Is(target error) bool {
	return target == ErrDelivery || errors.Is(err.cause, target)
}
func (err *DeliveryError) Stage() string { return err.stage }

// New validates and snapshots SMTP configuration.
func New(config Config) (*Sender, error) {
	host, port, err := net.SplitHostPort(config.Address)
	if err != nil || host == "" {
		return nil, ErrInvalidConfiguration
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, ErrInvalidConfiguration
	}
	if config.TLSMode != TLSSTARTTLS && config.TLSMode != TLSImplicit {
		return nil, ErrInvalidConfiguration
	}
	if config.UnsafeAllowPlaintext && config.TLSMode == TLSImplicit {
		return nil, ErrInvalidConfiguration
	}
	if config.UnsafeAllowPlaintext && !localHost(host) {
		return nil, ErrInvalidConfiguration
	}
	if (config.Username == "") != (config.Password == "") {
		return nil, ErrInvalidConfiguration
	}
	if config.UnsafeAllowPlaintext && config.Username != "" {
		return nil, ErrInvalidConfiguration
	}
	if config.DialTimeout < 0 || config.OperationTimeout < 0 {
		return nil, ErrInvalidConfiguration
	}
	localName := config.LocalName
	if localName == "" {
		localName = "localhost"
	}
	if !validSMTPToken(localName) {
		return nil, ErrInvalidConfiguration
	}
	dialTimeout := config.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = defaultDialTimeout
	}
	operationTimeout := config.OperationTimeout
	if operationTimeout == 0 {
		operationTimeout = defaultOperationTimeout
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	if config.TLSConfig != nil {
		tlsConfig = config.TLSConfig.Clone()
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = host
		}
		if tlsConfig.MinVersion == 0 || tlsConfig.MinVersion < tls.VersionTLS12 {
			tlsConfig.MinVersion = tls.VersionTLS12
		}
	}
	return &Sender{
		address: config.Address, host: host, tlsMode: config.TLSMode,
		tlsConfig: tlsConfig, username: config.Username, password: config.Password,
		localName: localName, dialTimeout: dialTimeout,
		operationTimeout: operationTimeout, plaintext: config.UnsafeAllowPlaintext,
	}, nil
}

func validSMTPToken(value string) bool {
	if strings.TrimSpace(value) != value || value == "" {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func localHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// Send delivers one validated message. Cancellation closes the active network
// connection so blocked greeting, TLS, AUTH, envelope, and DATA operations stop.
func (sender *Sender) Send(ctx context.Context, message forgemail.Message) error {
	if sender == nil {
		return ErrInvalidConfiguration
	}
	if err := ctx.Err(); err != nil {
		return delivery("dial", err)
	}
	var encoded bytes.Buffer
	if err := message.WriteRFC5322(&encoded); err != nil {
		return err
	}

	connection, err := (&net.Dialer{Timeout: sender.dialTimeout}).DialContext(ctx, "tcp", sender.address)
	if err != nil {
		return deliveryContext(ctx, "dial", err)
	}
	defer connection.Close()
	// Always close the original socket: TLS connections wrap it, and keeping a
	// stable reference avoids racing cancellation with the wrapper assignment.
	socket := connection
	stopCancellation := context.AfterFunc(ctx, func() { _ = socket.Close() })
	defer stopCancellation()
	if sender.tlsMode == TLSImplicit {
		if err := sender.setDeadline(ctx, connection); err != nil {
			return delivery("tls", err)
		}
		secured := tls.Client(connection, sender.tlsConfig.Clone())
		if err := secured.HandshakeContext(ctx); err != nil {
			return deliveryContext(ctx, "tls", err)
		}
		connection = secured
	}
	if err := sender.setDeadline(ctx, connection); err != nil {
		return delivery("greeting", err)
	}
	client, err := netsmtp.NewClient(connection, sender.host)
	if err != nil {
		return deliveryContext(ctx, "greeting", err)
	}
	defer client.Close()

	if err := sender.operation(ctx, connection, "hello", func() error { return client.Hello(sender.localName) }); err != nil {
		return err
	}
	tlsActive := sender.tlsMode == TLSImplicit
	if !sender.plaintext && sender.tlsMode == TLSSTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return delivery("tls", errors.New("STARTTLS is unavailable"))
		}
		if err := sender.operation(ctx, connection, "tls", func() error { return client.StartTLS(sender.tlsConfig.Clone()) }); err != nil {
			return err
		}
		tlsActive = true
	}
	if sender.username != "" {
		if !tlsActive {
			return ErrInvalidConfiguration
		}
		auth := netsmtp.PlainAuth("", sender.username, sender.password, sender.host)
		if err := sender.operation(ctx, connection, "authentication", func() error { return client.Auth(auth) }); err != nil {
			return err
		}
	}

	from := message.From()
	if err := sender.operation(ctx, connection, "sender", func() error { return client.Mail(from.Address) }); err != nil {
		return err
	}
	for _, recipient := range message.To() {
		if err := sender.operation(ctx, connection, "recipient", func() error { return client.Rcpt(recipient.Address) }); err != nil {
			_ = sender.operation(ctx, connection, "reset", client.Reset)
			return err
		}
	}

	if err := sender.setDeadline(ctx, connection); err != nil {
		return delivery("data", err)
	}
	data, err := client.Data()
	if err != nil {
		return deliveryContext(ctx, "data", err)
	}
	if _, err := data.Write(encoded.Bytes()); err != nil {
		_ = data.Close()
		return deliveryContext(ctx, "data", err)
	}
	if err := data.Close(); err != nil {
		return deliveryContext(ctx, "data", err)
	}

	// DATA's final 250 response is the delivery boundary. A subsequent QUIT
	// failure must not encourage callers to deliver the accepted message twice.
	_ = sender.operation(ctx, connection, "quit", client.Quit)
	return nil
}

func (sender *Sender) operation(ctx context.Context, connection net.Conn, stage string, operation func() error) error {
	if err := sender.setDeadline(ctx, connection); err != nil {
		return delivery(stage, err)
	}
	if err := operation(); err != nil {
		return deliveryContext(ctx, stage, err)
	}
	return nil
}

func (sender *Sender) setDeadline(ctx context.Context, connection net.Conn) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(sender.operationTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	return connection.SetDeadline(deadline)
}

func delivery(stage string, cause error) error {
	return &DeliveryError{stage: stage, cause: cause}
}

func deliveryContext(ctx context.Context, stage string, cause error) error {
	if err := ctx.Err(); err != nil {
		cause = err
	}
	return delivery(stage, cause)
}

var _ forgemail.Sender = (*Sender)(nil)
