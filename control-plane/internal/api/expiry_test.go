package api

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestWarning(t *testing.T) {
	n := func(v int) *int { return &v }
	for _, c := range []struct {
		left   int
		warned *int
		want   int
	}{
		{20, nil, 0},
		{15, nil, 15},
		{14, n(15), 0},
		{10, n(15), 10},
		{12, nil, 15},
		{4, n(10), 5},
		{3, n(5), 3},
		{1, n(3), 1},
		{1, n(1), 0},
		{2, n(15), 3}, // missed steps: send the nearest one
	} {
		if got := warning(c.left, c.warned); got != c.want {
			t.Errorf("warning(%d, %v) = %d, want %d", c.left, c.warned, got, c.want)
		}
	}
}

func TestDaysLeft(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if d := daysLeft(now.Add(14*24*time.Hour+time.Hour), now); d != 15 {
		t.Errorf("daysLeft = %d, want 15", d)
	}
	if d := daysLeft(now.Add(-time.Minute), now); d > 0 {
		t.Errorf("daysLeft past = %d, want <= 0", d)
	}
}

func TestAddress(t *testing.T) {
	if a := address("FloBase <noreply@example.com>"); a != "noreply@example.com" {
		t.Errorf("address = %q", a)
	}
	if a := address("noreply@example.com"); a != "noreply@example.com" {
		t.Errorf("address = %q", a)
	}
}

// TestSend talks to a minimal SMTP server and checks the message arrives.
func TestSend(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		say := func(s string) { fmt.Fprintf(c, "%s\r\n", s) }
		say("220 test")
		var data strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 test")
			case strings.HasPrefix(cmd, "DATA"):
				say("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				got <- data.String()
				say("250 ok")
			case strings.HasPrefix(cmd, "QUIT"):
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	m := SMTP{Host: "127.0.0.1", Port: port, From: "FloBase <noreply@example.test>"}
	if err := m.send([]string{"a@example.test"}, "Hello", "Line one\nLine two"); err != nil {
		t.Fatal(err)
	}
	msg := <-got
	for _, want := range []string{"Subject: Hello", "To: a@example.test", "Line one\r\nLine two"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
}
