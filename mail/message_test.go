package mail_test

import (
	"bytes"
	"errors"
	"net/mail"
	"strings"
	"testing"
	"time"

	forgemail "github.com/ShanilKoshitha/goforge/mail"
)

func TestMessageIsValidatedImmutableAndMultipart(t *testing.T) {
	recipients := []mail.Address{{Name: "Ada", Address: "ada@example.com"}, {Address: "grace@example.com"}}
	reply := mail.Address{Address: "reply@example.com"}
	message, err := forgemail.NewMessage(forgemail.MessageOptions{
		From: mail.Address{Name: "GoForge", Address: "sender@example.com"}, To: recipients, ReplyTo: &reply,
		Subject: "Welcome ✓", Text: "Hello\n.World", HTML: "<p>Hello</p>\rnext",
		Date: time.Date(2026, time.September, 8, 12, 34, 56, 0, time.FixedZone("ADT", -3*60*60)),
	})
	if err != nil {
		t.Fatal(err)
	}
	recipients[0].Address = "changed@example.com"
	reply.Address = "changed@example.com"
	returned := message.To()
	returned[0].Address = "also-changed@example.com"
	if message.To()[0].Address != "ada@example.com" {
		t.Fatal("message recipients were mutable through caller-owned slices")
	}
	if address, ok := message.ReplyTo(); !ok || address.Address != "reply@example.com" {
		t.Fatalf("reply-to = %#v, %v", address, ok)
	}

	var first, second bytes.Buffer
	if err := message.WriteRFC5322(&first); err != nil {
		t.Fatal(err)
	}
	if err := message.WriteRFC5322(&second); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("message encoding is not deterministic")
	}
	wire := first.String()
	for _, required := range []string{
		"Date: Tue, 08 Sep 2026 12:34:56 -0300\r\n",
		"From: \"GoForge\" <sender@example.com>\r\n",
		"To: \"Ada\" <ada@example.com>, <grace@example.com>\r\n",
		"Reply-To: <reply@example.com>\r\n",
		"Subject: =?UTF-8?q?Welcome_=E2=9C=93?=\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: multipart/alternative; boundary=goforge-",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"Content-Type: text/html; charset=UTF-8\r\n",
	} {
		if !strings.Contains(wire, required) {
			t.Errorf("wire message omits %q:\n%s", required, wire)
		}
	}
	withoutCRLF := strings.ReplaceAll(wire, "\r\n", "")
	if strings.ContainsAny(withoutCRLF, "\r\n") {
		t.Fatal("wire message contains a bare line ending")
	}
}

func TestMessageRejectsInjectionAndBoundsWithoutDisclosure(t *testing.T) {
	base := forgemail.MessageOptions{
		From: mail.Address{Address: "sender@example.com"}, To: []mail.Address{{Address: "ada@example.com"}},
		Subject: "Subject", Text: "body",
	}
	tests := []struct {
		name  string
		alter func(*forgemail.MessageOptions)
		field string
	}{
		{name: "from injection", alter: func(value *forgemail.MessageOptions) {
			value.From.Address = "sender@example.com\r\nBcc: stolen@example.com"
		}, field: "from"},
		{name: "recipient injection", alter: func(value *forgemail.MessageOptions) { value.To[0].Name = "Ada\nBcc: stolen@example.com" }, field: "to"},
		{name: "subject injection", alter: func(value *forgemail.MessageOptions) { value.Subject = "secret\r\nBcc: stolen@example.com" }, field: "subject"},
		{name: "empty body", alter: func(value *forgemail.MessageOptions) { value.Text = "" }, field: "body"},
		{name: "oversized body", alter: func(value *forgemail.MessageOptions) { value.Text = strings.Repeat("x", forgemail.MaximumBodyBytes+1) }, field: "body"},
		{name: "too many recipients", alter: func(value *forgemail.MessageOptions) { value.To = make([]mail.Address, forgemail.MaximumRecipients+1) }, field: "to"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := base
			options.To = append([]mail.Address(nil), base.To...)
			test.alter(&options)
			_, err := forgemail.NewMessage(options)
			if !errors.Is(err, forgemail.ErrInvalidMessage) {
				t.Fatalf("error = %v", err)
			}
			var messageError *forgemail.MessageError
			if !errors.As(err, &messageError) || messageError.Field() != test.field {
				t.Fatalf("message error = %#v", err)
			}
			if strings.Contains(err.Error(), "stolen@example.com") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error disclosed submitted content: %v", err)
			}
		})
	}
}

func TestZeroMessageCannotBeWritten(t *testing.T) {
	if err := (forgemail.Message{}).WriteRFC5322(&bytes.Buffer{}); !errors.Is(err, forgemail.ErrInvalidMessage) {
		t.Fatalf("error = %v", err)
	}
}
