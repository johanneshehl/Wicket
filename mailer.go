package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTPConfig is the operator's own mail server. Wicket never sends mail through a service of its own.
type SMTPConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"` // "starttls" | "tls" | "none"
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	From     string `json:"from"`
	FromName string `json:"fromName"`
}

func (c SMTPConfig) Configured() bool { return c.Host != "" && c.Port > 0 && c.From != "" }

// Mail is one message with a plain-text and an optional HTML part.
type Mail struct {
	To      string
	Subject string
	Text    string
	HTML    string
	ReplyTo string
}

func validSMTP(c SMTPConfig) error {
	switch c.Security {
	case "starttls", "tls", "none":
	default:
		return fmt.Errorf("unknown SMTP security %q", c.Security)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid SMTP port %d", c.Port)
	}
	if !strings.Contains(c.From, "@") {
		return fmt.Errorf("invalid sender address %q", c.From)
	}
	return nil
}

func sendMail(c SMTPConfig, m Mail) error {
	if !c.Configured() {
		return fmt.Errorf("no mail server configured")
	}
	if err := validSMTP(c); err != nil {
		return err
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	tlsCfg := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	var err error
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if c.Security == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP handshake: %w", err)
	}
	defer client.Close()

	if c.Security == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("server does not offer STARTTLS")
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if c.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
				return fmt.Errorf("SMTP login: %w", err)
			}
		}
	}
	if err := client.Mail(c.From); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := client.Rcpt(m.To); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(buildMessage(c, m)); err != nil {
		wc.Close()
		return err
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("sending: %w", err)
	}
	return client.Quit()
}

func buildMessage(c SMTPConfig, m Mail) []byte {
	var b bytes.Buffer
	from := c.From
	if c.FromName != "" {
		from = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", c.FromName), c.From)
	}
	domain := c.From[strings.LastIndex(c.From, "@")+1:]
	id := make([]byte, 12)
	_, _ = rand.Read(id)
	boundary := "wicket-" + hex.EncodeToString(id)

	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from)
	header("To", m.To)
	if m.ReplyTo != "" {
		header("Reply-To", m.ReplyTo)
	}
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%s@%s>", hex.EncodeToString(id), domain))
	header("MIME-Version", "1.0")
	header("Auto-Submitted", "auto-generated")

	part := func(contentType, body string) {
		fmt.Fprintf(&b, "Content-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", contentType)
		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n")))
		_ = qp.Close()
		b.WriteString("\r\n")
	}
	if m.HTML == "" {
		part("text/plain", m.Text)
		return b.Bytes()
	}
	header("Content-Type", fmt.Sprintf(`multipart/alternative; boundary="%s"`, boundary))
	b.WriteString("\r\n")
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	part("text/plain", m.Text)
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	part("text/html", m.HTML)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes()
}
