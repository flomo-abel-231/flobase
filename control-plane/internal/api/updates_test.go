package api

import (
	"context"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.9", true},
		{"0.1.10", "0.1.9", true},
		{"1.0.0", "0.99.99", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"v0.2.0", "0.1.0", true},
		{"0.2.0", "dev", false}, // a dev build never auto-updates
		{"", "0.1.0", false},
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestUpdateViewLocal(t *testing.T) {
	s := &Server{cfg: Config{Version: "0.3.0", Local: true, InstallDir: `C:\Users\me\.flobase`}}
	s.updates.latest = "0.4.0"
	v := s.updateView(context.Background())
	if v.Install != "local" || v.CanUpdate || v.ManualSteps != "flobase update" {
		t.Errorf("Windows desktop install: %+v", v)
	}
	s = &Server{cfg: Config{Version: "0.3.0", InstallDir: "/data/flobase"}}
	s.updates.latest = "0.4.0"
	if v := s.updateView(context.Background()); v.Install != "server" || v.ManualSteps != "cd <install dir> && sh upgrade.sh 0.4.0" {
		t.Errorf("server install: %+v", v)
	}
}
