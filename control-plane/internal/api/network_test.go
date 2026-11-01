package api

import (
	"net"
	"testing"
)

func TestCoversServer(t *testing.T) {
	ips := []net.IP{net.ParseIP("2.25.209.147"), net.ParseIP("2a02:4780:75:bb36::1")}
	for _, c := range []struct {
		allowed []string
		want    bool
	}{
		{[]string{"85.28.71.71/32"}, false},
		{[]string{"85.28.71.71/32", "2.25.209.147/32"}, true},
		{[]string{"2.25.209.0/24"}, true},
		{[]string{"2a02:4780:75::/48"}, true},
		{nil, false},
	} {
		if got := coversServer(c.allowed, ips); got != c.want {
			t.Errorf("coversServer(%v) = %v, want %v", c.allowed, got, c.want)
		}
	}
}
