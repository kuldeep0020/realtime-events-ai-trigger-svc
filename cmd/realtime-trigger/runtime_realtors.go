package main

import (
	"context"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/llm"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/rules"
)

// loadRealtorsFromPG loads the active realestate persona's full config YAML
// from Postgres and extracts the realtor roster. The roster is stored in
// `configs.config_yaml` for the realestate persona only — rs-self has no
// realtors.
//
// Returns nil (with no error) when the realestate config is missing or has
// no realtors — callers degrade gracefully (the dispatcher's SelectRealtor
// already handles a nil/empty roster by returning nil → empty realtor map).
func loadRealtorsFromPG(ctx context.Context, rt *runtime) ([]rules.RealtorEntry, error) {
	const q = `
		SELECT config_yaml
		FROM configs
		WHERE persona = $1 AND active = TRUE
		ORDER BY created_at DESC
		LIMIT 1`

	var raw string
	if err := rt.pool.QueryRow(ctx, q, llm.PersonaRealestate).Scan(&raw); err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	cfg, _, err := rules.LoadPersonaConfigYAML([]byte(raw))
	if err != nil {
		return nil, err
	}
	return cfg.Realtors, nil
}
