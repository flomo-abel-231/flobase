package api

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTP is the mail server FloBase sends its own emails through (deletion
// warnings). Port 465 is TLS from the start; other ports upgrade with
// STARTTLS when the server offers it.
type SMTP struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
}

func (m SMTP) enabled() bool { return m.Host != "" && m.From != "" }

var errNoMail = errors.New("email is not set up: set FLOBASE_SMTP_HOST and FLOBASE_SMTP_FROM")

// send emails a plain-text message to each address.
func (m SMTP) send(to []string, subject, body string) error {
	if !m.enabled() {
		return errNoMail
	}
	if len(to) == 0 {
		return nil
	}
	port := m.Port
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(m.Host, strconv.Itoa(port))
	var msg strings.Builder
	fmt.Fprintf(&msg, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n",
		m.From, strings.Join(to, ", "), subject, time.Now().UTC().Format(time.RFC1123Z))
	msg.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))

	var c *smtp.Client
	var err error
	if port == 465 {
		conn, derr := tls.DialWithDialer(&net.Dialer{Timeout: 20 * time.Second}, "tcp", addr, &tls.Config{ServerName: m.Host})
		if derr != nil {
			return derr
		}
		c, err = smtp.NewClient(conn, m.Host)
	} else {
		conn, derr := net.DialTimeout("tcp", addr, 20*time.Second)
		if derr != nil {
			return derr
		}
		c, err = smtp.NewClient(conn, m.Host)
		if err == nil {
			if ok, _ := c.Extension("STARTTLS"); ok {
				err = c.StartTLS(&tls.Config{ServerName: m.Host})
			}
		}
	}
	if err != nil {
		return err
	}
	defer c.Close()
	if m.User != "" {
		if err := c.Auth(smtp.PlainAuth("", m.User, m.Password, m.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(address(m.From)); err != nil {
		return err
	}
	for _, t := range to {
		if err := c.Rcpt(t); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg.String())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// address takes the bare address out of "Name <a@b>".
func address(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(strings.TrimSpace(from[i+1:]), ">")
	}
	return strings.TrimSpace(from)
}
