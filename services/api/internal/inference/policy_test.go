package inference

import (
	"strings"
	"testing"
)

func ptrF(v float64) *float64 { return &v }
func ptrI(v int) *int         { return &v }

func validReq() GenerationRequest {
	return GenerationRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are a support assistant."},
			{Role: RoleUser, Content: "What are your hours?"},
		},
	}
}

func TestValidateValid(t *testing.T) {
	s, err := ValidateRequest(validReq(), ResolvedSettings{Temperature: 0.3, TopP: 0.9, MaxTokens: 512}, DefaultPolicy())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *s.Temperature != 0.3 || *s.TopP != 0.9 || *s.MaxTokens != 512 {
		t.Fatalf("defaults not applied: %+v", s)
	}
}

func TestValidateEmptyMessages(t *testing.T) {
	for _, req := range []GenerationRequest{
		{},
		{Messages: []Message{}},
		{Messages: []Message{{Role: RoleUser, Content: "   "}}},
		{Messages: []Message{{Role: RoleSystem, Content: "hi"}}},
		{Messages: []Message{{Role: "admin", Content: "hi"}, {Role: RoleUser, Content: "hi"}}},
	} {
		if _, err := ValidateRequest(req, ResolvedSettings{}, DefaultPolicy()); err == nil {
			t.Fatalf("expected error for %+v", req)
		}
	}
}

func TestValidateParams(t *testing.T) {
	base := validReq()
	cases := []GenerationSettings{
		{Temperature: ptrF(-0.1)},
		{Temperature: ptrF(2.1)},
		{TopP: ptrF(0)},
		{TopP: ptrF(1.5)},
		{MaxTokens: ptrI(0)},
		{MaxTokens: ptrI(999999)},
		{Stop: []string{"a", "b", "c", "d", "e"}},
		{Stop: []string{""}},
		{Stop: []string{strings.Repeat("x", 51)}},
	}
	for i, s := range cases {
		req := base
		req.Settings = s
		if _, err := ValidateRequest(req, ResolvedSettings{Temperature: 0.3, TopP: 0.9, MaxTokens: 512}, DefaultPolicy()); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestValidatePromptLimit(t *testing.T) {
	req := GenerationRequest{Messages: []Message{{Role: RoleUser, Content: strings.Repeat("x", DefaultPolicy().MaxPromptChars+1)}}}
	if _, err := ValidateRequest(req, ResolvedSettings{}, DefaultPolicy()); err == nil {
		t.Fatal("expected prompt limit error")
	}
}

func TestStripThinking(t *testing.T) {
	out, stripped := StripThinking("<think>internal reasoning</think>Final answer.")
	if out != "Final answer." || !stripped {
		t.Fatalf("got %q stripped=%v", out, stripped)
	}
	out, stripped = StripThinking("No markers here.")
	if out != "No markers here." || stripped {
		t.Fatalf("got %q stripped=%v", out, stripped)
	}
	out, stripped = StripThinking("Prefix <think>dangling")
	if strings.Contains(out, "<think>") || !stripped {
		t.Fatalf("dangling opener leaked: %q", out)
	}
}
