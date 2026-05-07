package seed

import (
	"context"
	"encoding/json"

	"github.com/samber/oops"
)

// LoadTrackingPlans parses each persona's tracking_plans/<persona>.json and
// upserts one row per persona into tracking_plans (PK = persona).
func (s *Seeder) LoadTrackingPlans(ctx context.Context) error {
	for _, p := range personaConfigPaths {
		path := trackingPlanPath(p.Persona)
		body, err := s.readFile(path)
		if err != nil {
			return err
		}
		// Validate JSON before sending to PG so we surface parse errors with
		// a precise file-path tag rather than a SQL error.
		var probe any
		if err := json.Unmarshal(body, &probe); err != nil {
			return oops.With("path", path).Wrapf(err, "parse tracking plan JSON")
		}
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO tracking_plans (persona, spec)
			 VALUES ($1, $2::jsonb)
			 ON CONFLICT (persona)
			 DO UPDATE SET spec = EXCLUDED.spec`,
			p.Persona, body,
		); err != nil {
			return oops.With("persona", p.Persona).Wrapf(err, "upsert tracking_plans")
		}
	}
	return nil
}
