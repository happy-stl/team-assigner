package assign

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"

	"github.com/grimwm/team-assigner/internal/survey"
)

func mustSurvey(t *testing.T, text string) *survey.Survey {
	t.Helper()
	s, err := survey.Parse(strings.NewReader(text))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	return s
}

// checkInvariants verifies the hard contract on any result: every
// respondent seated exactly once, teams only within a section,
// sizes 3-4, no veto or exclusion violated, mutual '+' pairs together.
func checkInvariants(t *testing.T, s *survey.Survey, slots []*Slot, report *Report, minSize, maxSize int) {
	t.Helper()
	if !report.Clean() {
		t.Fatalf("expected strict split, report relaxes: %q", report.Describe())
	}
	seen := map[string]string{} // email key -> team label
	for _, slot := range slots {
		if len(slot.Members) < minSize || len(slot.Members) > maxSize {
			t.Fatalf("team %q size %d, want %d-%d",
				s.Teams[slot.Project], len(slot.Members), minSize, maxSize)
		}
		for _, m := range slot.Members {
			if m.Section != slot.Section {
				t.Fatalf("%s seated in section %q, belongs to %q",
					m.Email, slot.Section, m.Section)
			}
			if r := m.Ranks[slot.Project]; r != nil && *r == -1 {
				t.Fatalf("%s placed on vetoed team %q",
					m.Email, s.Teams[slot.Project])
			}
			label := s.Teams[slot.Project] + "\x00" + slot.Section
			if prev, dup := seen[m.Key]; dup {
				t.Fatalf("%s seated twice (%q and %q)", m.Email, prev, label)
			}
			seen[m.Key] = label
		}
		for i := 0; i < len(slot.Members); i++ {
			for j := i + 1; j < len(slot.Members); j++ {
				a, b := slot.Members[i], slot.Members[j]
				if a.Exclude == b.Key || b.Exclude == a.Key {
					t.Fatalf("excluded pair %s, %s on team %q",
						a.Email, b.Email, s.Teams[slot.Project])
				}
			}
		}
	}
	if len(seen) != len(s.People) {
		t.Fatalf("seated %d of %d respondents", len(seen), len(s.People))
	}
	for _, p := range s.People {
		if q := s.MutualPartner(p); q != nil && seen[p.Key] != seen[q.Key] {
			t.Fatalf("mutual pair %s, %s split across teams", p.Email, q.Email)
		}
	}
}

func teamOf(slots []*Slot, key string) *Slot {
	for _, slot := range slots {
		for _, m := range slot.Members {
			if m.Key == key {
				return slot
			}
		}
	}
	return nil
}

func TestPartition(t *testing.T) {
	// n -> (min, max) -> want team count; sizes must sum to n and
	// stay within bounds.
	cases := []struct {
		n, min, max, teams int
	}{
		{3, 3, 4, 1}, {4, 3, 4, 1}, {6, 3, 4, 2}, {7, 3, 4, 2},
		{8, 3, 4, 2}, {9, 3, 4, 3}, {10, 3, 4, 3}, {11, 3, 4, 3},
		{12, 3, 4, 3}, {13, 3, 4, 4}, {16, 3, 4, 4},
		// Wider bounds use fewer, larger teams.
		{10, 3, 5, 2}, {12, 3, 5, 3}, {15, 3, 5, 3},
		{9, 3, 3, 3}, {8, 2, 2, 4}, {7, 1, 7, 1},
		{5, 2, 4, 2}, {6, 2, 3, 2},
	}
	for _, c := range cases {
		got, err := Partition(c.n, c.min, c.max)
		if err != nil {
			t.Fatalf("Partition(%d, %d, %d) = %v", c.n, c.min, c.max, err)
		}
		sum := 0
		for _, size := range got {
			if size < c.min || size > c.max {
				t.Fatalf("Partition(%d, %d, %d) has size %d",
					c.n, c.min, c.max, size)
			}
			sum += size
		}
		if sum != c.n || len(got) != c.teams {
			t.Fatalf("Partition(%d, %d, %d) = %v, want %d teams",
				c.n, c.min, c.max, got, c.teams)
		}
	}
	// Fewest teams wins: 12 in 3-5 is 5+4+3, not four 3s.
	if got, _ := Partition(12, 3, 5); len(got) != 3 || got[0] != 5 {
		t.Fatalf("Partition(12, 3, 5) = %v, want larger teams first", got)
	}
	bad := []struct{ n, min, max int }{
		{0, 3, 4}, {1, 3, 4}, {2, 3, 4}, {5, 3, 4},
		{4, 3, 2}, {7, 0, 4}, {6, 4, 4}, {9, 2, 2},
	}
	for _, c := range bad {
		if _, err := Partition(c.n, c.min, c.max); err == nil {
			t.Fatalf("Partition(%d, %d, %d): expected error, got nil",
				c.n, c.min, c.max)
		}
	}
}

func TestCustomSizesAssign(t *testing.T) {
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
	// Max 5 fits ten people in two teams of five.
	s := mustSurvey(t, ten)
	slots, report, err := Assign(s, rand.New(rand.NewSource(1)), 4, 5)
	if err != nil {
		t.Fatalf("Assign min 4 max 5 = %v", err)
	}
	checkInvariants(t, s, slots, report, 4, 5)
	if len(slots) != 2 {
		t.Fatalf("teams = %d, want 2", len(slots))
	}

	// Min 2 lets a five-person section split at all.
	five := `timestamp,email,section,Red,Blue,person match
2024-01-01,a@example.com,1,1,2,
2024-01-02,b@example.com,1,1,2,
2024-01-03,c@example.com,1,2,1,
2024-01-04,d@example.com,1,2,1,
2024-01-05,e@example.com,1,1,2,
`
	s5 := mustSurvey(t, five)
	slots5, report5, err := Assign(s5, rand.New(rand.NewSource(1)), 2, 4)
	if err != nil {
		t.Fatalf("Assign min 2 max 4 = %v", err)
	}
	checkInvariants(t, s5, slots5, report5, 2, 4)

	// ...while the default 3-4 remaps it to one team of 5 (kept
	// together, one head over max) and says so.
	slots53, report53, err := Assign(s5, rand.New(rand.NewSource(1)), 3, 4)
	if err != nil {
		t.Fatalf("Assign min 3 max 4 = %v", err)
	}
	if report53.Clean() {
		t.Fatalf("5-person section at 3-4 should remap, got clean report")
	}
	if len(report53.Sizing) != 1 {
		t.Fatalf("want one sizing note, got %+v", report53.Sizing)
	}
	if sizes := report53.Sizing[0].Sizes; len(sizes) != 1 || sizes[0] != 5 {
		t.Fatalf("want one team of 5, got %v", sizes)
	}
	seated := 0
	for _, slot := range slots53 {
		seated += len(slot.Members)
	}
	if seated != 5 {
		t.Fatalf("seated %d of 5", seated)
	}
}

// Six respondents, one section, three projects: the smallest case
// with two full teams.
const sixPerson = `timestamp,email,section,Red,Blue,Green,person match
2024-01-01,a@example.com,1,1,2,3,
2024-01-02,b@example.com,1,1,3,2,
2024-01-03,c@example.com,1,2,1,3,
2024-01-04,d@example.com,1,3,1,2,
2024-01-05,e@example.com,1,3,2,1,
2024-01-06,f@example.com,1,2,3,1,
`

func TestAssignSmall(t *testing.T) {
	s := mustSurvey(t, sixPerson)
	slots, report, err := Assign(s, rand.New(rand.NewSource(7)), 3, 4)
	if err != nil {
		t.Fatalf("Assign = %v", err)
	}
	if len(slots) != 2 {
		t.Fatalf("teams = %d, want 2", len(slots))
	}
	checkInvariants(t, s, slots, report, 3, 4)
	// Red has the only double first-choice vote, so it loads first.
	if s.Teams[slots[0].Project] != "Red" {
		t.Fatalf("first loaded = %q, want Red", s.Teams[slots[0].Project])
	}
}

func TestSeedReproducible(t *testing.T) {
	run := func() string {
		s := mustSurvey(t, sixPerson)
		slots, report, err := Assign(s, rand.New(rand.NewSource(42)), 3, 4)
		if err != nil {
			t.Fatalf("Assign = %v", err)
		}
		if !report.Clean() {
			t.Fatalf("six-person split should be strict: %q", report.Describe())
		}
		var buf bytes.Buffer
		if err := WriteCSV(&buf, s, slots); err != nil {
			t.Fatalf("WriteCSV = %v", err)
		}
		return buf.String()
	}
	if a, b := run(), run(); a != b {
		t.Fatalf("same seed gave different output:\n%s\n%s", a, b)
	}
}

func TestVetoAndExclusionRespected(t *testing.T) {
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,Green,person match
2024-01-01,a@example.com,1,1,2,3,b@example.com
2024-01-02,b@example.com,1,1,2,3,
2024-01-03,c@example.com,1,-1,1,2,
2024-01-04,d@example.com,1,2,-1,1,
2024-01-05,e@example.com,1,3,1,-1,
2024-01-06,f@example.com,1,2,3,1,
2024-01-07,g@example.com,1,3,2,1,
`)
	for _, seed := range []int64{1, 2, 3, 4, 5} {
		slots, report, err := Assign(s, rand.New(rand.NewSource(seed)), 3, 4)
		if err != nil {
			t.Fatalf("seed %d: Assign = %v", seed, err)
		}
		checkInvariants(t, s, slots, report, 3, 4)
		// a excludes b: never on the same team, whatever the seed.
		if teamOf(slots, "a@example.com") == teamOf(slots, "b@example.com") {
			t.Fatalf("seed %d: excluded a and b teamed together", seed)
		}
	}
}

func TestMutualPlusStaysTogether(t *testing.T) {
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,person match
2024-01-01,a@example.com,1,1,2,+b@example.com
2024-01-02,b@example.com,1,2,1,+a@example.com
2024-01-03,c@example.com,1,1,2,
2024-01-04,d@example.com,1,2,1,
`)
	for _, seed := range []int64{1, 9, 99} {
		slots, report, err := Assign(s, rand.New(rand.NewSource(seed)), 3, 4)
		if err != nil {
			t.Fatalf("seed %d: Assign = %v", seed, err)
		}
		checkInvariants(t, s, slots, report, 3, 4)
		if teamOf(slots, "a@example.com") != teamOf(slots, "b@example.com") {
			t.Fatalf("seed %d: mutual pair split", seed)
		}
	}
}

func TestSectionsNeverMix(t *testing.T) {
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,Green,person match
2024-01-01,a1@example.com,A,1,2,3,
2024-01-02,a2@example.com,A,1,2,3,
2024-01-03,a3@example.com,A,2,1,3,
2024-01-04,a4@example.com,A,3,2,1,
2024-01-05,b1@example.com,B,1,2,3,
2024-01-06,b2@example.com,B,2,1,3,
2024-01-07,b3@example.com,B,3,1,2,
`)
	for _, seed := range []int64{1, 2, 3} {
		slots, report, err := Assign(s, rand.New(rand.NewSource(seed)), 3, 4)
		if err != nil {
			t.Fatalf("seed %d: Assign = %v", seed, err)
		}
		checkInvariants(t, s, slots, report, 3, 4)
	}
}

func TestBlankRanksAreLastResort(t *testing.T) {
	// Everyone leaves Green blank; Green must still be usable when
	// section headcount forces a third team... here two teams of 3
	// form on Red/Blue only. Blanks simply must not crash or veto.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,Green,person match
2024-01-01,a@example.com,1,1,2,
2024-01-02,b@example.com,1,1,2,
2024-01-03,c@example.com,1,2,1,
2024-01-04,d@example.com,1,2,1,
2024-01-05,e@example.com,1,1,2,
2024-01-06,f@example.com,1,2,1,
`)
	slots, report, err := Assign(s, rand.New(rand.NewSource(3)), 3, 4)
	if err != nil {
		t.Fatalf("Assign = %v", err)
	}
	checkInvariants(t, s, slots, report, 3, 4)
}

// seatedOnce checks the remap basics every completion must keep:
// everyone seated exactly once, teams section-pure.
func seatedOnce(t *testing.T, s *survey.Survey, slots []*Slot) {
	t.Helper()
	seen := map[string]bool{}
	for _, slot := range slots {
		for _, m := range slot.Members {
			if m.Section != slot.Section {
				t.Fatalf("%s seated out of section", m.Email)
			}
			if seen[m.Key] {
				t.Fatalf("%s seated twice", m.Email)
			}
			seen[m.Key] = true
		}
	}
	if len(seen) != len(s.People) {
		t.Fatalf("seated %d of %d", len(seen), len(s.People))
	}
}

func TestRemapVetoedRespondent(t *testing.T) {
	// a vetoed every project: no strict split exists, so the remap
	// seats them anyway and names the veto.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,person match
2024-01-01,a@example.com,1,-1,-1,
2024-01-02,b@example.com,1,1,2,
2024-01-03,c@example.com,1,1,2,
`)
	slots, report, err := Assign(s, rand.New(rand.NewSource(1)), 3, 4)
	if err != nil {
		t.Fatalf("Assign = %v", err)
	}
	if report.Clean() {
		t.Fatalf("all-veto respondent should remap, got clean report")
	}
	seatedOnce(t, s, slots)
	if len(report.Vetoes) != 1 || report.Vetoes[0].Email != "a@example.com" {
		t.Fatalf("want one veto note for a, got %+v", report.Vetoes)
	}
	if !strings.Contains(report.Vetoes[0].Alt, "vetoed all 2 projects") {
		t.Fatalf("want all-projects-vetoed why, got %q", report.Vetoes[0].Alt)
	}
	if got := report.Describe(); !strings.Contains(got, "veto: a@example.com") {
		t.Fatalf("Describe missing veto line:\n%s", got)
	}
}

func TestRemapForcedExclusion(t *testing.T) {
	// Three people, one team, a excludes b: someone must share with
	// an excluder. The remap does it and names the pair.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,person match
2024-01-01,a@example.com,1,1,2,b@example.com
2024-01-02,b@example.com,1,1,2,
2024-01-03,c@example.com,1,1,2,
`)
	slots, report, err := Assign(s, rand.New(rand.NewSource(1)), 3, 4)
	if err != nil {
		t.Fatalf("Assign = %v", err)
	}
	if report.Clean() {
		t.Fatalf("forced exclusion should remap, got clean report")
	}
	seatedOnce(t, s, slots)
	if len(report.Exclusions) != 1 {
		t.Fatalf("want one exclusion note, got %+v", report.Exclusions)
	}
	x := report.Exclusions[0]
	if x.By != "a@example.com" {
		t.Fatalf("exclusion stated by %q, want a@example.com", x.By)
	}
}

func TestRemapForcedSplit(t *testing.T) {
	// Two mutual partners with teams of 1: the pair must split, and
	// the remap records it.
	s := mustSurvey(t, `timestamp,email,section,Red,person match
2024-01-01,a@example.com,1,1,+b@example.com
2024-01-02,b@example.com,1,1,+a@example.com
`)
	slots, report, err := Assign(s, rand.New(rand.NewSource(1)), 1, 1)
	if err != nil {
		t.Fatalf("Assign = %v", err)
	}
	if report.Clean() {
		t.Fatalf("forced split should remap, got clean report")
	}
	seatedOnce(t, s, slots)
	if len(report.Splits) != 1 {
		t.Fatalf("want one split note, got %+v", report.Splits)
	}
}

func TestBoundsErrors(t *testing.T) {
	// Contradictory bounds are caller errors, not remaps.
	s := mustSurvey(t, sixPerson)
	if _, _, err := Assign(s, rand.New(rand.NewSource(1)), 4, 3); err == nil {
		t.Fatalf("min>max: expected error, got nil")
	}
	if _, _, err := Assign(s, rand.New(rand.NewSource(1)), 0, 4); err == nil {
		t.Fatalf("min<1: expected error, got nil")
	}
}

// totalCost sums stated ranks over the seating (the test fixtures
// here leave nothing blank).
func totalCost(t *testing.T, s *survey.Survey, slots []*Slot) int {
	t.Helper()
	total := 0
	for _, slot := range slots {
		for _, m := range slot.Members {
			total += *m.Ranks[slot.Project]
		}
	}
	return total
}

func TestOptimalCompletionBeatsGreedy(t *testing.T) {
	// Four Red-lovers and two Blue-lovers in two teams of 3. Booking
	// all four Red-lovers onto Red strands Blue short; the optimum
	// (total rank 7) lends one Red-lover to Blue.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,person match
2024-01-01,r1@example.com,1,1,2,
2024-01-02,r2@example.com,1,1,2,
2024-01-03,r3@example.com,1,1,2,
2024-01-04,r4@example.com,1,1,2,
2024-01-05,b1@example.com,1,2,1,
2024-01-06,b2@example.com,1,2,1,
`)
	for _, seed := range []int64{1, 2, 3, 4, 5} {
		slots, report, err := Assign(s, rand.New(rand.NewSource(seed)), 3, 4)
		if err != nil {
			t.Fatalf("seed %d: Assign = %v", seed, err)
		}
		checkInvariants(t, s, slots, report, 3, 4)
		if got := totalCost(t, s, slots); got != 7 {
			t.Fatalf("seed %d: total rank = %d, want optimum 7", seed, got)
		}
	}
}

func TestEveryProjectLoads(t *testing.T) {
	// Nine people, three slots, three projects — but nobody ranks
	// Green first. Slots allow, so Green must still load exactly
	// once, seeded by its closest-ranked respondent, with no
	// relaxation needed.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,Green,person match
2024-01-01,a@example.com,1,1,2,3,
2024-01-02,b@example.com,1,1,2,3,
2024-01-03,c@example.com,1,1,2,3,
2024-01-04,d@example.com,1,1,2,3,
2024-01-05,e@example.com,1,1,2,3,
2024-01-06,f@example.com,1,2,1,2,
2024-01-07,g@example.com,1,2,1,2,
2024-01-08,h@example.com,1,2,1,2,
2024-01-09,i@example.com,1,3,2,2,
`)
	for _, seed := range []int64{1, 2, 3} {
		slots, report, err := Assign(s, rand.New(rand.NewSource(seed)), 3, 4)
		if err != nil {
			t.Fatalf("seed %d: Assign = %v", seed, err)
		}
		checkInvariants(t, s, slots, report, 3, 4)
		seen := map[string]int{}
		for _, slot := range slots {
			seen[s.Teams[slot.Project]]++
		}
		if len(seen) != 3 || seen["Green"] != 1 {
			t.Fatalf("seed %d: projects loaded = %v, want each once", seed, seen)
		}
		if len(report.Unassigned) != 0 {
			t.Fatalf("seed %d: unassigned = %v, want none", seed, report.Unassigned)
		}
	}
}

func TestOverrideHonored(t *testing.T) {
	// a ranks Blue first but the teacher overrides them onto Red:
	// they land on Red with no relaxation needed.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,person match,override
2024-01-01,a@example.com,1,3,1,,Red
2024-01-02,b@example.com,1,1,2,,
2024-01-03,c@example.com,1,1,2,,
2024-01-04,d@example.com,1,2,1,,
`)
	for _, seed := range []int64{1, 2, 3} {
		slots, report, err := Assign(s, rand.New(rand.NewSource(seed)), 3, 4)
		if err != nil {
			t.Fatalf("seed %d: Assign = %v", seed, err)
		}
		checkInvariants(t, s, slots, report, 3, 4)
		got := teamOf(slots, "a@example.com")
		if got == nil || s.Teams[got.Project] != "Red" {
			t.Fatalf("seed %d: override ignored", seed)
		}
	}
}

func TestOverrideLoadsUnpopular(t *testing.T) {
	// Nobody ranks Green first, but a is overridden onto it: Green
	// still loads exactly once, seeded by its demander.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,Green,person match,override
2024-01-01,a@example.com,1,1,2,5,,Green
2024-01-02,b@example.com,1,1,2,3,,
2024-01-03,c@example.com,1,1,2,3,,
2024-01-04,d@example.com,1,1,2,3,,
2024-01-05,e@example.com,1,1,2,3,,
2024-01-06,f@example.com,1,2,1,2,,
2024-01-07,g@example.com,1,2,1,2,,
2024-01-08,h@example.com,1,2,1,2,,
2024-01-09,i@example.com,1,3,1,2,,
`)
	for _, seed := range []int64{1, 2, 3} {
		slots, report, err := Assign(s, rand.New(rand.NewSource(seed)), 3, 4)
		if err != nil {
			t.Fatalf("seed %d: Assign = %v", seed, err)
		}
		checkInvariants(t, s, slots, report, 3, 4)
		seen := map[string]int{}
		for _, slot := range slots {
			seen[s.Teams[slot.Project]]++
		}
		if len(seen) != 3 || seen["Green"] != 1 {
			t.Fatalf("seed %d: projects = %v, want each once", seed, seen)
		}
		if got := teamOf(slots, "a@example.com"); got == nil || s.Teams[got.Project] != "Green" {
			t.Fatalf("seed %d: overrider not on Green", seed)
		}
	}
}

func TestOverrideConflictFailsLoudly(t *testing.T) {
	// Four overriders onto one team of three: unsatisfiable, so a
	// loud error instead of silent disobedience.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,person match,override
2024-01-01,a@example.com,1,1,2,,Red
2024-01-02,b@example.com,1,1,2,,Red
2024-01-03,c@example.com,1,1,2,,Red
2024-01-04,d@example.com,1,1,2,,Red
2024-01-05,e@example.com,1,1,2,,
2024-01-06,f@example.com,1,2,1,,
`)
	_, _, err := Assign(s, rand.New(rand.NewSource(1)), 3, 4)
	if err == nil || !strings.Contains(err.Error(), "override") {
		t.Fatalf("Assign = %v, want override error", err)
	}
}

func TestOverrideVetoReported(t *testing.T) {
	// The teacher can force someone onto a vetoed team; the mandate
	// wins but the veto is still reported.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,person match,override
2024-01-01,a@example.com,1,-1,2,,Red
2024-01-02,b@example.com,1,1,2,,
2024-01-03,c@example.com,1,1,2,,
2024-01-04,d@example.com,1,1,2,,
`)
	slots, report, err := Assign(s, rand.New(rand.NewSource(1)), 3, 4)
	if err != nil {
		t.Fatalf("Assign = %v", err)
	}
	seatedOnce(t, s, slots)
	if got := teamOf(slots, "a@example.com"); got == nil || s.Teams[got.Project] != "Red" {
		t.Fatalf("override ignored")
	}
	if report.Clean() || len(report.Vetoes) != 1 || report.Vetoes[0].Email != "a@example.com" {
		t.Fatalf("want one veto note for a, got %+v", report)
	}
}

func TestUnassignedProjectsNoted(t *testing.T) {
	// Three people, one slot, two projects: only the most popular
	// loads. The other is listed as unassigned — information, not a
	// relaxation, so the report stays clean.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,person match
2024-01-01,a@example.com,1,1,2,
2024-01-02,b@example.com,1,1,2,
2024-01-03,c@example.com,1,2,1,
`)
	slots, report, err := Assign(s, rand.New(rand.NewSource(1)), 3, 4)
	if err != nil {
		t.Fatalf("Assign = %v", err)
	}
	checkInvariants(t, s, slots, report, 3, 4)
	if len(slots) != 1 || s.Teams[slots[0].Project] != "Red" {
		t.Fatalf("want the one team on Red")
	}
	if len(report.Unassigned) != 1 || report.Unassigned[0] != "Blue" {
		t.Fatalf("unassigned = %v, want [Blue]", report.Unassigned)
	}
}

func TestNonGreedyCompletionFound(t *testing.T) {
	// Greedy best-rank-first growth deterministically strands one of
	// a/b here (Green eats c at rank 2 and can never seat a/b at
	// rank 3); only a globally closest completion succeeds.
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,Green,person match
2024-01-01,a@example.com,1,1,2,3,b@example.com
2024-01-02,b@example.com,1,1,2,3,
2024-01-03,c@example.com,1,-1,1,2,
2024-01-04,d@example.com,1,2,-1,1,
2024-01-05,e@example.com,1,3,1,-1,
2024-01-06,f@example.com,1,2,3,1,
2024-01-07,g@example.com,1,3,2,1,
`)
	for seed := int64(1); seed <= 10; seed++ {
		slots, report, err := Assign(s, rand.New(rand.NewSource(seed)), 3, 4)
		if err != nil {
			t.Fatalf("seed %d: Assign = %v", seed, err)
		}
		checkInvariants(t, s, slots, report, 3, 4)
	}
}

func TestAffinityAndScoreFormula(t *testing.T) {
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,person match
2024-01-01,x@example.com,1,1,2,
2024-01-02,y@example.com,1,1,2,+x@example.com
2024-01-03,z@example.com,1,1,2,+y@example.com
`)
	x := s.ByKey["x@example.com"]
	y := s.ByKey["y@example.com"]
	z := s.ByKey["z@example.com"]
	if got := affinity(y, []*survey.Person{x}); got != 1 {
		t.Fatalf("one-sided affinity = %d, want 1", got)
	}
	if got := affinity(z, []*survey.Person{y}); got != 1 {
		t.Fatalf("one-sided affinity = %d, want 1", got)
	}
	if got := affinity(y, []*survey.Person{z}); got != 0 {
		t.Fatalf("stranger affinity = %d, want 0", got)
	}
	if got := affinity(x, []*survey.Person{y}); got != 0 {
		t.Fatalf("unrequited affinity = %d, want 0", got)
	}
	// Mutual wants score 2.
	x.Prefer = "y@example.com"
	if got := affinity(y, []*survey.Person{x}); got != 2 {
		t.Fatalf("mutual affinity = %d, want 2", got)
	}
	x.Prefer = ""

	eng := newEngine(s, rand.New(rand.NewSource(1)), 3, 4)
	// Equal ranks: affinity decides (4*1-1=3 beats 4*1-0=4).
	sy := eng.placeCost(y, 0, 0, []*survey.Person{x}, map[string]int{})
	sz := eng.placeCost(z, 0, 0, []*survey.Person{x}, map[string]int{})
	if sy.rank != 3 || sz.rank != 4 {
		t.Fatalf("ranks = %d, %d; want 3, 4", sy.rank, sz.rank)
	}
	// Better rank beats affinity: rank 1 unaffined (4) beats rank 2
	// affined (4*2-1=7).
	y.Ranks[0] = intPtr(2)
	if got := eng.placeCost(y, 0, 0, []*survey.Person{x}, map[string]int{}); got.rank != 7 {
		t.Fatalf("rank-2 affined bill = %d, want 7", got.rank)
	}
	if got := eng.placeCost(x, 0, 0, nil, map[string]int{}); got.rank != 4 {
		t.Fatalf("rank-1 bill = %d, want 4", got.rank)
	}
	// Penalties minimize in veto, exclusion, split order: zero
	// vetoes with any number of exclusions still beats one veto.
	c := s.ByKey["x@example.com"]
	c.Ranks[0] = intPtr(-1)
	if got := eng.placeCost(c, 0, 0, nil, map[string]int{}); got.vetoes != 1 {
		t.Fatalf("vetoed placement vetoes = %d, want 1", got.vetoes)
	}
	if !(penalty{0, 100, 100, 1 << 60}.less(penalty{1, 0, 0, 0})) {
		t.Fatalf("zero vetoes should beat one veto whatever else gives")
	}
	if !(penalty{0, 0, 100, 1 << 60}.less(penalty{0, 1, 0, 0})) {
		t.Fatalf("zero exclusions should beat one exclusion whatever else gives")
	}
	if !(penalty{0, 0, 0, 1 << 60}.less(penalty{0, 0, 1, 0})) {
		t.Fatalf("zero splits should beat one split whatever else gives")
	}
}

func intPtr(v int) *int { return &v }

func TestPlaceCostViolations(t *testing.T) {
	s := mustSurvey(t, `timestamp,email,section,Red,Blue,person match
2024-01-01,a@example.com,1,1,2,+b@example.com
2024-01-02,b@example.com,1,1,2,+a@example.com
2024-01-03,c@example.com,1,-1,1,a@example.com
`)
	eng := newEngine(s, rand.New(rand.NewSource(1)), 3, 4)
	a := s.ByKey["a@example.com"]
	b := s.ByKey["b@example.com"]
	c := s.ByKey["c@example.com"]
	nowhere := map[string]int{}
	// Exclusion with a seated member prices one exclusion.
	if got := eng.placeCost(a, 0, 0, []*survey.Person{c}, nowhere); got.exclusions != 1 {
		t.Fatalf("exclusions = %d, want 1", got.exclusions)
	}
	// Veto prices one veto even on an empty team.
	if got := eng.placeCost(c, 0, 0, nil, nowhere); got.vetoes != 1 {
		t.Fatalf("vetoes = %d, want 1", got.vetoes)
	}
	// Seating b apart from its mutual partner a prices one split;
	// seating them together prices none: rank 2 for Blue (8) minus
	// mutual affinity toward a (2).
	separate := map[string]int{"a@example.com": 0}
	if got := eng.placeCost(b, 1, 1, nil, separate); got.splits != 1 {
		t.Fatalf("splits = %d, want 1", got.splits)
	}
	together := map[string]int{"a@example.com": 0}
	if got := eng.placeCost(b, 1, 0, []*survey.Person{a}, together); got != (penalty{rank: 6}) {
		t.Fatalf("together cost = %+v, want rank 6 only", got)
	}
}

func TestWriteCSVShape(t *testing.T) {
	s := mustSurvey(t, sixPerson)
	slots, report, err := Assign(s, rand.New(rand.NewSource(7)), 3, 4)
	if err != nil {
		t.Fatalf("Assign = %v", err)
	}
	if !report.Clean() {
		t.Fatalf("six-person split should be strict: %q", report.Describe())
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, s, slots); err != nil {
		t.Fatalf("WriteCSV = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("rows = %d, want header + 2 teams:\n%s", len(lines), buf.String())
	}
	if lines[0] != "Team,Person 1,Person 2,Person 3" {
		t.Fatalf("header = %q", lines[0])
	}
	for _, line := range lines[1:] {
		cells := strings.Split(line, ",")
		if len(cells) != 4 { // team name + 3 members
			t.Fatalf("row %q has %d cells, want 4", line, len(cells))
		}
	}
}
