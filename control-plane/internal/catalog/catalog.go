// Package catalog holds what a project can be made of: engines, plans and
// regions. Plans and engines are fixed in code; regions are seeded into the
// platform database so operators can open or close them without a deploy.
package catalog

import "math"

// Engine is a database engine a project can run.
type Engine struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Licence     string `json:"licence"`
}

var Engines = []Engine{
	{ID: "postgres", Name: "Postgres 17", Description: "Postgres with pgvector. Full service set.", Licence: "PostgreSQL"},
	{ID: "mysql", Name: "MySQL 8.4 LTS", Description: "MySQL Community Server.", Licence: "GPLv2"},
	{ID: "mongodb", Name: "MongoDB-compatible", Description: "FerretDB 2 on Postgres + DocumentDB. Works with MongoDB drivers.", Licence: "Apache 2.0 / MIT"},
}

func EngineByID(id string) (Engine, bool) {
	for _, e := range Engines {
		if e.ID == id {
			return e, true
		}
	}
	return Engine{}, false
}

// Plan is a fixed size with a monthly price. Prices follow cost × markup,
// rounded to a .99 price point; EstCost is a rough estimate until OpenCost
// figures replace it.
type Plan struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	CPU         float64 `json:"cpu"`
	MemoryMB    int     `json:"memory_mb"`
	DiskGB      float64 `json:"disk_gb"`
	FilesGB     int     `json:"files_gb"`
	EgressGB    int     `json:"egress_gb"`
	Backups     string  `json:"backups"`
	EstCost     float64 `json:"est_cost_usd"`
	Price       float64 `json:"price_usd"`
	Description string  `json:"description"`
}

var Plans = []Plan{
	{ID: "free", Name: "Free", CPU: 0.25, MemoryMB: 256, DiskGB: 0.5, FilesGB: 1, EgressGB: 5, Backups: "None", EstCost: 1, Price: 0, Description: "Shared CPU with a 500 MB database, for trying FloBase."},
	{ID: "starter", Name: "Starter", CPU: 0.5, MemoryMB: 1024, DiskGB: 5, FilesGB: 10, EgressGB: 25, Backups: "Daily", EstCost: 2.5, Price: 3.99, Description: "Shared 0.5 vCPU for side projects."},
	{ID: "pro", Name: "Pro", CPU: 1, MemoryMB: 2048, DiskGB: 20, FilesGB: 50, EgressGB: 100, Backups: "7-day point-in-time", EstCost: 12, Price: 19, Description: "1 dedicated vCPU for production apps."},
	{ID: "business", Name: "Business", CPU: 2, MemoryMB: 8192, DiskGB: 100, FilesGB: 250, EgressGB: 250, Backups: "14-day point-in-time", EstCost: 75, Price: 120, Description: "2 vCPU with a high-availability standby."},
}

// PriceIn is the plan's monthly price in a region: its estimated cost there
// (EstCost × the region's multiplier) × markup, rounded up to a .99 price
// point. Free stays free.
func (p Plan) PriceIn(multiplier, markup float64) float64 {
	if p.Price == 0 {
		return 0
	}
	if multiplier <= 0 {
		multiplier = 1
	}
	// Round to cents first so 2.5 × 1.6 counts as 4, not 4.0000001.
	cost := math.Round(p.EstCost*multiplier*markup*100) / 100
	return math.Ceil(cost) - 0.01
}

func PlanByID(id string) (Plan, bool) {
	for _, p := range Plans {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
}

// Region is one location projects can run in. Besides the local region,
// each is a Docker host of its own (a region server, see the store).
type Region struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Location string `json:"location"`
	Status   string `json:"status"` // open, full, closed
	// PriceMultiplier is the region's cost relative to the estimate, for
	// servers that cost more in some places.
	PriceMultiplier float64 `json:"price_multiplier"`
}

// LocalRegion runs projects on the Docker host the control plane talks to.
// It is the only region until region servers are added.
var LocalRegion = Region{ID: "local", Name: "Local", Provider: "docker", Location: "This machine", Status: "open", PriceMultiplier: 1}
