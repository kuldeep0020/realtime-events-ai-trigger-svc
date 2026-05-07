package demofire_test

import (
	"encoding/json"
	"testing"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/demofire"
)

// TestProfileSpecs_HaveDistinctAnonIDs verifies that the realestate and
// rs-self profile specs have unique anonymousIds across their respective
// rotation tables.
func TestProfileSpecs_HaveDistinctAnonIDs(t *testing.T) {
	t.Parallel()

	// Realestate: 8 profiles, each with a unique anonID.
	reIDs := make(map[string]bool)
	for i := 0; i < 8; i++ {
		script := demofire.ScriptForPersonaIndex("realestate", i)
		if script == nil || len(script) == 0 {
			t.Fatalf("realestate index %d returned nil/empty script", i)
		}
		anonID := script[0].Event.AnonymousID
		if anonID == "" {
			t.Errorf("realestate index %d has empty anonymousId", i)
			continue
		}
		if reIDs[anonID] {
			t.Errorf("realestate index %d: duplicate anonID %q", i, anonID)
		}
		reIDs[anonID] = true
	}
	if len(reIDs) != 8 {
		t.Errorf("expected 8 distinct realestate anonIDs, got %d: %v", len(reIDs), reIDs)
	}

	// Rs-self: 3 profiles, each with a unique anonID.
	rsIDs := make(map[string]bool)
	for i := 0; i < 3; i++ {
		script := demofire.ScriptForPersonaIndex("rs-self", i)
		if script == nil || len(script) == 0 {
			t.Fatalf("rs-self index %d returned nil/empty script", i)
		}
		anonID := script[0].Event.AnonymousID
		if anonID == "" {
			t.Errorf("rs-self index %d has empty anonymousId", i)
			continue
		}
		if rsIDs[anonID] {
			t.Errorf("rs-self index %d: duplicate anonID %q", i, anonID)
		}
		rsIDs[anonID] = true
	}
	if len(rsIDs) != 3 {
		t.Errorf("expected 3 distinct rs-self anonIDs, got %d: %v", len(rsIDs), rsIDs)
	}
}

// TestRealestateScriptForProfile_PreservesShape verifies the structural shape
// of the script produced for each realestate profile. Profiles with 3 listings
// should have more steps than profiles with 2 listings.
func TestRealestateScriptForProfile_PreservesShape(t *testing.T) {
	t.Parallel()

	// Profiles 0-4 (index 0-4) have 3 listings — expect identify + page + 3
	// listing views + filter + dwell page + final filter = 8 steps.
	// Profiles 5-7 (index 5-7) have 2 listings — expect identify + page + 2
	// listing views + filter + dwell page + final filter = 7 steps.
	cases := []struct {
		idx      int
		wantLen  int
		desc     string
	}{
		{0, 8, "3-listing profile"},
		{1, 8, "3-listing profile"},
		{2, 8, "3-listing profile"},
		{3, 8, "3-listing profile"},
		{4, 8, "3-listing profile"},
		{5, 7, "2-listing profile"},
		{6, 7, "2-listing profile"},
		{7, 7, "2-listing profile"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc+"_"+string(rune('0'+tc.idx)), func(t *testing.T) {
			t.Parallel()
			steps := demofire.ScriptForPersonaIndex("realestate", tc.idx)
			if steps == nil {
				t.Fatalf("index %d: nil script", tc.idx)
			}
			if got := len(steps); got != tc.wantLen {
				t.Errorf("index %d: want %d steps, got %d", tc.idx, tc.wantLen, got)
			}
			// First step must be identify
			if steps[0].Event.Type != "identify" {
				t.Errorf("index %d: first step type=%q, want identify", tc.idx, steps[0].Event.Type)
			}
			// All steps must share the same anonymousId
			anonID := steps[0].Event.AnonymousID
			for j, s := range steps {
				if s.Event.AnonymousID != anonID {
					t.Errorf("index %d: step %d has anonID=%q, want %q", tc.idx, j, s.Event.AnonymousID, anonID)
				}
			}
		})
	}
}

// TestRealestateProfile003_IsAnonymous verifies that the third realestate
// profile (index 2 = "anon_demo-re-003") emits an identify event whose traits
// contain only membership_tier — no email, first_name, or last_name.
func TestRealestateProfile003_IsAnonymous(t *testing.T) {
	t.Parallel()

	steps := demofire.ScriptForPersonaIndex("realestate", 2)
	if len(steps) == 0 {
		t.Fatal("realestate index 2 returned empty script")
	}

	// First step must be identify
	if steps[0].Event.Type != "identify" {
		t.Fatalf("step 0 type=%q, want identify", steps[0].Event.Type)
	}
	if steps[0].Event.AnonymousID != "anon_demo-re-003" {
		t.Errorf("anonymousId=%q, want anon_demo-re-003", steps[0].Event.AnonymousID)
	}

	// Traits must have only membership_tier — no email, first_name, or last_name
	var traits map[string]any
	if err := json.Unmarshal(steps[0].Event.Traits, &traits); err != nil {
		t.Fatalf("unmarshal traits: %v", err)
	}
	if _, hasEmail := traits["email"]; hasEmail {
		t.Errorf("anonymous profile 003 should NOT have 'email' trait, but got traits: %v", traits)
	}
	if _, hasFirstName := traits["first_name"]; hasFirstName {
		t.Errorf("anonymous profile 003 should NOT have 'first_name' trait, but got traits: %v", traits)
	}
	if _, hasLastName := traits["last_name"]; hasLastName {
		t.Errorf("anonymous profile 003 should NOT have 'last_name' trait, but got traits: %v", traits)
	}
	if tier, ok := traits["membership_tier"].(string); !ok || tier != "browse" {
		t.Errorf("expected traits[membership_tier]=browse, got %v", traits["membership_tier"])
	}
}

// TestRealestateProfile001_IsKnown verifies the first known profile (Sarah
// Chen) has email/first_name/last_name in its identify traits.
func TestRealestateProfile001_IsKnown(t *testing.T) {
	t.Parallel()

	steps := demofire.ScriptForPersonaIndex("realestate", 0)
	if len(steps) == 0 {
		t.Fatal("realestate index 0 returned empty script")
	}
	if steps[0].Event.AnonymousID != "anon_demo-re-001" {
		t.Errorf("anonymousId=%q, want anon_demo-re-001", steps[0].Event.AnonymousID)
	}

	var traits map[string]any
	if err := json.Unmarshal(steps[0].Event.Traits, &traits); err != nil {
		t.Fatalf("unmarshal traits: %v", err)
	}
	for _, key := range []string{"email", "first_name", "last_name"} {
		if _, ok := traits[key]; !ok {
			t.Errorf("known profile 001 missing expected trait %q; traits: %v", key, traits)
		}
	}
	if email, _ := traits["email"].(string); email != "sarah.chen@stripe.com" {
		t.Errorf("email=%q, want sarah.chen@stripe.com", email)
	}
}

// TestRSSelfProfile_HasUserID verifies that rs-self profiles set UserID equal
// to AnonID per §6.3.
func TestRSSelfProfile_HasUserID(t *testing.T) {
	t.Parallel()

	for i := 0; i < 3; i++ {
		steps := demofire.ScriptForPersonaIndex("rs-self", i)
		if len(steps) == 0 {
			t.Fatalf("rs-self index %d: empty script", i)
		}
		// identify step
		id := steps[0]
		if id.Event.Type != "identify" {
			t.Fatalf("rs-self index %d: first step type=%q, want identify", i, id.Event.Type)
		}
		if id.Event.UserID == "" {
			t.Errorf("rs-self index %d: UserID is empty", i)
		}
		if id.Event.UserID != id.Event.AnonymousID {
			t.Errorf("rs-self index %d: UserID=%q != AnonID=%q", i, id.Event.UserID, id.Event.AnonymousID)
		}
	}
}

// TestScriptForPersonaIndex_WrapsAround verifies that idx > len(specs)
// wraps around via modulo rather than panicking.
func TestScriptForPersonaIndex_WrapsAround(t *testing.T) {
	t.Parallel()

	// idx=8 should equal idx=0 for realestate (8 profiles)
	s0 := demofire.ScriptForPersonaIndex("realestate", 0)
	s8 := demofire.ScriptForPersonaIndex("realestate", 8)
	if s0 == nil || s8 == nil {
		t.Fatal("nil script unexpected")
	}
	if s0[0].Event.AnonymousID != s8[0].Event.AnonymousID {
		t.Errorf("wrap-around anonID mismatch: s0=%q, s8=%q", s0[0].Event.AnonymousID, s8[0].Event.AnonymousID)
	}

	// idx=3 should equal idx=0 for rs-self (3 profiles)
	rs0 := demofire.ScriptForPersonaIndex("rs-self", 0)
	rs3 := demofire.ScriptForPersonaIndex("rs-self", 3)
	if rs0 == nil || rs3 == nil {
		t.Fatal("nil script unexpected")
	}
	if rs0[0].Event.AnonymousID != rs3[0].Event.AnonymousID {
		t.Errorf("wrap-around anonID mismatch: rs0=%q, rs3=%q", rs0[0].Event.AnonymousID, rs3[0].Event.AnonymousID)
	}
}
