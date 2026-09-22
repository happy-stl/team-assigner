package survey

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, text string) *Survey {
	t.Helper()
	s, err := Parse(strings.NewReader(text))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	return s
}

func TestParseLayout(t *testing.T) {
	s := mustParse(t, `timestamp,email,section,Alpha,Beta,person match
2024-01-01,a@example.com,1,1,2,
2024-01-02,b@example.com,1,2,1,c@example.com
2024-01-03,c@example.com,2,-1,,+a@example.com
`)
	if len(s.Teams) != 2 || s.Teams[0] != "Alpha" || s.Teams[1] != "Beta" {
		t.Fatalf("teams = %v", s.Teams)
	}
	if len(s.People) != 3 {
		t.Fatalf("people = %d", len(s.People))
	}
	a := s.ByKey["a@example.com"]
	if a.Section != "1" || a.Exclude != "" || a.Prefer != "" {
		t.Fatalf("a = %+v", a)
	}
	if got := *a.Ranks[0]; got != 1 {
		t.Fatalf("a.Alpha = %d", got)
	}
	b := s.ByKey["b@example.com"]
	if b.Exclude != "c@example.com" {
		t.Fatalf("b.Exclude = %q", b.Exclude)
	}
	c := s.ByKey["c@example.com"]
	if c.Ranks[0] == nil || *c.Ranks[0] != -1 {
		t.Fatalf("c.Alpha veto missing: %+v", c.Ranks[0])
	}
	if c.Ranks[1] != nil {
		t.Fatalf("c.Beta should be blank (no opinion)")
	}
	if c.Prefer != "a@example.com" {
		t.Fatalf("c.Prefer = %q", c.Prefer)
	}
}

func TestParseSkipsBlankRowsAndShortRows(t *testing.T) {
	s := mustParse(t, `timestamp,email,section,Alpha,person match

2024-01-01,a@example.com,1,1
2024-01-02,b@example.com,1,2,
`)
	if len(s.People) != 2 {
		t.Fatalf("people = %d", len(s.People))
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"empty":        ``,
		"header only":  "timestamp,email,section,Alpha,person match\n",
		"too few cols": "a,b,c\n1,2,3\n",
		"empty team":   "timestamp,email,section,,person match\n",
		"dup email": `timestamp,email,section,Alpha,person match
2024-01-01,A@example.com,1,1,
2024-01-02,a@example.com,1,2,
`,
		"missing email": `timestamp,email,section,Alpha,person match
2024-01-01,,1,1,
`,
		"rank zero": `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,0,
`,
		"rank -2": `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,-2,
`,
		"rank text": `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,best,
`,
		"bare plus": `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,1,+,
`,
		"long row": `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,1,,extra,
`,
	}
	for name, text := range cases {
		if _, err := Parse(strings.NewReader(text)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestValidate(t *testing.T) {
	valid := mustParse(t, `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,1,+b@example.com
2024-01-02,b@example.com,1,1,+a@example.com
2024-01-03,c@example.com,1,1,a@example.com
`)
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	if got := valid.MutualPartner(valid.ByKey["a@example.com"]); got == nil || got.Key != "b@example.com" {
		t.Fatalf("mutual partner missing")
	}
	// One-sided '+' is not a mutual partnership.
	if got := valid.MutualPartner(valid.ByKey["c@example.com"]); got != nil {
		t.Fatalf("one-sided want should not bind: %v", got)
	}

	invalid := map[string]string{
		"unknown exclusion": `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,1,ghost@example.com
`,
		"unknown want": `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,1,+ghost@example.com
`,
		"self exclusion": `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,1,a@example.com
`,
		"self want": `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,1,+a@example.com
`,
		"cross-section mutual want": `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,1,1,+b@example.com
2024-01-02,b@example.com,2,1,+a@example.com
`,
	}
	for name, text := range invalid {
		s := mustParse(t, text)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestEmptySectionIsShared(t *testing.T) {
	s := mustParse(t, `timestamp,email,section,Alpha,person match
2024-01-01,a@example.com,,1,
2024-01-02,b@example.com,,1,
`)
	if s.ByKey["a@example.com"].Section != "" {
		t.Fatalf("empty section should stay empty (one shared section)")
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
}
