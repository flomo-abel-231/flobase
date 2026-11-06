package api

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

// Mailer sends transactional email over SMTP. Gmail: host smtp.gmail.com,
// port 587 (STARTTLS) or 465 (implicit TLS), user your address, password an
// app password from Google Account → Security → App passwords.
type Mailer struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
}

func (m *Mailer) send(to, subject, html string) error {
	addr := net.JoinHostPort(m.Host, m.Port)
	auth := smtp.PlainAuth("", m.User, m.Password, m.Host)
	msg := []byte("From: " + m.From + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n\r\n" +
		html)

	if m.Port == "465" {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: m.Host})
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, m.Host)
		if err != nil {
			return err
		}
		defer client.Close()
		if err := client.Auth(auth); err != nil {
			return err
		}
		if err := client.Mail(m.From); err != nil {
			return err
		}
		if err := client.Rcpt(to); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		if _, err = w.Write(msg); err != nil {
			return err
		}
		return w.Close()
	}

	return smtp.SendMail(addr, auth, m.From, []string{to}, msg)
}

func (m *Mailer) sendPasswordReset(to, resetURL string, expires time.Time) error {
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family:sans-serif;max-width:480px;margin:40px auto;color:#111">
<h2 style="margin-bottom:8px">Reset your FloBase password</h2>
<p style="color:#555;margin-bottom:24px">This link expires in one hour (%s).</p>
<a href="%s" style="display:inline-block;padding:10px 20px;background:#0066ff;color:#fff;border-radius:6px;text-decoration:none;font-weight:600">Reset password</a>
<p style="margin-top:24px;color:#888;font-size:13px">If you did not request this, ignore this email — your password will not change.</p>
<hr style="border:none;border-top:1px solid #eee;margin:24px 0">
<p style="color:#aaa;font-size:12px">FloBase</p>
</body>
</html>`, expires.UTC().Format("15:04 UTC"), resetURL)
	return m.send(to, "Reset your FloBase password", body)
}

func (m *Mailer) sendInvite(to, role, inviteURL string, expires time.Time) error {
	roleLabel := "a member"
	if role == "admin" {
		roleLabel = "an admin"
	}
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family:sans-serif;max-width:480px;margin:40px auto;color:#111">
<h2 style="margin-bottom:8px">You have been invited to FloBase</h2>
<p style="color:#555;margin-bottom:4px">You have been invited as %s.</p>
<p style="color:#555;margin-bottom:24px">This invite expires on %s.</p>
<a href="%s" style="display:inline-block;padding:10px 20px;background:#0066ff;color:#fff;border-radius:6px;text-decoration:none;font-weight:600">Accept invite</a>
<hr style="border:none;border-top:1px solid #eee;margin:24px 0">
<p style="color:#aaa;font-size:12px">FloBase</p>
</body>
</html>`, roleLabel, expires.UTC().Format("2 Jan 2006"), inviteURL)
	return m.send(to, "You have been invited to FloBase", body)
}
