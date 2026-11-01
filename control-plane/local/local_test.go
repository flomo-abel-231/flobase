package local

import (
	"os"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.1", true},
		{"0.1.10", "0.1.9", true},
		{"0.1.1", "0.1.1", false},
		{"0.1.0", "0.2.0", false},
		{"0.2.0", "dev", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSumFor(t *testing.T) {
	list := "aaa  lima-2.2.1-Darwin-arm64.tar.gz\n3082737f *colima-Darwin-x86_64\n"
	if got := sumFor(list, "colima-Darwin-x86_64"); got != "3082737f" {
		t.Errorf("starred name: got %q", got)
	}
	if got := sumFor(list, "lima-2.2.1-Darwin-arm64.tar.gz"); got != "aaa" {
		t.Errorf("plain name: got %q", got)
	}
	single := "0123456789012345678901234567890123456789012345678901234567890123\n"
	if got := sumFor(single, ""); len(got) != 64 {
		t.Errorf("single hash: got %q", got)
	}
	if got := sumFor(list, "missing"); got != "" {
		t.Errorf("missing: got %q", got)
	}
}

func TestMigrateEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no install directory on Windows")
	}
	m := &Manager{Dir: t.TempDir(), Name: "flobase-test"}
	// What 0.3.0 and before wrote.
	old := "FLOBASE_VERSION=0.2.1\nFLOBASE_AUTO_UPDATE=false\nFLOBASE_INSTALL_DIR=\nFLOBASE_NAME=flobase-local\n"
	if err := os.WriteFile(m.envPath(), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.migrateEnv(); err != nil {
		t.Fatal(err)
	}
	env := m.Env()
	want := map[string]string{
		"FLOBASE_VERSION": "0.2.1", "FLOBASE_AUTO_UPDATE": "true", "FLOBASE_INSTALL_DIR": m.Dir,
		"FLOBASE_NAME": "flobase-test", "COMPOSE_PROJECT_NAME": "flobase-test", "FLOBASE_INSTALL_KIND": "local",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	// Turned off by hand afterwards: stays off.
	if err := m.setEnv(map[string]string{"FLOBASE_AUTO_UPDATE": "false"}); err != nil {
		t.Fatal(err)
	}
	if err := m.migrateEnv(); err != nil {
		t.Fatal(err)
	}
	if got := m.Env()["FLOBASE_AUTO_UPDATE"]; got != "false" {
		t.Errorf("auto-update turned back on: %q", got)
	}
}
