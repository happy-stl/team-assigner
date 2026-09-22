// Package assign places survey respondents onto teams.
//
// The rules, in brief: people join only with others in their own
// section; teams hold minSize to maxSize people; a -1 rank is a veto
// against that team; a person-match email is an exclusion; a '+'
// email is a pairing want, binding only when both people named each
// other.
//
// The procedure:
//
//  1. Split each section into slots covering it exactly, minimizing
//     sizing deviation (heads outside [minSize, maxSize]), then
//     maximizing in-bounds teams, then fewest teams (see
//     bestEffortPartition). A deviation-free split is the strict
//     case; anything else is reported as a sizing relaxation.
//
//  2. Load projects: teacher overrides first (most demanded unloaded
//     project, seeded by a demander), then repeatedly the most popular
//     not-yet-loaded project — popularity(p) is the number of
//     still-unassigned respondents who ranked p as 1, ties going to
//     the leftmost CSV column — seeded with one uniformly random
//     respondent who ranked it 1, opening a team in that respondent's
//     section. Slots left over once every loadable project is taken
//     reuse projects the same way, seeded by each section's
//     closest-ranked respondent (stated ranks, then blanks, then
//     vetoes as a last resort). An overridden respondent seeds solely
//     the mandated project; the fill seats them solely there, and an
//     unsatisfiable mandate fails loudly instead of seating them
//     elsewhere.
//
//  3. Fill every remaining seat with the closest feasible completion:
//     the assignment of the unassigned to their sections' teams that
//     minimizes the penalty tuple
//
//     (vetoes, exclusions, splits, rank)
//
//     compared lexicographically, where vetoes counts placements on
//     -1 projects, exclusions counts co-seated excluded pairs, splits
//     counts separated mutual '+' pairs, and rank is total 4*rank
//     minus '+' affinity toward teammates (blanks worst). Rank always
//     outranks affinity, affinity outranks chance: remaining ties are
//     ordered uniformly at random.
//
// A strict split — no relaxed constraint — is exactly a completion
// with penalty (0, 0, 0, r). When the CSV admits one, the search
// returns it: nothing outranks zero violations. When it does not, the
// same search returns the least-violation remap instead — second-best
// choices where the best are impossible, best choices everywhere
// else — and the Report names each relaxed constraint, which is the
// explanation of why the strict split failed. Greedy filling cannot
// be trusted for either case: seating each team's closest match first
// can strand a respondent even when a valid split exists, and no
// reshuffling of ties escapes such dead ends. Failed layouts (a seed
// can corner the remainder) are retried with fresh random choices,
// keeping the least penalty; retries derive deterministically from
// the caller's RNG, so a fixed seed still reproduces the same output.
package assign

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"github.com/grimwm/team-assigner/internal/survey"
)

// Slot is one team being built: a project in a section with members.
type Slot struct {
	Section string
	Cap     int
	Project int // index into Survey.Teams; -1 until a project is loaded
	Members []*survey.Person
	// LoadOrder records the sequence in which slots received their
	// project; it fixes the order teams are reported in.
	LoadOrder int
}

// Partition splits n section members into teams of minSize to
// maxSize people, or rejects headcounts that admit no such split
// (fewer than a minimum team, or gaps like 5 for sizes 3-4).
func Partition(n, minSize, maxSize int) ([]int, error) {
	sizes, dev, err := bestEffortPartition(n, minSize, maxSize)
	if err != nil {
		return nil, err
	}
	if dev > 0 {
		return nil, fmt.Errorf("section has %d people, which cannot be split into teams of %d-%d",
			n, minSize, maxSize)
	}
	return sizes, nil
}

// partKey orders partitions: least sizing deviation first, then
// fewest deviant teams, then fewest teams. Deviation counts heads
// outside [minSize, maxSize]. Undersized teams lose to oversized
// ones on the way: a lone 5 beats 3+2, keeping the section together
// on one team rather than stranding a pair.
type partKey struct {
	dev   int
	outCt int // out-of-bounds team count
	teams int
}

func (a partKey) less(b partKey) bool {
	if a.dev != b.dev {
		return a.dev < b.dev
	}
	if a.outCt != b.outCt {
		return a.outCt < b.outCt
	}
	return a.teams < b.teams
}

// bestEffortPartition splits n members into teams covering them
// exactly, minimizing the partKey above. Sizes may fall outside
// [minSize, maxSize]; dev reports the total heads outside. It only
// fails on contradictory bounds: every headcount admits some split
// into teams of at least 1.
func bestEffortPartition(n, minSize, maxSize int) (sizes []int, dev int, err error) {
	if minSize < 1 {
		return nil, 0, fmt.Errorf("minimum team size %d must be at least 1", minSize)
	}
	if maxSize < minSize {
		return nil, 0, fmt.Errorf("max team size %d is below minimum team size %d",
			maxSize, minSize)
	}
	if n < 1 {
		return nil, 0, fmt.Errorf("section has %d people, need at least 1", n)
	}
	devOf := func(s int) (int, bool) {
		if s < minSize {
			return minSize - s, false
		}
		if s > maxSize {
			return s - maxSize, false
		}
		return 0, true
	}
	const inf = math.MaxInt32
	best := make([]partKey, n+1)
	parent := make([]int, n+1)
	for i := range best {
		best[i] = partKey{inf, 0, inf}
	}
	best[0] = partKey{}
	for i := 0; i < n; i++ {
		if best[i].dev == inf {
			continue
		}
		// Descending sizes: strict improvement keeps the larger
		// team on fully tied keys.
		for s := n - i; s >= 1; s-- {
			d, ok := devOf(s)
			out := 0
			if !ok {
				out = 1
			}
			cand := partKey{best[i].dev + d, best[i].outCt + out, best[i].teams + 1}
			if cand.less(best[i+s]) {
				best[i+s] = cand
				parent[i+s] = s
			}
		}
	}
	var out []int
	for i := n; i > 0; i -= parent[i] {
		out = append(out, parent[i])
	}
	return out, best[n].dev, nil
}

// maxAttempts bounds the placement retries described above.
const maxAttempts = 32

// maxStaleAttempts stops the retries once remap quality stops
// improving; the best layout found is returned.
const maxStaleAttempts = 8

// nodeBudget caps the branch-and-bound search per section; beyond
// it the attempt is abandoned and the ritual retries.
const nodeBudget = 2000000

// blankCost is the cost of seating someone on a project they left
// blank: worse than any stated rank, but still placeable.
const blankCost = 1 << 20

// engine holds the mutable assignment state.
type engine struct {
	s       *survey.Survey
	rng     *rand.Rand
	minSize int
	maxSize int
	sizing  []span
	slots   []*Slot
	left    map[string]*survey.Person // unassigned, by key
	loaded  map[int]bool
	order   int
	nodes   int
	// exhausted records node-budget exhaustion in the latest
	// fillSection, qualifying the Report's optimality claim.
	exhausted bool
}

// Assign places every respondent with teams of minSize to maxSize
// people, relaxing constraints to a least-violation remap when no
// strict split exists. It returns the slots in fill order plus a
// Report naming each relaxed constraint (empty when strict). The
// error is for contradictory size bounds or a teacher override no
// layout can satisfy.
func Assign(s *survey.Survey, rng *rand.Rand, minSize, maxSize int) ([]*Slot, *Report, error) {
	// Sizing is deterministic and order-independent: settle it once
	// instead of retrying something retries cannot fix.
	probe := newEngine(s, rng, minSize, maxSize)
	sizing, err := probe.openSlots()
	if err != nil {
		return nil, nil, err
	}
	var bestSlots []*Slot
	var best *Report
	var lastErr error
	stale := 0
	for attempt := 0; attempt < maxAttempts && stale <= maxStaleAttempts; attempt++ {
		e := newEngine(s, rand.New(rand.NewSource(rng.Int63())), minSize, maxSize)
		e.sizing = sizing
		e.openSlotsFrom(sizing)
		e.loadNewProjects()
		if err := e.openRemainingSlots(); err != nil {
			lastErr = err
			continue
		}
		if err := e.fillOptimized(); err != nil {
			lastErr = err
			continue
		}
		rep := e.buildReport()
		if rep.Clean() {
			return e.orderedSlots(), rep, nil
		}
		if best == nil || rep.penalty.less(best.penalty) {
			best, bestSlots = rep, e.orderedSlots()
			stale = 0
		} else {
			stale++
		}
	}
	if best == nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("no satisfiable layout found")
		}
		return nil, nil, lastErr
	}
	return bestSlots, best, nil
}

// orderedSlots returns the slots in fill order.
func (e *engine) orderedSlots() []*Slot {
	out := append([]*Slot(nil), e.slots...)
	sort.Slice(out, func(i, j int) bool { return out[i].LoadOrder < out[j].LoadOrder })
	return out
}

func newEngine(s *survey.Survey, rng *rand.Rand, minSize, maxSize int) *engine {
	e := &engine{s: s, rng: rng, minSize: minSize, maxSize: maxSize, left: map[string]*survey.Person{}, loaded: map[int]bool{}}
	for _, p := range s.People {
		e.left[p.Key] = p
	}
	return e
}

// span is one section's slot sizes plus its sizing deviation from
// [minSize, maxSize] (zero in the strict case).
type span struct {
	section string
	sizes   []int
	dev     int
}

// openSlots plans one empty slot per team each section needs,
// allowing sizing deviation for the remap. Sections are processed in
// sorted order for determinism.
func (e *engine) openSlots() ([]span, error) {
	bySection := map[string][]*survey.Person{}
	for _, p := range e.s.People {
		bySection[p.Section] = append(bySection[p.Section], p)
	}
	sections := make([]string, 0, len(bySection))
	for sec := range bySection {
		sections = append(sections, sec)
	}
	sort.Strings(sections)
	var spans []span
	for _, sec := range sections {
		sizes, dev, err := bestEffortPartition(len(bySection[sec]), e.minSize, e.maxSize)
		if err != nil {
			return nil, fmt.Errorf("section %q: %w", sec, err)
		}
		spans = append(spans, span{sec, sizes, dev})
	}
	return spans, nil
}

// openSlotsFrom rebuilds empty slots from a sizing plan.
func (e *engine) openSlotsFrom(spans []span) {
	e.sizing = spans
	for _, sp := range spans {
		for _, size := range sp.sizes {
			e.slots = append(e.slots, &Slot{Section: sp.section, Cap: size, Project: -1})
		}
	}
}

// rankCost is the placement cost of seating p on proj: the stated
// rank, blankCost for no opinion. Vetoes are not costs but bans;
// callers must exclude them first.
func rankCost(p *survey.Person, proj int) int {
	if r := p.Ranks[proj]; r != nil {
		return *r
	}
	return blankCost
}

func vetoed(p *survey.Person, proj int) bool {
	return p.Ranks[proj] != nil && *p.Ranks[proj] == -1
}

func excluded(a, b *survey.Person) bool {
	return a.Exclude == b.Key || b.Exclude == a.Key
}

// affinity scores p against seated members by '+' wants: 2 for a
// mutual want seated with them, 1 for a one-sided want, 0 otherwise.
func affinity(p *survey.Person, members []*survey.Person) int {
	for _, m := range members {
		if m.Key == p.Prefer {
			if m.Prefer == p.Key {
				return 2
			}
			return 1
		}
	}
	return 0
}

// canJoin reports whether the unassigned respondent p may join a
// team for proj holding members: veto-free, exclusion-free, with a
// free seat. Mutual '+' partners are not seated atomically here;
// keeping them together is the fill optimizer's job (a split counts
// as one violation there).
func (e *engine) canJoin(members []*survey.Person, p *survey.Person, proj, cap int) bool {
	if _, ok := e.left[p.Key]; !ok {
		return false
	}
	if len(members)+1 > cap {
		return false
	}
	if vetoed(p, proj) {
		return false
	}
	for _, seated := range members {
		if excluded(p, seated) {
			return false
		}
	}
	return true
}

// place seats p on the slot and removes them from the unassigned pool.
func (e *engine) place(slot *Slot, p *survey.Person) {
	slot.Members = append(slot.Members, p)
	delete(e.left, p.Key)
}

// popularity counts unassigned respondents who ranked proj as 1.
func (e *engine) popularity(proj int) int {
	n := 0
	for _, p := range e.left {
		if r := p.Ranks[proj]; r != nil && *r == 1 {
			n++
		}
	}
	return n
}

// firstOpen returns the first slot in a section that has no project
// yet, or nil.
func (e *engine) firstOpen(section string) *Slot {
	for _, slot := range e.slots {
		if slot.Section == section && slot.Project == -1 {
			return slot
		}
	}
	return nil
}

// open loads proj onto an open slot in section.
func (e *engine) open(section string, proj int) *Slot {
	slot := e.firstOpen(section)
	slot.Project = proj
	slot.LoadOrder = e.order
	e.order++
	e.loaded[proj] = true
	return slot
}

// supporters lists unassigned respondents who ranked proj as 1 and
// whose section still has an open slot they could join. Respondents
// overridden onto another project never seed this one. Order is by
// email for determinism; callers pick uniformly at random.
func (e *engine) supporters(proj int) []*survey.Person {
	var out []*survey.Person
	for _, p := range e.s.People {
		if _, ok := e.left[p.Key]; !ok {
			continue
		}
		if p.OverrideIdx >= 0 && p.OverrideIdx != proj {
			continue
		}
		if r := p.Ranks[proj]; r == nil || *r != 1 {
			continue
		}
		slot := e.firstOpen(p.Section)
		if slot == nil || !e.canJoin(nil, p, proj, slot.Cap) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// demand lists unassigned respondents overridden onto proj whose
// section still has an open slot, in CSV order; callers pick
// uniformly at random.
func (e *engine) demand(proj int) []*survey.Person {
	var out []*survey.Person
	for _, p := range e.s.People {
		if _, ok := e.left[p.Key]; !ok {
			continue
		}
		if p.OverrideIdx != proj {
			continue
		}
		if e.firstOpen(p.Section) == nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// loadNewProjects seeds one team per project while slots last: teacher
// overrides first (most demanded unloaded project, then popularity,
// then leftmost), then the most popular unloaded project, then back to
// selection. Every project loads — even one nobody ranked first,
// seeded by its closest-ranked respondent — so a project goes without
// a team only when slots run out first (recorded in the Report).
func (e *engine) loadNewProjects() {
	for e.anyOpen() {
		target, seed := -1, (*survey.Person)(nil)
		// Teacher overrides: most demanded unloaded project first.
		bd, bp := -1, -1
		for proj := range e.s.Teams {
			if e.loaded[proj] {
				continue
			}
			d := e.demand(proj)
			if len(d) == 0 {
				continue
			}
			if pop := e.popularity(proj); len(d) > bd || (len(d) == bd && pop > bp) {
				target, seed, bd, bp = proj, d[e.rng.Intn(len(d))], len(d), pop
			}
		}
		if target == -1 {
			target = e.nextUnloadTarget()
			if target == -1 {
				return // every project loaded; reuse covers the rest
			}
			if sup := e.supporters(target); len(sup) > 0 {
				seed = sup[e.rng.Intn(len(sup))]
			} else {
				seed = e.seedAnywhere(target)
				if seed == nil {
					return // defensive; open slots imply candidates
				}
			}
		}
		slot := e.open(seed.Section, target)
		e.place(slot, seed)
	}
}

// anyOpen reports whether any slot still needs a project.
func (e *engine) anyOpen() bool {
	for _, slot := range e.slots {
		if slot.Project == -1 {
			return true
		}
	}
	return false
}

// nextUnloadTarget picks the most popular unloaded project by rank-1
// votes, ties to the leftmost column. With no rank-1 votes left it
// falls back to fewest vetoes, then lowest rank sum, then leftmost.
// It returns -1 once every project is loaded.
func (e *engine) nextUnloadTarget() int {
	best, bestFirsts := -1, 0
	for proj := range e.s.Teams {
		if e.loaded[proj] {
			continue
		}
		if n := e.popularity(proj); n > bestFirsts {
			best, bestFirsts = proj, n
		}
	}
	if best != -1 {
		return best
	}
	best, bestKey := -1, [2]int{math.MaxInt, math.MaxInt}
	for proj := range e.s.Teams {
		if e.loaded[proj] {
			continue
		}
		vetoes, sum := 0, 0
		for _, p := range e.left {
			if vetoed(p, proj) {
				vetoes++
			} else {
				sum += rankCost(p, proj)
			}
		}
		if k := [2]int{vetoes, sum}; k[0] < bestKey[0] ||
			(k[0] == bestKey[0] && k[1] < bestKey[1]) {
			best, bestKey = proj, k
		}
	}
	return best
}

// seedAnywhere picks the unassigned respondent with the smallest rank
// cost for proj across sections that still have an open slot: stated
// ranks first, then blanks, then vetoes as a last resort (recorded as
// a violation in the Report). Ties break uniformly at random.
func (e *engine) seedAnywhere(proj int) *survey.Person {
	var stated []*survey.Person
	bestRank := math.MaxInt
	var blanks, vetoes []*survey.Person
	for _, p := range e.s.People {
		if _, ok := e.left[p.Key]; !ok {
			continue
		}
		if e.firstOpen(p.Section) == nil {
			continue
		}
		if p.OverrideIdx >= 0 && p.OverrideIdx != proj {
			continue // overridden elsewhere; the fill seats them there
		}
		switch r := p.Ranks[proj]; {
		case r == nil:
			blanks = append(blanks, p)
		case *r == -1:
			vetoes = append(vetoes, p)
		case *r < bestRank:
			bestRank, stated = *r, []*survey.Person{p}
		case *r == bestRank:
			stated = append(stated, p)
		}
	}
	switch {
	case len(stated) > 0:
		return stated[e.rng.Intn(len(stated))]
	case len(blanks) > 0:
		return blanks[e.rng.Intn(len(blanks))]
	case len(vetoes) > 0:
		return vetoes[e.rng.Intn(len(vetoes))]
	default:
		return nil
	}
}

// openRemainingSlots loads projects onto slots left over after every
// project has a team, reusing projects. A loaded project with more
// unassigned override demand in a section than free seats on its
// existing teams there reopens first, seeded by a demander;
// otherwise the most popular project reopens, seeded by the section's
// closest-ranked respondent.
func (e *engine) openRemainingSlots() error {
	for {
		if section, target, ok := e.reuseTarget(); ok {
			d := e.demandIn(section, target)
			seed := d[e.rng.Intn(len(d))]
			slot := e.open(section, target)
			e.place(slot, seed)
			continue
		}
		var section string
		found := false
		for _, slot := range e.slots {
			if slot.Project == -1 {
				section, found = slot.Section, true
				break
			}
		}
		if !found {
			return nil
		}
		target := e.mostPopular()
		seed := e.seedFor(section, target)
		if seed == nil {
			// Everyone left in this section vetoed the favorite;
			// try the next project before giving up.
			next := -1
			for proj := range e.s.Teams {
				if proj == target {
					continue
				}
				if e.seedFor(section, proj) != nil {
					next = proj
					break
				}
			}
			if next == -1 {
				return fmt.Errorf(
					"section %q: remaining respondents veto every project", section)
			}
			target = next
			seed = e.seedFor(section, target)
		}
		slot := e.open(section, target)
		e.place(slot, seed)
	}
}

// demandIn lists unassigned respondents in a section overridden onto
// proj, in CSV order.
func (e *engine) demandIn(section string, proj int) []*survey.Person {
	var out []*survey.Person
	for _, p := range e.s.People {
		if _, ok := e.left[p.Key]; !ok || p.Section != section {
			continue
		}
		if p.OverrideIdx == proj {
			out = append(out, p)
		}
	}
	return out
}

// reuseTarget finds a loaded project that still needs a fresh team: a
// section with an open slot whose unassigned demanders outnumber the
// free seats on its existing teams for that project. Sections are
// scanned in slot order and projects in column order, so the first
// hit wins deterministically.
func (e *engine) reuseTarget() (string, int, bool) {
	seen := map[string]bool{}
	for _, slot := range e.slots {
		sec := slot.Section
		if seen[sec] || e.firstOpen(sec) == nil {
			seen[sec] = true
			continue
		}
		seen[sec] = true
		free := map[int]int{}
		for _, s2 := range e.slots {
			if s2.Section == sec && s2.Project != -1 && e.loaded[s2.Project] {
				free[s2.Project] += s2.Cap - len(s2.Members)
			}
		}
		need := map[int][]*survey.Person{}
		for _, p := range e.s.People {
			if _, ok := e.left[p.Key]; !ok || p.Section != sec {
				continue
			}
			if p.OverrideIdx >= 0 && e.loaded[p.OverrideIdx] {
				need[p.OverrideIdx] = append(need[p.OverrideIdx], p)
			}
		}
		for proj := range e.s.Teams {
			if len(need[proj]) > free[proj] {
				return sec, proj, true
			}
		}
	}
	return "", -1, false
}

// mostPopular returns the project with the most rank-1 votes among
// the unassigned, ties to the leftmost column. With no rank-1 votes
// left it falls back to fewest vetoes, then lowest rank sum, then
// the leftmost column.
func (e *engine) mostPopular() int {
	best, bestFirsts := 0, -1
	for proj := range e.s.Teams {
		if n := e.popularity(proj); n > bestFirsts {
			best, bestFirsts = proj, n
		}
	}
	if bestFirsts > 0 {
		return best
	}
	best, bestKey := 0, [2]int{math.MaxInt, math.MaxInt}
	for proj := range e.s.Teams {
		vetoes, sum := 0, 0
		for _, p := range e.left {
			if vetoed(p, proj) {
				vetoes++
			} else {
				sum += rankCost(p, proj)
			}
		}
		if k := [2]int{vetoes, sum}; k[0] < bestKey[0] ||
			(k[0] == bestKey[0] && k[1] < bestKey[1]) {
			best, bestKey = proj, k
		}
	}
	return best
}

// seedFor picks the unassigned respondent in a section with the
// smallest rank cost for proj: stated ranks first, then blanks, then
// vetoes as a last resort (a vetoed seed is recorded as a violation
// in the Report). Ties break uniformly at random. It returns nil
// only when the section has nobody left to seed with.
func (e *engine) seedFor(section string, proj int) *survey.Person {
	opener := e.firstOpen(section)
	if opener == nil {
		return nil
	}
	var stated, blanks, vetoes []*survey.Person
	best := math.MaxInt
	for _, p := range e.s.People {
		if _, ok := e.left[p.Key]; !ok || p.Section != section {
			continue
		}
		if p.OverrideIdx >= 0 && p.OverrideIdx != proj {
			continue // overridden elsewhere; the fill seats them there
		}
		switch r := p.Ranks[proj]; {
		case r == nil:
			blanks = append(blanks, p)
		case *r == -1:
			vetoes = append(vetoes, p)
		case *r < best:
			best, stated = *r, []*survey.Person{p}
		case *r == best:
			stated = append(stated, p)
		}
	}
	switch {
	case len(stated) > 0:
		return stated[e.rng.Intn(len(stated))]
	case len(blanks) > 0:
		return blanks[e.rng.Intn(len(blanks))]
	case len(vetoes) > 0:
		return vetoes[e.rng.Intn(len(vetoes))]
	default:
		return nil
	}
}

// penalty tallies a completion's relaxed constraints plus its rank
// bill, compared lexicographically: fewer veto placements first,
// then fewer co-seated exclusions, then fewer split mutual pairs,
// then the closest ranks. A strict split is exactly (0, 0, 0, r).
type penalty struct {
	vetoes     int
	exclusions int
	splits     int
	rank       int64
}

func (a penalty) less(b penalty) bool {
	if a.vetoes != b.vetoes {
		return a.vetoes < b.vetoes
	}
	if a.exclusions != b.exclusions {
		return a.exclusions < b.exclusions
	}
	if a.splits != b.splits {
		return a.splits < b.splits
	}
	return a.rank < b.rank
}

func (a penalty) add(b penalty) penalty {
	return penalty{
		a.vetoes + b.vetoes,
		a.exclusions + b.exclusions,
		a.splits + b.splits,
		a.rank + b.rank,
	}
}

// fillItem is one respondent awaiting a seat.
type fillItem struct {
	p *survey.Person
	// minRank bounds the search: cheapest rank over every slot,
	// vetoes included, hence never above the true bill.
	minRank int
	// allow lists joinable slot indices: every slot, or solely the
	// mandated project's teams for an overridden respondent.
	allow []int
}

// fillOptimized seats every remaining respondent via the closest
// feasible completion of each section, independently: constraints
// never cross sections since teams never span them. Vetoes,
// exclusions, and splits penalize rather than ban, so a completion
// always exists and the Report carries any relaxation — except an
// unsatisfiable teacher override, which fails loudly.
func (e *engine) fillOptimized() error {
	bySection := map[string][]*Slot{}
	for _, slot := range e.slots {
		bySection[slot.Section] = append(bySection[slot.Section], slot)
	}
	sections := make([]string, 0, len(bySection))
	for sec := range bySection {
		sections = append(sections, sec)
	}
	sort.Strings(sections)
	for _, sec := range sections {
		var pool []*survey.Person
		for _, p := range e.s.People {
			if _, ok := e.left[p.Key]; ok && p.Section == sec {
				pool = append(pool, p)
			}
		}
		if len(pool) == 0 {
			continue
		}
		if err := e.fillSection(bySection[sec], pool); err != nil {
			return err
		}
	}
	return nil
}

// fillSection assigns pool to slots minimizing the penalty with a
// branch-and-bound search. Search orderings are shuffled, so equally
// penalized completions win uniformly at random. On node-budget
// exhaustion the best completion found so far stands and the engine
// records it (see Report.complete).
func (e *engine) fillSection(slots []*Slot, pool []*survey.Person) error {
	items := make([]*fillItem, 0, len(pool))
	for _, p := range pool {
		item := &fillItem{p: p, minRank: math.MaxInt}
		for _, slot := range slots {
			if r := rankCost(p, slot.Project); r < item.minRank {
				item.minRank = r
			}
		}
		items = append(items, item)
	}

	// Allowed slots per item: an overridden respondent may join
	// solely teams playing the mandated project. Missing allowed
	// slots fail loudly below instead of silently seating them
	// elsewhere.
	for _, item := range items {
		for si, slot := range slots {
			if item.p.OverrideIdx >= 0 && slot.Project != item.p.OverrideIdx {
				continue
			}
			item.allow = append(item.allow, si)
		}
		if len(item.allow) == 0 {
			return fmt.Errorf("cannot satisfy override: %s must join %q but it has no team in section %q",
				item.p.Email, e.s.Teams[item.p.OverrideIdx], slots[0].Section)
		}
	}

	// Most constrained items first (fewest fitting slots); shuffle
	// ties so equivalent orders vary run to run.
	e.rng.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	sort.SliceStable(items, func(i, j int) bool {
		return countFits(items[i], slots) < countFits(items[j], slots)
	})

	// Per-item slot order: cheapest rank first, ties shuffled.
	orders := make([][]int, len(items))
	for i, item := range items {
		orders[i] = e.slotOrder(item, slots)
	}

	where := map[string]int{}
	seated := make([][]*survey.Person, len(slots))
	for i, slot := range slots {
		seated[i] = append([]*survey.Person(nil), slot.Members...)
		for _, m := range slot.Members {
			where[m.Key] = i
		}
	}
	st := &searchState{
		best:   penalty{math.MaxInt, math.MaxInt, math.MaxInt, math.MaxInt64},
		choice: make([]int, len(items)),
	}
	// Seed the bound with a greedy completion. Without overrides one
	// always exists (slots cover the pool exactly); with overrides the
	// mandated seats may not suffice, which fails loudly.
	gbest, gchoice, ok := e.greedyComplete(items, orders, slots, seated, where)
	if !ok {
		return overrideShortage(e.s, slots, pool)
	}
	st.best, st.choice = gbest, gchoice
	e.nodes = 0
	// Note: exhausted is sticky across sections within an attempt;
	// each attempt starts from a fresh engine.
	e.descend(items, orders, slots, 0, penalty{}, make([]int, len(items)), seated, where, st)
	for i, item := range items {
		si := st.choice[i]
		slots[si].Members = append(slots[si].Members, item.p)
		delete(e.left, item.p.Key)
	}
	return nil
}

// overrideShortage explains an unsatisfiable override: demanders
// outnumber free seats on the mandated project's teams, or the
// project has no team in the section at all.
func overrideShortage(s *survey.Survey, slots []*Slot, pool []*survey.Person) error {
	teams := map[int]bool{}
	free := map[int]int{}
	for _, slot := range slots {
		teams[slot.Project] = true
		free[slot.Project] += slot.Cap - len(slot.Members)
	}
	need := map[int]int{}
	for _, p := range pool {
		if p.OverrideIdx >= 0 {
			need[p.OverrideIdx]++
		}
	}
	for proj, n := range need {
		if !teams[proj] {
			return fmt.Errorf("cannot satisfy override: %d respondent(s) must join %q but it has no team in section %q",
				n, s.Teams[proj], slots[0].Section)
		}
		if n > free[proj] {
			return fmt.Errorf("cannot satisfy override: %d respondent(s) must join %q in section %q but it has %d free seat(s)",
				n, s.Teams[proj], slots[0].Section, free[proj])
		}
	}
	return fmt.Errorf("cannot satisfy overrides in section %q", slots[0].Section)
}

// countFits counts allowed slots the respondent ranked without a
// veto: an ordering heuristic only, since vetoes penalize rather
// than ban.
func countFits(item *fillItem, slots []*Slot) int {
	n := 0
	for _, si := range item.allow {
		if !vetoed(item.p, slots[si].Project) {
			n++
		}
	}
	return n
}

// placeCost prices seating p on slot si holding members: veto,
// exclusion, and split penalties plus 4*rank minus affinity toward
// the teammates seated so far (teammates seated later are missed,
// so affinity settles near-ties rather than exact ones).
func (e *engine) placeCost(p *survey.Person, proj, si int, members []*survey.Person, where map[string]int) penalty {
	pen := penalty{}
	if vetoed(p, proj) {
		pen.vetoes = 1
	}
	for _, m := range members {
		if excluded(p, m) {
			pen.exclusions++
		}
	}
	if q := e.s.MutualPartner(p); q != nil {
		if sj, ok := where[q.Key]; ok && sj != si {
			pen.splits = 1
		}
	}
	pen.rank = int64(4*rankCost(p, proj) - affinity(p, members))
	return pen
}

// slotOrder returns allowed slot indices cheapest-rank-first for the
// item, uniform-random within equal ranks.
func (e *engine) slotOrder(item *fillItem, slots []*Slot) []int {
	type scored struct {
		idx int
		val int
	}
	ss := make([]scored, 0, len(item.allow))
	for _, i := range item.allow {
		slot := slots[i]
		v := blankCost
		if !vetoed(item.p, slot.Project) {
			v = rankCost(item.p, slot.Project)
		}
		ss = append(ss, scored{i, v})
	}
	e.rng.Shuffle(len(ss), func(i, j int) { ss[i], ss[j] = ss[j], ss[i] })
	sort.SliceStable(ss, func(i, j int) bool { return ss[i].val < ss[j].val })
	order := make([]int, len(ss))
	for i, s := range ss {
		order[i] = s.idx
	}
	return order
}

type searchState struct {
	best   penalty
	choice []int
}

// greedyComplete seats each item on its first allowed slot with a
// free seat, accumulating the true penalty. Without overrides it
// always completes (slots cover the pool exactly); with overrides the
// mandated seats may not suffice, reported as false.
func (e *engine) greedyComplete(items []*fillItem, orders [][]int, slots []*Slot, seated [][]*survey.Person, where map[string]int) (penalty, []int, bool) {
	gseated := make([][]*survey.Person, len(seated))
	for i := range seated {
		gseated[i] = append([]*survey.Person(nil), seated[i]...)
	}
	gwhere := make(map[string]int, len(where))
	for k, v := range where {
		gwhere[k] = v
	}
	choice := make([]int, len(items))
	var total penalty
	for i, item := range items {
		placed := false
		for _, si := range orders[i] {
			if len(gseated[si]) >= slots[si].Cap {
				continue
			}
			total = total.add(e.placeCost(item.p, slots[si].Project, si, gseated[si], gwhere))
			gseated[si] = append(gseated[si], item.p)
			gwhere[item.p.Key] = si
			choice[i] = si
			placed = true
			break
		}
		if !placed {
			return penalty{}, nil, false
		}
	}
	return total, choice, true
}

// searchWithSeating explores completions depth-first along the
// seating carried down the path, keeping the least penalty found.
func (e *engine) searchWithSeating(items []*fillItem, orders [][]int, slots []*Slot, idx int, cur penalty, choice []int, seated [][]*survey.Person, where map[string]int, st *searchState) {
	for _, si := range orders[idx] {
		if len(seated[si]) >= slots[si].Cap {
			continue
		}
		next := cur.add(e.placeCost(items[idx].p, slots[si].Project, si, seated[si], where))
		if !next.less(st.best) {
			continue
		}
		choice[idx] = si
		seated[si] = append(seated[si], items[idx].p)
		where[items[idx].p.Key] = si
		e.descend(items, orders, slots, idx+1, next, choice, seated, where, st)
		seated[si] = seated[si][:len(seated[si])-1]
		delete(where, items[idx].p.Key)
		if e.nodes > nodeBudget {
			return
		}
	}
}

// descend checks the bound, records completions, and recurses. The
// bound assumes every remaining item takes its cheapest rank with
// full affinity and no penalties: optimistic, therefore safe.
func (e *engine) descend(items []*fillItem, orders [][]int, slots []*Slot, idx int, cur penalty, choice []int, seated [][]*survey.Person, where map[string]int, st *searchState) {
	e.nodes++
	if e.nodes > nodeBudget {
		e.exhausted = true
		return
	}
	if !cur.less(st.best) {
		return
	}
	if idx == len(items) {
		st.best, st.choice = cur, append([]int(nil), choice...)
		return
	}
	bound := cur
	for j := idx; j < len(items); j++ {
		bound.rank += int64(4*items[j].minRank - 2) // max affinity is 2
		if !bound.less(st.best) {
			return
		}
	}
	e.searchWithSeating(items, orders, slots, idx, cur, choice, seated, where, st)
}

// SizingNote records a section whose headcount cannot split into
// [minSize, maxSize] teams and the sizes used instead.
type SizingNote struct {
	Section  string
	Sizes    []int
	Min, Max int
}

// VetoNote records a respondent seated on a vetoed project and the
// closest non-vetoed alternative offered in their section, if any.
type VetoNote struct {
	Email, Team, Alt string
}

// ExclusionNote records two respondents sharing a team despite an
// exclusion, and who stated it.
type ExclusionNote struct {
	A, B, Team, By string
}

// SplitNote records mutual '+' partners seated on different teams.
type SplitNote struct {
	A, B, TeamA, TeamB string
}

// Report explains a remap: every relaxed constraint, plus the
// projects that got no team for lack of slots. Clean reports carry
// nothing because the strict split held.
type Report struct {
	penalty    penalty
	complete   bool // search proved the penalty optimal
	Sizing     []SizingNote
	Vetoes     []VetoNote
	Exclusions []ExclusionNote
	Splits     []SplitNote
	// Unassigned names projects that loaded onto no team, in CSV
	// column order. This only happens when team slots run out first;
	// it is information, not a relaxed constraint, and does not
	// affect Clean.
	Unassigned []string
}

// Clean reports whether every constraint held strictly.
func (r *Report) Clean() bool {
	return r != nil && len(r.Sizing) == 0 &&
		r.penalty.vetoes == 0 && r.penalty.exclusions == 0 && r.penalty.splits == 0
}

// Relaxed counts the relaxed constraints for one-line summaries.
func (r *Report) Relaxed() int {
	if r == nil {
		return 0
	}
	return len(r.Sizing) + r.penalty.vetoes + r.penalty.exclusions + r.penalty.splits
}

// Describe renders the why: each relaxed constraint on its own line,
// or "" when strict.
func (r *Report) Describe() string {
	if r == nil || r.Clean() {
		return ""
	}
	var b strings.Builder
	if r.complete {
		b.WriteString("no strict split exists; best-effort remap relaxes:\n")
	} else {
		b.WriteString("search budget exhausted (a strict split may still exist); best layout found relaxes:\n")
	}
	for _, s := range r.Sizing {
		fmt.Fprintf(&b, "  sizing: section %q has %d people, cannot split into teams of %d-%d → using %s\n",
			s.Section, sumInts(s.Sizes), s.Min, s.Max, joinInts(s.Sizes, "+"))
	}
	for _, v := range r.Vetoes {
		fmt.Fprintf(&b, "  veto: %s on %q (ranked -1); %s\n", v.Email, v.Team, v.Alt)
	}
	for _, x := range r.Exclusions {
		fmt.Fprintf(&b, "  exclusion: %s and %s share %q (excluded by %s)\n", x.A, x.B, x.Team, x.By)
	}
	for _, sp := range r.Splits {
		fmt.Fprintf(&b, "  split: %s and %s separated (%q vs %q)\n", sp.A, sp.B, sp.TeamA, sp.TeamB)
	}
	return b.String()
}

func sumInts(ns []int) int {
	total := 0
	for _, n := range ns {
		total += n
	}
	return total
}

func joinInts(ns []int, sep string) string {
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, sep)
}

// buildReport scans the finished seating into a Report: sizing
// deviations from the plan, veto placements with their closest
// in-section alternative, co-seated exclusions, and split pairs.
func (e *engine) buildReport() *Report {
	rep := &Report{complete: !e.exhausted}
	for _, sp := range e.sizing {
		if sp.dev > 0 {
			rep.Sizing = append(rep.Sizing, SizingNote{
				Section: sp.section,
				Sizes:   append([]int(nil), sp.sizes...),
				Min:     e.minSize, Max: e.maxSize,
			})
		}
	}
	loaded := map[int]bool{}
	for _, slot := range e.slots {
		loaded[slot.Project] = true
	}
	for i, name := range e.s.Teams {
		if !loaded[i] {
			rep.Unassigned = append(rep.Unassigned, name)
		}
	}
	where := map[string]*Slot{}
	for _, slot := range e.slots {
		for _, m := range slot.Members {
			where[m.Key] = slot
		}
	}
	for _, slot := range e.slots {
		team := e.s.Teams[slot.Project]
		for _, m := range slot.Members {
			if vetoed(m, slot.Project) {
				rep.Vetoes = append(rep.Vetoes, VetoNote{m.Email, team, e.altFor(m, slot)})
			}
		}
		for i := 0; i < len(slot.Members); i++ {
			for j := i + 1; j < len(slot.Members); j++ {
				a, b := slot.Members[i], slot.Members[j]
				if !excluded(a, b) {
					continue
				}
				by := a.Email
				if b.Exclude == a.Key {
					by = b.Email
					if a.Exclude == b.Key {
						by = a.Email + " and " + b.Email
					}
				}
				rep.Exclusions = append(rep.Exclusions, ExclusionNote{a.Email, b.Email, team, by})
			}
		}
	}
	for _, p := range e.s.People {
		q := e.s.MutualPartner(p)
		if q == nil || p.Key >= q.Key {
			continue // emit each pair once
		}
		if sp, sq := where[p.Key], where[q.Key]; sp != sq {
			rep.Splits = append(rep.Splits, SplitNote{
				p.Email, q.Email,
				e.s.Teams[sp.Project], e.s.Teams[sq.Project],
			})
		}
	}
	sort.Slice(rep.Vetoes, func(i, j int) bool { return rep.Vetoes[i].Email < rep.Vetoes[j].Email })
	sort.Slice(rep.Exclusions, func(i, j int) bool {
		if rep.Exclusions[i].A != rep.Exclusions[j].A {
			return rep.Exclusions[i].A < rep.Exclusions[j].A
		}
		return rep.Exclusions[i].B < rep.Exclusions[j].B
	})
	sort.Slice(rep.Splits, func(i, j int) bool { return rep.Splits[i].A < rep.Splits[j].A })
	var pen penalty
	for _, slot := range e.slots {
		for _, m := range slot.Members {
			if vetoed(m, slot.Project) {
				pen.vetoes++
			}
			pen.rank += int64(4*rankCost(m, slot.Project) - affinity(m, slot.Members))
		}
	}
	pen.exclusions = len(rep.Exclusions)
	pen.splits = len(rep.Splits)
	rep.penalty = pen
	return rep
}

// altFor names the closest non-vetoed alternative offered in the
// respondent's section: project, rank, or blank. When they vetoed
// everything offered — or every project in the file — it says so.
func (e *engine) altFor(p *survey.Person, seated *Slot) string {
	best := -1
	bestRank := math.MaxInt
	seen := map[int]bool{}
	for _, slot := range e.slots {
		if slot.Section != seated.Section || seen[slot.Project] {
			continue
		}
		seen[slot.Project] = true
		if vetoed(p, slot.Project) || slot.Project == seated.Project {
			continue
		}
		if r := rankCost(p, slot.Project); r < bestRank {
			best, bestRank = slot.Project, r
		}
	}
	if best != -1 {
		if bestRank == blankCost {
			return fmt.Sprintf("closest alternative %q (no opinion)", e.s.Teams[best])
		}
		return fmt.Sprintf("closest alternative %q (rank %d)", e.s.Teams[best], bestRank)
	}
	allVeto := true
	for i := range e.s.Teams {
		if !vetoed(p, i) {
			allVeto = false
			break
		}
	}
	if allVeto {
		return fmt.Sprintf("vetoed all %d projects", len(e.s.Teams))
	}
	return "vetoed every other project offered in this section"
}

// WriteCSV renders slots as a header row of "Team,Person 1, ..."
// followed by one row per team, rows sorted by team name then
// section, emails sorted within each row. The person columns span the
// largest team; shorter rows are padded with blanks so the CSV stays
// rectangular.
func WriteCSV(w io.Writer, s *survey.Survey, slots []*Slot) error {
	ordered := append([]*Slot(nil), slots...)
	sort.Slice(ordered, func(i, j int) bool {
		a, b := s.Teams[ordered[i].Project], s.Teams[ordered[j].Project]
		if a != b {
			return a < b
		}
		return ordered[i].Section < ordered[j].Section
	})
	width := 0
	for _, slot := range ordered {
		if len(slot.Members) > width {
			width = len(slot.Members)
		}
	}
	cw := csv.NewWriter(w)
	header := make([]string, 0, width+1)
	header = append(header, "Team")
	for i := 1; i <= width; i++ {
		header = append(header, fmt.Sprintf("Person %d", i))
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, slot := range ordered {
		emails := make([]string, 0, len(slot.Members))
		for _, m := range slot.Members {
			emails = append(emails, m.Email)
		}
		sort.Strings(emails)
		row := append([]string{s.Teams[slot.Project]}, emails...)
		for len(row) < width+1 {
			row = append(row, "")
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
