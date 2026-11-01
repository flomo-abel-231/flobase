package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"flobase/control-plane/internal/catalog"
	"flobase/control-plane/internal/store"
)

func TestPlanOpen(t *testing.T) {
	free, _ := catalog.PlanByID("free")
	pro, _ := catalog.PlanByID("pro")
	req := func(id identity) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		return r.WithContext(context.WithValue(r.Context(), identityKey{}, id))
	}
	member := identity{User: &store.User{Role: "member"}}
	owner := identity{User: &store.User{Role: "owner"}}
	token := identity{}
	for _, c := range []struct {
		mode string
		id   identity
		plan catalog.Plan
		want bool
	}{
		{"self-hosted", member, pro, true},
		{"cloud", member, free, true},
		{"cloud", member, pro, false},
		{"cloud", owner, pro, true},
		{"cloud", token, pro, true},
	} {
		s := &Server{cfg: Config{Mode: c.mode}}
		if got := s.planOpen(req(c.id), c.plan); got != c.want {
			t.Errorf("%s %s %s: planOpen = %v, want %v", c.mode, c.id.role(), c.plan.ID, got, c.want)
		}
	}
}
