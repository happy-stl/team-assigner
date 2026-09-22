// Command team-assigner assigns survey respondents to teams
// straight off a survey CSV. See the README for the file layout,
// the ranking rules, and the assignment formula.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grimwm/team-assigner/internal/assign"
	"github.com/grimwm/team-assigner/internal/survey"
)

const usage = `team-assigner: assign survey respondents to teams

Usage:
  team-assigner assign -input VOTES.csv -output TEAMS.csv [-seed N]
  team-assigner validate -input VOTES.csv

Commands:
  assign    read VOTES.csv, assign teams, write TEAMS.csv
  validate  check VOTES.csv for errors without assigning

Options:
  -input     survey CSV to read
  -output    assignment CSV to write (assign only)
  -seed      RNG seed for reproducible runs (assign only; default: time-based)
  -max-size  most people per team (default 4)
  -min-size  fewest people per team (default: one less than -max-size)
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "assign":
		return runAssign(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "-h", "-help", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runAssign(args []string) int {
	fs := flag.NewFlagSet("assign", flag.ContinueOnError)
	input := fs.String("input", "", "survey CSV to read")
	output := fs.String("output", "", "assignment CSV to write")
	maxSize := fs.Int("max-size", 4, "most people per team")
	minSize := fs.Int("min-size", 0, "fewest people per team (0 = one less than -max-size)")
	var seed seedFlag
	fs.Var(&seed, "seed", "RNG seed for reproducible runs (default: time-based)")
	// Accept -seed N and positional INPUT OUTPUT fallbacks.
	var pos []string
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pos = fs.Args()
	if *input == "" && len(pos) > 0 {
		*input = pos[0]
		pos = pos[1:]
	}
	if *output == "" && len(pos) > 0 {
		*output = pos[0]
	}
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "assign needs -input VOTES.csv and -output TEAMS.csv")
		return 2
	}

	s, err := loadSurvey(*input)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	useed := time.Now().UnixNano()
	if seed.set {
		useed = seed.val
	}
	min := resolveMinSize(*minSize, *maxSize)
	if min < 1 || *maxSize < min {
		fmt.Fprintf(os.Stderr, "error: -min-size %d and -max-size %d are contradictory\n",
			*minSize, *maxSize)
		return 2
	}
	rng := rand.New(rand.NewSource(useed))
	slots, report, err := assign.Assign(s, rng, min, *maxSize)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	out, err := os.Create(*output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	werr := assign.WriteCSV(out, s, slots)
	cerr := out.Close()
	if werr != nil || cerr != nil {
		fmt.Fprintln(os.Stderr, "error: writing output:", firstErr(werr, cerr))
		return 1
	}

	// The seed always prints on its own line so any run can be
	// repeated exactly by passing it back with -seed.
	if report.Clean() {
		fmt.Printf("assigned %d people to %d teams, wrote %s\n",
			len(s.People), len(slots), *output)
		fmt.Printf("seed: %d (re-run with -seed %d)\n", useed, useed)
		printUnassigned(s, slots, report)
		return 0
	}
	// Infeasible strictly: the CSV holds the best-effort remap, and
	// the why goes to stderr (capture with 2> report.txt).
	fmt.Printf("assigned %d people to %d teams with %d relaxed constraint(s), wrote %s\n",
		len(s.People), len(slots), report.Relaxed(), *output)
	fmt.Printf("seed: %d (re-run with -seed %d)\n", useed, useed)
	fmt.Fprint(os.Stderr, report.Describe())
	return 1
}

func runValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	input := fs.String("input", "", "survey CSV to read")
	maxSize := fs.Int("max-size", 4, "most people per team")
	minSize := fs.Int("min-size", 0, "fewest people per team (0 = one less than -max-size)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path := *input
	if path == "" && len(fs.Args()) > 0 {
		path = fs.Args()[0]
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "validate needs -input VOTES.csv")
		return 2
	}
	s, err := loadSurvey(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	sections := map[string]int{}
	for _, p := range s.People {
		sections[p.Section]++
	}
	min := resolveMinSize(*minSize, *maxSize)
	if min < 1 || *maxSize < min {
		fmt.Fprintf(os.Stderr, "error: -min-size %d and -max-size %d are contradictory\n",
			*minSize, *maxSize)
		return 2
	}
	for sec, n := range sections {
		if _, err := assign.Partition(n, min, *maxSize); err != nil {
			fmt.Fprintf(os.Stderr, "error: section %q: %v\n", sec, err)
			return 1
		}
	}
	for _, p := range s.People {
		allVeto := true
		for _, r := range p.Ranks {
			if r == nil || *r != -1 {
				allVeto = false
				break
			}
		}
		if allVeto {
			fmt.Fprintf(os.Stderr, "error: %s vetoed every project; assign will seat them anyway and report it\n",
				p.Email)
			return 1
		}
	}
	fmt.Printf("valid: %d people, %d projects, %d sections\n",
		len(s.People), len(s.Teams), len(sections))
	return 0
}

func loadSurvey(path string) (*survey.Survey, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s, err := survey.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// printUnassigned notes projects that got no team because team
// slots ran out first. Informational only: fewer slots than projects
// is ordinary survey design, not a broken constraint.
func printUnassigned(s *survey.Survey, slots []*assign.Slot, report *assign.Report) {
	if len(report.Unassigned) == 0 {
		return
	}
	quoted := make([]string, 0, len(report.Unassigned))
	for _, name := range report.Unassigned {
		quoted = append(quoted, strconv.Quote(name))
	}
	fmt.Printf("note: no team for %s (%d projects, %d team slots)\n",
		strings.Join(quoted, ", "), len(s.Teams), len(slots))
}

// resolveMinSize settles the -min-size flag: an explicit positive
// value wins, while anything else means one less than the max,
// floored at 1 so -max-size 1 still yields a usable bound. A min
// above the max surfaces later as a Partition error.
func resolveMinSize(flag, maxSize int) int {
	if flag > 0 {
		return flag
	}
	if maxSize-1 >= 1 {
		return maxSize - 1
	}
	return 1
}

// seedFlag is an int64 flag that records whether it was set.
type seedFlag struct {
	set bool
	val int64
}

func (s *seedFlag) String() string { return fmt.Sprint(s.val) }

func (s *seedFlag) Set(text string) error {
	var v int64
	if _, err := fmt.Sscanf(text, "%d", &v); err != nil {
		return fmt.Errorf("invalid -seed %q: want an integer", text)
	}
	s.set, s.val = true, v
	return nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
