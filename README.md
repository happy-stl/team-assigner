# Team Assigner

A Go CLI that assigns people to teams straight off a survey CSV — no
database, no config files. It reads one CSV of rankings, computes the
assignment, and writes one CSV of teams.

## Install

Requires Go 1.24+.

```bash
go build -o team-assigner ./cmd/team-assigner
```

## Usage

```bash
# Check a survey file for errors without assigning:
team-assigner validate -input votes.csv

# Assign teams (seed is optional; without it the run is randomized):
team-assigner assign -input votes.csv -output teams.csv -seed 1

# Larger or smaller teams:
team-assigner assign -input votes.csv -output teams.csv -max-size 5
team-assigner assign -input votes.csv -output teams.csv -max-size 5 -min-size 2
```

`assign` prints a summary plus the seed on its own line, so any run can
be reproduced exactly by passing that seed back with `-seed`.
`-min-size` defaults to one less than `-max-size` (so the default is
teams of 3–4). Exit codes: 0 means every constraint held, 1 means the
output is a best-effort remap (details on stderr — see below), 2 means
a usage or flag error.

Try it with the bundled example (synthetic data):

```bash
team-assigner assign -input testdata/sample_votes.csv -output /tmp/teams.csv -seed 1
```

## Input layout

The survey CSV has one header row plus one row per respondent:

```
timestamp, email, section, <team 1>, <team 2>, ..., <person match>
```

- `timestamp` is ignored.
- `email` identifies the respondent.
- `section` groups respondents. Only people in the same section may be
  placed on the same team. A blank section puts everyone in one shared
  section.
- Each team column is headed by the team (project) name and holds that
  respondent's rank: `-1` means "do not put me on this team", `1` is
  their favorite, and larger numbers are progressively less liked. A
  blank cell means no opinion: still placeable, but only as a last
  resort. Anything else (`0`, below `-1`, non-numeric) is an error.
- `person match` holds an optional constraint toward one other person:
  a plain email means "do not team me with them" (an exclusion, honored
  whichever side stated it), while an email beginning with `+` means "I
  want to be teamed with them". A `+` want binds the pair to the same
  team **only when both people name each other** — a two-way connection.
  A one-sided `+` is kept as a soft preference (see scoring below).
- An optional `override` column may come last (headed `override`,
  case-insensitive): a teacher mandate. A blank cell means no override;
  anything else must match a team name (case-insensitive) or validation
  fails naming the known teams. A matched respondent joins a team
  playing that project regardless of ranks — overrides seed first and
  restrict the fill — while everything else (sections, vetoes,
  exclusions, pairs) still applies and is still reported. Section still
  binds: the mandate picks the project, not the section, so a person
  whose section fields no team on that project fails loudly instead of
  landing elsewhere. Unsatisfiable mandates (more demanders than seats)
  fail the same way.

Every `person match` target must be a respondent in the file, nobody may
name themselves, mutual `+` partners must share a section, and mutual
partners may not simultaneously exclude each other.

## Team sizes

Each section is split into teams of `-min-size` to `-max-size` people
(defaults 3–4). Splits use the fewest teams possible, preferring larger
teams. Headcounts with no exact split don't fail: a 5-person section at
3–4 becomes one team of five (kept together, one head over max) and the
remap report says so — see below. Contradictory bounds (`-min-size`
above `-max-size`, minimum below 1) are usage errors.

## The formula

Let rank(v, p) be respondent v's rank for project p (blank = worst,
veto = forbidden) and let popularity(p) be the number of still
unassigned respondents who ranked p as 1.

1. **Load projects.** Teacher overrides first (most demanded unloaded
   project, seeded by a demander). Then, while a team still needs a
   project, take the most popular not-yet-loaded project across all
   sections — ties go to the leftmost CSV column — and seed it with one
   uniformly random respondent who ranked it 1, opening a team in that
   respondent's section. Every project gets its turn while slots last: projects
   nobody ranked first are seeded by their closest-ranked respondent
   instead, so a project goes teamless only when slots run out first —
   those are listed in a stdout `note: no team for ...` line, which is
   information, not a relaxation. Leftover slots after all projects
   load reuse projects by the same popularity rule, seeded by each
   section's closest-ranked respondent (stated ranks, then blanks,
   then vetoes as a last resort).
2. **Fill seats.** Seat everyone left with the assignment minimizing the
   penalty tuple

   (vetoes, exclusions, splits, rank)

   compared lexicographically, where vetoes counts placements on −1
   projects, exclusions counts co-seated excluded pairs, splits counts
   separated mutual `+` pairs, and rank is total 4·rank minus `+`
   affinity toward teammates (2 mutual, 1 one-sided, 0 otherwise).
   Rank always outranks affinity, affinity outranks chance: remaining
   ties — respondents who ranked a choice equally — are ordered uniformly
   at random. Blanks cost more than any stated rank. This is the whole
   formula, and it answers the infeasible case up front: a strict split
   is exactly penalty (0, 0, 0, r), so when the CSV admits one, nothing
   outranks it; when it does not, the same minimization keeps descending
   — second-best choices, then third-best, and so on down the rabbit
   hole until everyone is seated — with best choices everywhere else.

Two deliberate choices deserve a note. First, filling is a global
optimum per section rather than team-by-team greed: seating each team's
closest match first can strand a respondent behind a veto or exclusion
even when a valid split exists, and no reshuffling of ties escapes such
dead ends. Optimizing lets fellow fans land together whenever any valid
split allows it. Second, a seed can itself corner the remainder, so
layouts are retried with fresh random choices (up to 32 attempts, stopping
after 8 with no improvement, deterministic per seed), keeping the least
penalty found.

## When no strict split exists

`assign` still writes the CSV — the best-effort remap — prints a summary
naming the relaxed-constraint count, exits 1, and explains the why on
stderr (capture it with `2> report.txt`):

```
no strict split exists; best-effort remap relaxes:
  sizing: section "101" has 5 people, cannot split into teams of 3-4 → using 5
  veto: dan@example.com on "Drift" (ranked -1); closest alternative "Blue" (rank 3)
  exclusion: amy@example.com and lee@example.com share "Red" (excluded by amy@example.com)
  split: hal@example.com and kim@example.com separated ("Red" vs "Blue")
```

Each line is one relaxed constraint: sizing deviations from the headcount
split, veto placements with the closest non-vetoed alternative offered in
that section (or "vetoed every project" when nothing better existed),
co-seated exclusions with who stated them, and separated mutual pairs with
both teams. `validate` predicts the two cheapest failures up front —
unsplittable headcounts and respondents who vetoed every project — so run
it first on a new file.

## Output layout

A header row of `Team,Person 1, ...` (spanning the largest team) followed
by one row per team: the team name first, then the member emails.
Shorter rows are padded with blanks so the CSV stays rectangular.

```
Team,Person 1,Person 2,Person 3,Person 4
Beacon,amy@example.com,ben@example.com,cat@example.com,dan@example.com
Cipher,amy@example.com,ben@example.com,eli@example.com,gus@example.com
```

Rows sort by team name, emails sort within each row. Every respondent
appears exactly once, on a team from their own section. On a strict
split (exit 0) every team is within the size bounds, nobody sits on a
vetoed project or beside an excluded person, and mutual `+` pairs are
always together; on a remap (exit 1) the stderr report lists each
exception.

## Privacy note

Survey responses are real people's data: `*.csv` files are git-ignored
by default and only `testdata/` (hand-made synthetic examples) is
tracked. Keep response files out of the repo.

## Development

```bash
make build       # compile to ./bin/team-assigner
make test        # unit + CLI + end-to-end (synthetic fixtures only)
make vet fmt     # static checks; fmt fails if gofmt wants changes
make run-example # assign testdata/sample_votes.csv (SEED=7, MAX_SIZE=5, MIN_SIZE=2 overrides)
```

Layout:

- `cmd/team-assigner/` — CLI (`assign`, `validate`).
- `internal/survey/` — CSV parsing and cross-row validation.
- `internal/assign/` — sizing, the loading ritual, and the optimal fill.
- `testdata/sample_votes.csv` — synthetic example exercised by the tests.
