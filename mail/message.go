// Package mail defines immutable outbound messages and a replaceable delivery
// boundary. Transport implementations live in subpackages.
package mail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	netmail "net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaximumRecipients bounds one SMTP transaction and its To header.
	MaximumRecipients = 100
	// MaximumHeaderBytes is the RFC 5322 hard limit for one header line,
	// excluding its terminating CRLF.
	MaximumHeaderBytes = 998
	// MaximumBodyBytes bounds the combined unencoded text and HTML bodies.
	MaximumBodyBytes = 4 << 20
)

var ErrInvalidMessage = errors.New("mail: invalid message")

// Sender is the complete outbound delivery seam. Applications can replace an
// SMTP sender with an API transport, test double, or transactional outbox.
type Sender interface {
	Send(context.Context, Message) error
}

// MessageOptions is consumed by NewMessage. NewMessage copies all values, so
// later changes to this structure or its To slice cannot alter the Message.
type MessageOptions struct {
	From    netmail.Address
	To      []netmail.Address
	ReplyTo *netmail.Address
	Subject string
	Text    string
	HTML    string
	// Date defaults to the construction time. Supplying it is useful for
	// deterministic fixtures and messages restored from an outbox.
	Date time.Time
}

// Message is an immutable, validated email message.
type Message struct {
	from     netmail.Address
	to       []netmail.Address
	replyTo  netmail.Address
	hasReply bool
	subject  string
	text     string
	html     string
	date     time.Time
	valid    bool
}

// MessageError identifies a bad field without including submitted content.
type MessageError struct{ field string }

func (err *MessageError) Error() string { return "mail: invalid message field " + err.field }
func (err *MessageError) Unwrap() error { return ErrInvalidMessage }
func (err *MessageError) Field() string { return err.field }

// NewMessage validates and snapshots an outbound message. At least one of Text
// or HTML is required; supplying both emits multipart/alternative.
func NewMessage(options MessageOptions) (Message, error) {
	if err := validateAddress("from", options.From); err != nil {
		return Message{}, err
	}
	if len(options.To) == 0 || len(options.To) > MaximumRecipients {
		return Message{}, invalid("to")
	}
	recipients := append([]netmail.Address(nil), options.To...)
	for _, address := range recipients {
		if err := validateAddress("to", address); err != nil {
			return Message{}, err
		}
	}
	if options.ReplyTo != nil {
		if err := validateAddress("reply-to", *options.ReplyTo); err != nil {
			return Message{}, err
		}
	}
	if options.Subject == "" || !validHeaderValue(options.Subject) {
		return Message{}, invalid("subject")
	}
	if !validBody(options.Text) || !validBody(options.HTML) || len(options.Text) > MaximumBodyBytes-len(options.HTML) {
		return Message{}, invalid("body")
	}
	if options.Text == "" && options.HTML == "" {
		return Message{}, invalid("body")
	}

	date := options.Date
	if date.IsZero() {
		date = time.Now()
	}
	message := Message{
		from: options.From, to: recipients, subject: options.Subject,
		text: options.Text, html: options.HTML, date: date, valid: true,
	}
	if options.ReplyTo != nil {
		message.replyTo, message.hasReply = *options.ReplyTo, true
	}
	if err := message.validateRenderedHeaders(); err != nil {
		return Message{}, err
	}
	return message, nil
}

func invalid(field string) error { return &MessageError{field: field} }

func validateAddress(field string, address netmail.Address) error {
	if address.Address == "" || !validHeaderValue(address.Address) || !validHeaderValue(address.Name) {
		return invalid(field)
	}
	parsed, err := netmail.ParseAddress(address.Address)
	if err != nil || parsed.Address != address.Address || parsed.Name != "" {
		return invalid(field)
	}
	for _, character := range address.Address {
		// net/smtp has no SMTPUTF8 support, so accepting a non-ASCII envelope
		// address here would produce a message the transport cannot send.
		if character > 0x7e {
			return invalid(field)
		}
	}
	return nil
}

func validHeaderValue(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func validBody(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func (message Message) validateRenderedHeaders() error {
	if len("From: ")+len(message.from.String()) > MaximumHeaderBytes {
		return invalid("from")
	}
	to := make([]string, len(message.to))
	for index := range message.to {
		to[index] = message.to[index].String()
	}
	if len("To: ")+len(strings.Join(to, ", ")) > MaximumHeaderBytes {
		return invalid("to")
	}
	if message.hasReply && len("Reply-To: ")+len(message.replyTo.String()) > MaximumHeaderBytes {
		return invalid("reply-to")
	}
	if len("Subject: ")+len(encodeSubject(message.subject)) > MaximumHeaderBytes {
		return invalid("subject")
	}
	return nil
}

func encodeSubject(subject string) string {
	for _, character := range subject {
		if character < 0x20 || character > 0x7e {
			return mime.QEncoding.Encode("UTF-8", subject)
		}
	}
	return subject
}

// From returns a copy of the envelope sender and From header address.
func (message Message) From() netmail.Address { return message.from }

// To returns a copy of the envelope recipients and To header addresses.
func (message Message) To() []netmail.Address {
	return append([]netmail.Address(nil), message.to...)
}

// ReplyTo returns the optional Reply-To address.
func (message Message) ReplyTo() (netmail.Address, bool) {
	return message.replyTo, message.hasReply
}

func (message Message) Subject() string { return message.subject }
func (message Message) Text() string    { return message.text }
func (message Message) HTML() string    { return message.html }
func (message Message) Date() time.Time { return message.date }

// WriteRFC5322 writes a deterministic RFC 5322 message using CRLF line endings and
// quoted-printable UTF-8 bodies. It does not perform SMTP dot stuffing; SMTP
// transports apply that at the DATA boundary.
func (message Message) WriteRFC5322(writer io.Writer) error {
	if !message.valid {
		return ErrInvalidMessage
	}
	var encoded bytes.Buffer
	writeHeader(&encoded, "Date", message.date.Format(time.RFC1123Z))
	writeHeader(&encoded, "From", message.from.String())
	recipients := make([]string, len(message.to))
	for index := range message.to {
		recipients[index] = message.to[index].String()
	}
	writeHeader(&encoded, "To", strings.Join(recipients, ", "))
	if message.hasReply {
		writeHeader(&encoded, "Reply-To", message.replyTo.String())
	}
	writeHeader(&encoded, "Subject", encodeSubject(message.subject))
	writeHeader(&encoded, "MIME-Version", "1.0")

	var err error
	switch {
	case message.text != "" && message.html != "":
		boundary := message.boundary()
		writeHeader(&encoded, "Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": boundary}))
		encoded.WriteString("\r\n")
		err = writePart(&encoded, boundary, "text/plain; charset=UTF-8", message.text)
		if err == nil {
			err = writePart(&encoded, boundary, "text/html; charset=UTF-8", message.html)
		}
		if err == nil {
			fmt.Fprintf(&encoded, "--%s--\r\n", boundary)
		}
	case message.text != "":
		writeHeader(&encoded, "Content-Type", "text/plain; charset=UTF-8")
		writeHeader(&encoded, "Content-Transfer-Encoding", "quoted-printable")
		encoded.WriteString("\r\n")
		err = writeQuotedPrintable(&encoded, message.text)
	case message.html != "":
		writeHeader(&encoded, "Content-Type", "text/html; charset=UTF-8")
		writeHeader(&encoded, "Content-Transfer-Encoding", "quoted-printable")
		encoded.WriteString("\r\n")
		err = writeQuotedPrintable(&encoded, message.html)
	}
	if err != nil {
		return fmt.Errorf("mail: encode message: %w", err)
	}
	_, err = writer.Write(encoded.Bytes())
	return err
}

func writeHeader(writer *bytes.Buffer, name, value string) {
	fmt.Fprintf(writer, "%s: %s\r\n", name, value)
}

func writePart(writer *bytes.Buffer, boundary, contentType, body string) error {
	fmt.Fprintf(writer, "--%s\r\n", boundary)
	writeHeader(writer, "Content-Type", contentType)
	writeHeader(writer, "Content-Transfer-Encoding", "quoted-printable")
	writer.WriteString("\r\n")
	return writeQuotedPrintable(writer, body)
}

func writeQuotedPrintable(writer io.Writer, body string) error {
	encoder := quotedprintable.NewWriter(writer)
	if _, err := io.WriteString(encoder, normalizeCRLF(body)); err != nil {
		_ = encoder.Close()
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	if !strings.HasSuffix(normalizeCRLF(body), "\r\n") {
		_, err := io.WriteString(writer, "\r\n")
		return err
	}
	return nil
}

func normalizeCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func (message Message) boundary() string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, message.from.Address)
	for _, recipient := range message.to {
		_, _ = io.WriteString(hash, "\x00"+recipient.Address)
	}
	_, _ = io.WriteString(hash, "\x00"+message.subject+"\x00"+message.text+"\x00"+message.html)
	return "goforge-" + hex.EncodeToString(hash.Sum(nil)[:18])
}
