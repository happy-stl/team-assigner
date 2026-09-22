// Package survey parses the survey CSV that drives team assignment.
//
// Layout (columns in order):
//
//		timestamp, email, section, <team 1>, <team 2>, ..., <person match>
//
//	  - timestamp is ignored.
//	  - email identifies the respondent.
//	  - section groups respondents; only people in the same section
//	    may be placed on the same team.
//	  - each team column holds a rank: -1 means "do not assign me to
//	    this team", 1 is the respondent's favorite, and larger numbers
//	    are progressively less liked. A blank cell means no opinion.
//	  - person match holds either a plain email (this respondent must
//	    not be teamed with that person) or an email prefixed with '+'
//	    (this respondent wants to be teamed with that person; the want
//	    is binding only when both people name each other).
package survey

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Person is one survey respondent.
type Person struct {
	// Email is the respondent's address as written in the CSV.
	Email string
	// Key is the normalized identity used for matching
	// (trimmed, lowercased email).
	Key string
	// Section groups respondents; "" means "no section given",
	// which places everyone in a single shared section.
	Section string
	// Ranks parallels Survey.Teams. A nil entry is a blank cell
	// (no opinion). -1 is a veto; 1 is the favorite.
	Ranks []*int
	// Exclude is the normalized email of someone this person must
	// not be teamed with ("" when the person-match cell is blank
	// or a '+' want).
	Exclude string
	// Prefer is the normalized email (without the '+') of someone
	// this person wants to be teamed with ("" when absent).
	Prefer string
	// Override is the team name from the override column as written
	// ("" when blank or the column is absent): a teacher mandate to
	// join that team regardless of ranks.
	Override string
	// OverrideIdx is the mandated team index, -1 when none.
	// Resolved by Validate.
	OverrideIdx int
}

// Survey is a parsed survey: team names in CSV column order plus
// the respondents keyed by normalized email.
type Survey struct {
	Teams  []string
	People []*Person
	ByKey  map[string]*Person
}

// normalize trims surrounding space; keys additionally lowercase.
func normalize(s string) string { return strings.TrimSpace(s) }
func keyOf(s string) string     { return strings.ToLower(normalize(s)) }

// Parse reads a survey CSV. The first row is the header; its team
// columns (everything between section and person match) supply the
// team names in column order.
func Parse(r io.Reader) (*Survey, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	// Drop leading blank lines; the first non-blank row is the header.
	for len(rows) > 0 && isBlankRow(rows[0]) {
		rows = rows[1:]
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("csv is empty")
	}
	header := rows[0]
	// An optional override column comes last, headed "override"
	// (case-insensitive); person match is the last column otherwise.
	matchIdx := len(header) - 1
	overrideIdx := -1
	if keyOf(header[matchIdx]) == "override" {
		overrideIdx = matchIdx
		matchIdx--
	}
	minCols := 5
	want := "timestamp, email, section, one team, person match"
	if overrideIdx != -1 {
		minCols = 6
		want += ", override"
	}
	if len(header) < minCols {
		return nil, fmt.Errorf("header has %d columns, need at least %d (%s)",
			len(header), minCols, want)
	}
	teams := make([]string, 0, matchIdx-3)
	for i := 3; i < matchIdx; i++ {
		name := normalize(header[i])
		if name == "" {
			return nil, fmt.Errorf("team column %d has an empty name", i+1)
		}
		teams = append(teams, name)
	}

	s := &Survey{Teams: teams, ByKey: map[string]*Person{}}
	for ri, row := range rows[1:] {
		if isBlankRow(row) {
			continue
		}
		// Pad short rows (a missing trailing override cell is
		// common); reject long rows instead of silently dropping data.
		if len(row) < len(header) {
			padded := make([]string, len(header))
			copy(padded, row)
			row = padded
		}
		if len(row) > len(header) {
			return nil, fmt.Errorf("row %d has %d fields, header has %d",
				ri+2, len(row), len(header))
		}
		p, err := parsePerson(row, teams, matchIdx, overrideIdx, ri+2)
		if err != nil {
			return nil, err
		}
		if _, dup := s.ByKey[p.Key]; dup {
			return nil, fmt.Errorf("row %d: duplicate email %q", ri+2, p.Email)
		}
		s.People = append(s.People, p)
		s.ByKey[p.Key] = p
	}
	if len(s.People) == 0 {
		return nil, fmt.Errorf("csv has a header but no respondents")
	}
	return s, nil
}

func isBlankRow(row []string) bool {
	for _, c := range row {
		if normalize(c) != "" {
			return false
		}
	}
	return true
}

func parsePerson(row []string, teams []string, matchIdx, overrideIdx, rownum int) (*Person, error) {
	email := normalize(row[1])
	if email == "" {
		return nil, fmt.Errorf("row %d: missing email", rownum)
	}
	p := &Person{Email: email, Key: keyOf(email), Section: normalize(row[2]), OverrideIdx: -1}

	p.Ranks = make([]*int, len(teams))
	for i := 0; i < len(teams); i++ {
		cell := normalize(row[3+i])
		if cell == "" {
			continue // blank: no opinion
		}
		v, err := strconv.Atoi(cell)
		if err != nil || v == 0 || v < -1 {
			return nil, fmt.Errorf(
				"row %d: team %q rank %q is invalid (want -1, 1, 2, ... or blank)",
				rownum, teams[i], cell)
		}
		vv := v
		p.Ranks[i] = &vv
	}

	match := normalize(row[matchIdx])
	switch {
	case match == "":
		// no constraint
	case strings.HasPrefix(match, "+"):
		want := keyOf(strings.TrimPrefix(match, "+"))
		if want == "" {
			return nil, fmt.Errorf("row %d: '+' must be followed by an email", rownum)
		}
		p.Prefer = want
	default:
		p.Exclude = keyOf(match)
	}
	if overrideIdx >= 0 {
		p.Override = normalize(row[overrideIdx])
	}
	return p, nil
}

// Validate checks cross-row references (person-match targets must be
// known respondents, nobody may name themselves), resolves each
// override to a team index (a non-blank override matching no team is
// an error naming the known teams), and checks that mutual '+' wants
// only join people in the same section — a mutual want across
// sections could never be satisfied since teams never span sections.
func (s *Survey) Validate() error {
	for _, p := range s.People {
		if p.Override != "" {
			want := keyOf(p.Override)
			var matches []int
			for i, name := range s.Teams {
				if keyOf(name) == want {
					matches = append(matches, i)
				}
			}
			switch len(matches) {
			case 0:
				return fmt.Errorf("%s has override %q, which matches no known team name (teams: %s)",
					p.Email, p.Override, strings.Join(s.Teams, ", "))
			case 1:
				p.OverrideIdx = matches[0]
			default:
				return fmt.Errorf("%s has override %q, which matches multiple teams",
					p.Email, p.Override)
			}
		}
		for _, target := range []string{p.Exclude, p.Prefer} {
			if target == "" {
				continue
			}
			q, ok := s.ByKey[target]
			if !ok {
				return fmt.Errorf("%s names unknown person %q in person match",
					p.Email, target)
			}
			if q == p {
				return fmt.Errorf("%s names themselves in person match", p.Email)
			}
		}
		if p.Exclude != "" && p.Exclude == p.Prefer {
			return fmt.Errorf("%s both excludes and wants %q", p.Email, p.Exclude)
		}
		if p.Prefer != "" {
			q := s.ByKey[p.Prefer]
			if q.Prefer == p.Key {
				if q.Section != p.Section {
					return fmt.Errorf(
						"%s and %s want to be teamed together but are in different sections (%q vs %q)",
						p.Email, q.Email, p.Section, q.Section)
				}
				if p.Exclude == q.Key || q.Exclude == p.Key {
					return fmt.Errorf(
						"%s and %s want to be teamed together but also exclude each other",
						p.Email, q.Email)
				}
			}
		}
	}
	return nil
}

// MutualPartner returns the person p is mutually bound to by '+'
// wants (both named each other), or nil. A one-sided '+' is a
// preference only; only a two-way connection binds.
func (s *Survey) MutualPartner(p *Person) *Person {
	if p.Prefer == "" {
		return nil
	}
	q, ok := s.ByKey[p.Prefer]
	if !ok || q.Prefer != p.Key {
		return nil
	}
	return q
}
