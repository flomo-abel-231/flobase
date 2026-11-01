package api

import "testing"

func TestSizeState(t *testing.T) {
	const mb = 1_000_000
	for _, c := range []struct {
		used, limit int64
		current     string
		want        string
	}{
		{100 * mb, 500 * mb, "", sizeOK},
		{400 * mb, 500 * mb, "", sizeWarn80},
		{460 * mb, 500 * mb, sizeWarn80, sizeWarn90},
		{500 * mb, 500 * mb, sizeWarn90, sizeReadOnly},
		{480 * mb, 500 * mb, sizeReadOnly, sizeReadOnly}, // stays until under 95%
		{470 * mb, 500 * mb, sizeReadOnly, sizeWarn90},
		{10 * mb, 5000 * mb, sizeReadOnly, sizeOK}, // a bigger plan lifts it
		{10 * mb, 0, "", sizeOK},
	} {
		if got := sizeState(c.used, c.limit, c.current); got != c.want {
			t.Errorf("sizeState(%d, %d, %q) = %q, want %q", c.used, c.limit, c.current, got, c.want)
		}
	}
}
