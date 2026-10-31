package local

import "testing"

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
