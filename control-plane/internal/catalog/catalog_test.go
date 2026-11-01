package catalog

import "testing"

func TestPriceIn(t *testing.T) {
	starter, _ := PlanByID("starter")
	pro, _ := PlanByID("pro")
	free, _ := PlanByID("free")
	for _, c := range []struct {
		plan       Plan
		multiplier float64
		want       float64
	}{
		{starter, 1, 3.99},   // 2.5 × 1.6 = 4
		{starter, 1.2, 4.99}, // 4.8
		{pro, 1, 19.99},      // 19.2
		{free, 2, 0},
	} {
		if got := c.plan.PriceIn(c.multiplier, 1.6); got != c.want {
			t.Errorf("%s × %v = %v, want %v", c.plan.ID, c.multiplier, got, c.want)
		}
	}
}
