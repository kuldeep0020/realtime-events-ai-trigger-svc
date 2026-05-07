package seed

import (
	"context"

	"github.com/samber/oops"
	"gopkg.in/yaml.v3"
)

// actionTemplateEntry mirrors one entry in seed/action-templates.yaml.
type actionTemplateEntry struct {
	Name           string `yaml:"name"`
	OutputFormat   string `yaml:"output_format"`
	SystemPrompt   string `yaml:"system_prompt"`
	UserPromptTmpl string `yaml:"user_prompt_tmpl"`
}

// LoadActionTemplates parses seed/action-templates.yaml (a YAML list) and
// upserts each row by `name` (UNIQUE constraint).
func (s *Seeder) LoadActionTemplates(ctx context.Context) error {
	body, err := s.readFile("action-templates.yaml")
	if err != nil {
		return err
	}
	var entries []actionTemplateEntry
	if err := yaml.Unmarshal(body, &entries); err != nil {
		return oops.Wrapf(err, "parse action-templates.yaml")
	}
	if len(entries) == 0 {
		return nil
	}

	const stmt = `
		INSERT INTO action_templates (name, system_prompt, user_prompt_tmpl, output_format)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name)
		DO UPDATE SET system_prompt = EXCLUDED.system_prompt,
		              user_prompt_tmpl = EXCLUDED.user_prompt_tmpl,
		              output_format = EXCLUDED.output_format`

	for i, e := range entries {
		if e.Name == "" {
			return oops.With("index", i).Errorf("action_templates: name required")
		}
		format := e.OutputFormat
		if format == "" {
			format = "json"
		}
		if _, err := s.pool.Exec(ctx, stmt, e.Name, e.SystemPrompt, e.UserPromptTmpl, format); err != nil {
			return oops.With("name", e.Name).Wrapf(err, "upsert action_templates")
		}
	}
	return nil
}
