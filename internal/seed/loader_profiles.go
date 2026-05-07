package seed

import (
	"context"
	"encoding/json"

	"github.com/samber/oops"
	"gopkg.in/yaml.v3"
)

// mockProfileEntry mirrors one entry in seed/mock_profiles.yaml.
type mockProfileEntry struct {
	Entity  string         `yaml:"entity"`
	IDType  string         `yaml:"id_type"`
	IDValue string         `yaml:"id_value"`
	Traits  map[string]any `yaml:"traits"`
}

// LoadMockProfiles parses seed/mock_profiles.yaml (a YAML list of profile
// entries) and upserts each into the mock_profiles table. The composite
// primary key (entity, id_type, id_value) ensures repeat runs are
// idempotent.
func (s *Seeder) LoadMockProfiles(ctx context.Context) error {
	body, err := s.readFile("mock_profiles.yaml")
	if err != nil {
		return err
	}
	var entries []mockProfileEntry
	if err := yaml.Unmarshal(body, &entries); err != nil {
		return oops.Wrapf(err, "parse mock_profiles.yaml")
	}
	if len(entries) == 0 {
		return nil
	}

	const stmt = `
		INSERT INTO mock_profiles (entity, id_type, id_value, traits, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, NOW())
		ON CONFLICT (entity, id_type, id_value)
		DO UPDATE SET traits = EXCLUDED.traits, updated_at = NOW()`

	for i, e := range entries {
		if e.Entity == "" || e.IDType == "" || e.IDValue == "" {
			return oops.With("index", i).Errorf("mock_profiles: entity, id_type, id_value required")
		}
		traits, err := json.Marshal(e.Traits)
		if err != nil {
			return oops.With("index", i).Wrapf(err, "marshal traits")
		}
		if _, err := s.pool.Exec(ctx, stmt, e.Entity, e.IDType, e.IDValue, traits); err != nil {
			return oops.
				With("entity", e.Entity).
				With("id_type", e.IDType).
				With("id_value", e.IDValue).
				Wrapf(err, "upsert mock_profiles")
		}
	}
	return nil
}
