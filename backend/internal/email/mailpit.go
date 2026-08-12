package email

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// MailpitOptions configures the development-only local SMTP adapter.
type MailpitOptions struct {
	Address string
	Timeout time.Duration
}

// MailpitSender delivers already-rendered transactional messages to Mailpit.
// Configuration ensures its address is a loopback endpoint and it is selected
// only in development.
type MailpitSender struct {
	address string
	timeout time.Duration
	dial    net.Dialer
}

type mailpitEnvelope struct {
	from       string
	recipient  string
	data       []byte
	providerID string
}

func NewMailpitSender(options MailpitOptions) (*MailpitSender, error) {
	if err := validateMailpitAddress(options.Address); err != nil {
		return nil, err
	}
	if options.Timeout < time.Second || options.Timeout > 30*time.Second {
		return nil, errors.New("Mailpit timeout must be between 1s and 30s")
	}
	return &MailpitSender{address: options.Address, timeout: options.Timeout}, nil
}

func (*MailpitSender) Provider() string { return "mailpit" }

func (s *MailpitSender) Send(ctx context.Context, message Message, idempotencyKey string) (SendResult, error) {
	envelope, err := mailpitEnvelopeFor(message, idempotencyKey)
	if err != nil {
		return SendResult{}, err
	}
	if err := s.deliver(ctx, envelope); err != nil {
		return SendResult{}, err
	}
	return SendResult{ProviderMessageID: envelope.providerID}, nil
}

func mailpitEnvelopeFor(message Message, idempotencyKey string) (mailpitEnvelope, error) {
	if err := validateMessage(message); err != nil {
		return mailpitEnvelope{}, permanent("invalid_message", "invalid_message")
	}
	if !validIdempotencyKey(idempotencyKey) {
		return mailpitEnvelope{}, permanent("invalid_idempotency_key", "invalid_idempotency_key")
	}
	data, err := mailpitData(message, idempotencyKey)
	if err != nil {
		return mailpitEnvelope{}, permanent("invalid_message", "encoding_failed")
	}
	from, err := mail.ParseAddress(message.From)
	if err != nil {
		return mailpitEnvelope{}, permanent("invalid_message", "invalid_message")
	}
	return mailpitEnvelope{from: from.Address, recipient: message.Recipient, data: data, providerID: mailpitProviderID(idempotencyKey)}, nil
}

func (s *MailpitSender) deliver(ctx context.Context, envelope mailpitEnvelope) error {
	conn, deadline, cancel, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	defer conn.Close()
	return deliverMailpitSMTP(deadline, conn, mailpitHost(s.address), envelope)
}

func (s *MailpitSender) connect(ctx context.Context) (net.Conn, context.Context, context.CancelFunc, error) {
	deadline, cancel := context.WithTimeout(ctx, s.timeout)
	conn, err := s.dial.DialContext(deadline, "tcp", s.address)
	if err != nil {
		cancel()
		return nil, nil, nil, mailpitDeliveryFailure(deadline, err)
	}
	if at, ok := deadline.Deadline(); ok {
		if err := conn.SetDeadline(at); err != nil {
			_ = conn.Close()
			cancel()
			return nil, nil, nil, mailpitDeliveryFailure(deadline, err)
		}
	}
	return conn, deadline, cancel, nil
}

func deliverMailpitSMTP(ctx context.Context, conn net.Conn, host string, envelope mailpitEnvelope) error {
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return mailpitDeliveryFailure(ctx, err)
	}
	err = smtpDeliver(client, envelope.from, envelope.recipient, envelope.data)
	if err != nil {
		return mailpitDeliveryFailure(ctx, err)
	}
	return nil
}

func smtpDeliver(client *smtp.Client, from, recipient string, data []byte) (err error) {
	defer func() { _ = client.Quit() }()
	if err = client.Mail(from); err != nil {
		return err
	}
	if err = client.Rcpt(recipient); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(data); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func mailpitDeliveryFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return transient("timeout", "timeout", 0)
	}
	var smtpError *textproto.Error
	if errors.As(err, &smtpError) && smtpError.Code >= 500 {
		return permanent("smtp_rejected", "smtp_rejected")
	}
	return transient("smtp_delivery", "smtp_delivery", 0)
}

func mailpitData(message Message, idempotencyKey string) ([]byte, error) {
	var buffer bytes.Buffer
	boundary := multipart.NewWriter(&buffer)
	if err := writeMailpitHeaders(&buffer, message, idempotencyKey, boundary.Boundary()); err != nil {
		return nil, err
	}
	if err := writeMailpitPart(boundary, "text/plain", message.Text); err != nil {
		return nil, err
	}
	if err := writeMailpitPart(boundary, "text/html", message.HTML); err != nil {
		return nil, err
	}
	if err := boundary.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeMailpitHeaders(buffer *bytes.Buffer, message Message, idempotencyKey, boundary string) error {
	if _, err := fmt.Fprintf(buffer, "From: %s\r\nTo: %s\r\n", message.From, message.Recipient); err != nil {
		return err
	}
	if message.ReplyTo != "" {
		if _, err := fmt.Fprintf(buffer, "Reply-To: %s\r\n", message.ReplyTo); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(buffer, "Subject: %s\r\nMIME-Version: 1.0\r\nMessage-ID: <%s@mailpit.local>\r\nContent-Type: multipart/alternative; boundary=%q\r\n\r\n", mime.QEncoding.Encode("UTF-8", message.Subject), mailpitProviderID(idempotencyKey), boundary)
	return err
}

func writeMailpitPart(boundary *multipart.Writer, contentType, content string) error {
	headers := textproto.MIMEHeader{}
	headers.Set("Content-Type", contentType+`; charset="UTF-8"`)
	headers.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := boundary.CreatePart(headers)
	if err != nil {
		return err
	}
	writer := quotedprintable.NewWriter(part)
	if _, err := io.WriteString(writer, strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\n", "\r\n")); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func validateMailpitAddress(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || host == "" || port == "" {
		return errors.New("Mailpit address must be a loopback host:port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("Mailpit address must use a loopback IP address")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("Mailpit address must contain a valid port")
	}
	return nil
}

func mailpitHost(address string) string {
	host, _, _ := net.SplitHostPort(address)
	return host
}

func validIdempotencyKey(value string) bool {
	return len(value) >= 1 && len(value) <= 256 && !strings.ContainsAny(value, "\r\n\x00")
}

func mailpitProviderID(idempotencyKey string) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return "mailpit-" + hex.EncodeToString(digest[:16])
}
