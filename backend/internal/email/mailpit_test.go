package email

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestMailpitSenderDeliversRenderedMultipartEmail(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan []byte, 1)
	serverErr := make(chan error, 1)
	go acceptSMTPMessage(listener, received, serverErr)

	sender, err := NewMailpitSender(MailpitOptions{Address: listener.Addr().String(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := sender.Send(t.Context(), validTestMessage(), "gradex/test-mailpit-message")
	if err != nil {
		select {
		case serverErr := <-serverErr:
			t.Fatalf("sending message: %v (SMTP server: %v)", err, serverErr)
		default:
		}
		t.Fatal(err)
	}
	if result.ProviderMessageID != mailpitProviderID("gradex/test-mailpit-message") {
		t.Fatalf("provider message ID = %q", result.ProviderMessageID)
	}

	select {
	case err := <-serverErr:
		t.Fatal(err)
	case data := <-received:
		assertMailpitMessage(t, data)
	case <-time.After(time.Second):
		t.Fatal("Mailpit SMTP listener did not receive a message")
	}
}

func TestMailpitSenderRefusesNonLoopbackAddress(t *testing.T) {
	if _, err := NewMailpitSender(MailpitOptions{Address: "192.0.2.7:1025", Timeout: time.Second}); err == nil {
		t.Fatal("non-loopback Mailpit address was accepted")
	}
}

func acceptSMTPMessage(listener net.Listener, received chan<- []byte, serverErr chan<- error) {
	conn, err := listener.Accept()
	if err != nil {
		serverErr <- err
		return
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	if _, err := writer.WriteString("220 local Mailpit test\r\n"); err != nil {
		serverErr <- err
		return
	}
	if err := writer.Flush(); err != nil {
		serverErr <- err
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			serverErr <- err
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO "):
			_, err = writer.WriteString("250-local\r\n250 OK\r\n")
		case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
			_, err = writer.WriteString("250 OK\r\n")
		case line == "DATA\r\n":
			if _, err = writer.WriteString("354 send data\r\n"); err == nil {
				err = writer.Flush()
			}
			if err != nil {
				serverErr <- err
				return
			}
			var data bytes.Buffer
			for {
				line, err = reader.ReadString('\n')
				if err != nil {
					serverErr <- err
					return
				}
				if line == ".\r\n" {
					break
				}
				_, _ = data.WriteString(line)
			}
			received <- data.Bytes()
			_, err = writer.WriteString("250 accepted\r\n")
		case line == "QUIT\r\n":
			_, err = writer.WriteString("221 bye\r\n")
			if err == nil {
				err = writer.Flush()
			}
			if err != nil {
				serverErr <- err
			}
			return
		default:
			err = nil
		}
		if err == nil {
			err = writer.Flush()
		}
		if err != nil {
			serverErr <- err
			return
		}
	}
}

func assertMailpitMessage(t *testing.T, data []byte) {
	t.Helper()
	message, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if message.Header.Get("From") != validTestMessage().From || message.Header.Get("To") != validTestMessage().Recipient || message.Header.Get("Reply-To") != validTestMessage().ReplyTo {
		t.Fatalf("headers = %#v", message.Header)
	}
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("content type = %q (%v)", message.Header.Get("Content-Type"), err)
	}
	parts := multipart.NewReader(message.Body, params["boundary"])
	plain := readMailpitPart(t, parts)
	html := readMailpitPart(t, parts)
	if plain != validTestMessage().Text || html != validTestMessage().HTML {
		t.Fatalf("parts = plain %q html %q", plain, html)
	}
}

func readMailpitPart(t *testing.T, parts *multipart.Reader) string {
	t.Helper()
	part, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	reader := quotedprintable.NewReader(part)
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMailpitSenderHonorsCanceledContextBeforeNetwork(t *testing.T) {
	sender, err := NewMailpitSender(MailpitOptions{Address: "127.0.0.1:1025", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = sender.Send(ctx, validTestMessage(), "gradex/canceled")
	failure, ok := AsSendFailure(err)
	if !ok || !failure.Transient() || failure.Class != "timeout" {
		t.Fatalf("failure = %#v", err)
	}
}
