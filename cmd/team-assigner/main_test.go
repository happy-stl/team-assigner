package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/grimwm/team-assigner/internal/survey"
)

// Four respondents, one section, one inevitable team: the output is
// fully determined by the spec (most first-choice votes wins ties
// to the left column; single team of four), so the exact bytes are
// a hand-checkable golden value.
const goldenVotes = `timestamp,email,section,Red,Blue,person match
2024-01-01,a@example.com,1,1,2,+b@example.com
2024-01-02,b@example.com,1,2,1,+a@example.com
2024-01-03,c@example.com,1,1,2,
2024-01-04,d@example.com,1,2,1,
`

const goldenTeams = "Team,Person 1,Person 2,Person 3,Person 4\n" +
	"Red,a@example.com,b@example.com,c@example.com,d@example.com\n" +
	"\nseed,11\n"

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAssignGolden(t *testing.T) {
	in := writeTemp(t, "votes.csv", goldenVotes)
	out := filepath.Join(t.TempDir(), "teams.csv")
	if code := run([]string{"assign", "-input", in, "-output", out, "-seed", "11"}); code != 0 {
		t.Fatalf("assign exit = %d", code)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != goldenTeams {
		t.Fatalf("teams.csv =\n%q\nwant\n%q", got, goldenTeams)
	}
}

func TestAssignPositionalArgs(t *testing.T) {
	in := writeTemp(t, "votes.csv", goldenVotes)
	out := filepath.Join(t.TempDir(), "teams.csv")
	if code := run([]string{"assign", in, out, "-seed", "11"}); code != 0 {
		t.Fatalf("assign exit = %d", code)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != goldenTeams {
		t.Fatalf("teams.csv =\n%q\nwant\n%q", got, goldenTeams)
	}
}

func TestValidate(t *testing.T) {
	in := writeTemp(t, "votes.csv", goldenVotes)
	if code := run([]string{"validate", "-input", in}); code != 0 {
		t.Fatalf("validate exit = %d", code)
	}
	bad := writeTemp(t, "bad.csv", `timestamp,email,section,Red,person match
2024-01-01,a@example.com,1,1,ghost@example.com
`)
	if code := run([]string{"validate", "-input", bad}); code == 0 {
		t.Fatalf("validate of unknown exclusion exit = %d, want nonzero", code)
	}
}

func TestBadInvocations(t *testing.T) {
	if code := run([]string{}); code == 0 {
		t.Fatalf("empty args exit = %d, want nonzero", code)
	}
	if code := run([]string{"bogus"}); code == 0 {
		t.Fatalf("bogus command exit = %d, want nonzero", code)
	}
	if code := run([]string{"assign", "-input", "nonexistent.csv", "-output", "x"}); code == 0 {
		t.Fatalf("missing input exit = %d, want nonzero", code)
	}
	if code := run([]string{"assign", "-input", "only"}); code == 0 {
		t.Fatalf("missing output exit = %d, want nonzero", code)
	}
	if code := run([]string{"assign", "-seed", "abc", "-input", "x", "-output", "y"}); code == 0 {
		t.Fatalf("bad seed exit = %d, want nonzero", code)
	}
	if code := run([]string{"validate"}); code == 0 {
		t.Fatalf("validate without input exit = %d, want nonzero", code)
	}
}

// TestSampleEndToEnd runs the committed example file through the
// CLI and re-checks every hard rule against the output: each team
// has 3-4 members from one section, nobody sits on a vetoed project
// or beside an excluded person, mutual '+' pairs stay together, and
// everyone is seated exactly once.
func TestSampleEndToEnd(t *testing.T) {
	in := filepath.Join("..", "..", "testdata", "sample_votes.csv")
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	s, err := survey.Parse(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "teams.csv")
	if code := run([]string{"assign", "-input", in, "-output", out, "-seed", "1"}); code != 0 {
		t.Fatalf("assign exit = %d", code)
	}
	rows := readCSV(t, out)
	if len(rows) == 0 || rows[0][0] != "Team" {
		t.Fatalf("missing header row, got %v", rows)
	}
	for i, cell := range rows[0][1:] {
		if want := "Person " + itoa(i+1); cell != want {
			t.Fatalf("header[%d] = %q, want %q", i+1, cell, want)
		}
	}

	teamIdx := map[string]int{}
	for i, name := range s.Teams {
		teamIdx[name] = i
	}
	seated := map[string]bool{}
	teamOf := map[string][]string{} // member key -> row emails
	teamRows := rows[1:]
	// The file ends with a blank line and a seed,<n> footer row;
	// encoding/csv already dropped the blank line.
	footer := teamRows[len(teamRows)-1]
	if len(footer) != 2 || footer[0] != "seed" || footer[1] != "1" {
		t.Fatalf("footer = %v, want [seed 1]", footer)
	}
	for _, row := range teamRows[:len(teamRows)-1] {
		if len(row) != len(rows[0]) {
			t.Fatalf("row %v has %d cells, want %d (header width)", row, len(row), len(rows[0]))
		}
		proj, ok := teamIdx[row[0]]
		if !ok {
			t.Fatalf("unknown team %q", row[0])
		}
		members := nonBlank(row[1:])
		if len(members) < 3 || len(members) > 4 {
			t.Fatalf("team %q has %d members, want 3-4", row[0], len(members))
		}
		var section string
		for j, email := range members {
			p, ok := s.ByKey[key(email)]
			if !ok {
				t.Fatalf("unknown email %q in output", email)
			}
			if seated[p.Key] {
				t.Fatalf("%s seated twice", email)
			}
			seated[p.Key] = true
			if j == 0 {
				section = p.Section
			} else if p.Section != section {
				t.Fatalf("team %q mixes sections", row[0])
			}
			if r := p.Ranks[proj]; r != nil && *r == -1 {
				t.Fatalf("%s placed on vetoed team %q", email, row[0])
			}
			teamOf[p.Key] = members
		}
		for a := 0; a < len(members); a++ {
			for b := a + 1; b < len(members); b++ {
				pa, pb := s.ByKey[key(members[a])], s.ByKey[key(members[b])]
				if pa.Exclude == pb.Key || pb.Exclude == pa.Key {
					t.Fatalf("excluded pair %s, %s together", members[a], members[b])
				}
			}
		}
	}
	if len(seated) != len(s.People) {
		t.Fatalf("seated %d of %d", len(seated), len(s.People))
	}
	for _, p := range s.People {
		if q := s.MutualPartner(p); q != nil {
			a, b := teamOf[p.Key], teamOf[q.Key]
			if len(a) == 0 || len(b) == 0 || a[0] != b[0] || !sameSet(a, b) {
				t.Fatalf("mutual pair %s, %s split", p.Email, q.Email)
			}
		}
	}
}

func key(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// nonBlank drops padding cells from rectangular output rows.
func nonBlank(cells []string) []string {
	var out []string
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			out = append(out, c)
		}
	}
	return out
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[key(x)]++
	}
	for _, x := range b {
		if seen[key(x)] == 0 {
			return false
		}
		seen[key(x)]--
	}
	return true
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // rows hold a team plus 3-4 emails
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestSplitArgs(t *testing.T) {
	// Flags keep working after positionals: "assign IN OUT -seed 1"
	// must not silently drop the seed.
	flags, pos := splitArgs([]string{"in.csv", "out.csv", "-seed", "11"})
	if len(pos) != 2 || pos[0] != "in.csv" || pos[1] != "out.csv" {
		t.Fatalf("pos = %v", pos)
	}
	if len(flags) != 2 || flags[0] != "-seed" || flags[1] != "11" {
		t.Fatalf("flags = %v", flags)
	}
	// -flag=value stays one token; -- ends flag parsing.
	flags, pos = splitArgs([]string{"-seed=7", "--", "-seed"})
	if len(flags) != 1 || flags[0] != "-seed=7" {
		t.Fatalf("flags = %v", flags)
	}
	if len(pos) != 1 || pos[0] != "-seed" {
		t.Fatalf("pos = %v", pos)
	}
}

func TestResolveMinSize(t *testing.T) {
	// Unset (0) defaults to one less than the max...
	if got := resolveMinSize(0, 4); got != 3 {
		t.Fatalf("resolveMinSize(0, 4) = %d, want 3", got)
	}
	if got := resolveMinSize(0, 5); got != 4 {
		t.Fatalf("resolveMinSize(0, 5) = %d, want 4", got)
	}
	// ...floored at 1, and an explicit value always wins.
	if got := resolveMinSize(0, 1); got != 1 {
		t.Fatalf("resolveMinSize(0, 1) = %d, want 1", got)
	}
	if got := resolveMinSize(2, 5); got != 2 {
		t.Fatalf("resolveMinSize(2, 5) = %d, want 2", got)
	}
}

func TestSizeFlags(t *testing.T) {
	ten := `timestamp,email,section,Red,Blue,person match
2024-01-01,a@example.com,1,1,2,
2024-01-02,b@example.com,1,1,2,
2024-01-03,c@example.com,1,1,2,
2024-01-04,d@example.com,1,1,2,
2024-01-05,e@example.com,1,1,2,
2024-01-06,f@example.com,1,2,1,
2024-01-07,g@example.com,1,2,1,
2024-01-08,h@example.com,1,2,1,
2024-01-09,i@example.com,1,2,1,
2024-01-10,j@example.com,1,2,1,
`
	in := writeTemp(t, "votes.csv", ten)
	out := filepath.Join(t.TempDir(), "teams.csv")
	// Ten people need teams of five: allowed with -max-size 5...
	if code := run([]string{"assign", "-input", in, "-output", out,
		"-seed", "1", "-max-size", "5"}); code != 0 {
		t.Fatalf("assign -max-size 5 exit = %d", code)
	}
	rows := readCSV(t, out)
	if len(rows) != 4 || rows[0][0] != "Team" {
		t.Fatalf("want header + 2 teams of five + seed footer, got %v", rows)
	}
	// ...while a bound the data cannot meet remaps instead of
	// failing: ten people cannot split into teams of exactly 3.
	if code := run([]string{"assign", "-input", in, "-output", out,
		"-seed", "1", "-max-size", "3", "-min-size", "3"}); code == 0 {
		t.Fatalf("assign 3-3 on ten exit = 0, want remap exit 1")
	}
	rows = readCSV(t, out)
	seated := 0
	for _, row := range rows[1:] {
		if row[0] == "seed" {
			continue
		}
		seated += len(nonBlank(row[1:]))
	}
	if seated != 10 {
		t.Fatalf("remap seated %d of 10", seated)
	}
	// -min-size above -max-size is rejected.
	if code := run([]string{"validate", "-input", in,
		"-max-size", "3", "-min-size", "4"}); code == 0 {
		t.Fatalf("validate min>max exit = 0, want nonzero")
	}
}

func TestRemapEndToEnd(t *testing.T) {
	// Five people cannot split into teams of 3-4: assign writes the
	// best-effort remap (one team of five) and exits 1 with the why
	// on stderr, while validate predicts the fallback.
	in := writeTemp(t, "votes.csv", `timestamp,email,section,Red,Blue,person match
2024-01-01,a@example.com,1,1,2,
2024-01-02,b@example.com,1,1,2,
2024-01-03,c@example.com,1,2,1,
2024-01-04,d@example.com,1,2,1,
2024-01-05,e@example.com,1,1,2,
`)
	out := filepath.Join(t.TempDir(), "teams.csv")
	if code := run([]string{"assign", "-input", in, "-output", out, "-seed", "1"}); code != 1 {
		t.Fatalf("5-person section exit = %d, want 1 (remap written)", code)
	}
	rows := readCSV(t, out)
	if len(rows) != 3 || len(nonBlank(rows[1])) != 6 {
		t.Fatalf("want header + one team of five + seed footer, got %v", rows)
	}
	if code := run([]string{"validate", "-input", in}); code == 0 {
		t.Fatalf("validate of 5-person section exit = %d, want nonzero", code)
	}
}

func TestOverrideCLI(t *testing.T) {
	// Teacher override through the CLI: a lands on Red despite
	// ranking Blue first; an unknown team fails validation.
	in := writeTemp(t, "votes.csv", `timestamp,email,section,Red,Blue,person match,override
2024-01-01,a@example.com,1,3,1,,Red
2024-01-02,b@example.com,1,1,2,,
2024-01-03,c@example.com,1,1,2,,
2024-01-04,d@example.com,1,2,1,,
`)
	out := filepath.Join(t.TempDir(), "teams.csv")
	if code := run([]string{"assign", "-input", in, "-output", out, "-seed", "1"}); code != 0 {
		t.Fatalf("assign exit = %d", code)
	}
	rows := readCSV(t, out)
	found := false
	for _, row := range rows[1:] {
		for _, email := range nonBlank(row[1:]) {
			if key(email) == "a@example.com" {
				found = true
				if row[0] != "Red" {
					t.Fatalf("a on %q, want Red", row[0])
				}
			}
		}
	}
	if !found {
		t.Fatalf("a missing from output")
	}

	bad := writeTemp(t, "bad.csv", `timestamp,email,section,Red,Blue,person match,override
2024-01-01,a@example.com,1,1,2,,Green
`)
	if code := run([]string{"validate", "-input", bad}); code == 0 {
		t.Fatalf("validate of unknown override exit = %d, want nonzero", code)
	}
	if code := run([]string{"assign", "-input", bad, "-output", out, "-seed", "1"}); code == 0 {
		t.Fatalf("assign of unknown override exit = %d, want nonzero", code)
	}
}

func TestValidateVetoAll(t *testing.T) {
	// Someone who vetoed every project is guaranteed a relaxed
	// placement: validate flags it up front.
	in := writeTemp(t, "votes.csv", `timestamp,email,section,Red,Blue,person match
2024-01-01,a@example.com,1,-1,-1,
2024-01-02,b@example.com,1,1,2,
2024-01-03,c@example.com,1,1,2,
`)
	if code := run([]string{"validate", "-input", in}); code == 0 {
		t.Fatalf("validate of veto-all exit = %d, want nonzero", code)
	}
	out := filepath.Join(t.TempDir(), "teams.csv")
	if code := run([]string{"assign", "-input", in, "-output", out, "-seed", "1"}); code != 1 {
		t.Fatalf("assign of veto-all exit = %d, want 1 (remap written)", code)
	}
	rows := readCSV(t, out)
	if len(rows) != 3 || len(nonBlank(rows[1])) != 4 {
		t.Fatalf("want header + one team of three + seed footer, got %v", rows)
	}
}
