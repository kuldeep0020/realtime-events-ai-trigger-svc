package dispatch_test

import (
	"testing"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/dispatch"
)

func TestLLMPayload_RoundTrip(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{"headline": "Test"}
	p := dispatch.NewLLMPayload("realestate_realtor_pitch", parsed, `{"headline":"Test"}`)
	if p.Template() != "realestate_realtor_pitch" {
		t.Errorf("template=%q", p.Template())
	}
	if p.Raw() != `{"headline":"Test"}` {
		t.Errorf("raw=%q", p.Raw())
	}
	if got := p.Parsed()["headline"]; got != "Test" {
		t.Errorf("parsed=%v", got)
	}
}

func TestLLMPayload_NilSafe(t *testing.T) {
	t.Parallel()
	var p *dispatch.LLMPayload
	if p.Template() != "" {
		t.Errorf("expected empty for nil receiver, got %q", p.Template())
	}
	if p.Parsed() != nil {
		t.Error("expected nil parsed for nil receiver")
	}
	if p.Raw() != "" {
		t.Error("expected empty raw for nil receiver")
	}
}
