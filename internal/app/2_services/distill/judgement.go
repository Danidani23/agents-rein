package distill

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
)

// Judgement is what the judge said about one candidate.
type Judgement struct {
	Verdict    string   `yaml:"verdict" json:"verdict"`
	Related    []string `yaml:"related,omitempty" json:"related,omitempty"`
	Quotes     []Quote  `yaml:"quotes,omitempty" json:"quotes,omitempty"`
	Suggestion string   `yaml:"suggestion" json:"suggestion"`
	Level      string   `yaml:"level,omitempty" json:"level,omitempty"`
	ApplyIf    []string `yaml:"applyIf,omitempty" json:"applyIf,omitempty"`
	Reason     string   `yaml:"reason" json:"reason"`
}

// Quote is a passage the judge points at, verbatim.
type Quote struct {
	Address string `yaml:"address" json:"address"`
	Text    string `yaml:"text" json:"text"`
}

// Answer is the judge's whole reply: the candidates judged whole, and the
// units cut out of the ones that mix concerns (D34).
type Answer struct {
	Version    int               `json:"version"`
	Candidates []AnswerCandidate `json:"candidates"`
	Units      []Unit            `json:"units"`
}

// Unit is a declaration the distiller cut out of rules and skills: the
// lines it takes, verbatim, the shape it gives them, and its judgement.
type Unit struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	From        []UnitFrom `json:"from"`
	Shape       string     `json:"shape"`
	Paths       []string   `json:"paths,omitempty"`
	Description string     `json:"description,omitempty"`
	Heading     string     `json:"heading,omitempty"`
	Judgement
}

// UnitFrom is what a unit takes from one candidate: numbered lines of its
// rule or SKILL.md, or a skill's other file whole.
type UnitFrom struct {
	Candidate string `json:"candidate"`
	File      string `json:"file,omitempty"`
	Lines     string `json:"lines,omitempty"`
}

// AnswerCandidate is one entry of the reply, keyed by kind/id.
type AnswerCandidate struct {
	Candidate string `json:"candidate"`
	Judgement
}

// JudgeRun says what happened with the judge, for the report.
type JudgeRun struct {
	Backend string `yaml:"backend"`
	Status  string `yaml:"status"`
	// Session is the conversation the backend kept, for a person to open.
	Session  string   `yaml:"session"`
	Problems []string `yaml:"problems"`
	Dropped  []string `yaml:"dropped"`
}

// Statuses of a judge run.
const (
	JudgeAnswered = "answered"
	JudgeSkipped  = "skipped"
	JudgeFailed   = "failed"
)

// The vocabulary the judge answers in.
var (
	JudgeVerdicts    = []string{"new", "duplicate", "overlaps", "contradicts"}
	JudgeSuggestions = []string{"keep", "drop", "lift"}
	JudgeLevels      = []string{"organization", "team", "project", "user"}
	JudgeShapes      = []string{"always", "paths", "skill"}
)

// AnswerVersion is the version of the answer format this build reads.
const AnswerVersion = 2

// maxDescriptionLength is the longest description a skill may have.
const maxDescriptionLength = 1024

// Brief is the prompt: the task, the answer format, the candidates and the
// corpus. Everything the judge may quote is in it.
func Brief(level string, candidates []Candidate, corpus Corpus) string {
	var brief strings.Builder
	fmt.Fprintf(&brief, "You are the distiller of rein, a tool that keeps the settings of AI coding agents as declarations in one repository per organisation, at four levels: organisation, team, project, user.\n\n")
	fmt.Fprintf(&brief, "# Task\n\nCandidate declarations were distilled from a project and would be added at the level `%s`. Compare each candidate with the declarations the organisation already has, below, and judge it.\n\n", level)
	fmt.Fprintf(&brief, "A candidate that holds one concern is judged whole, as an entry of `candidates`. A rule or a skill that mixes concerns, such as a repeat of an existing declaration beside something new, a project fact inside general advice, or two procedures in one skill, is cut into `units` instead. A unit takes lines of one or more candidates by their numbers, `\"lines\": \"3-7, 9\"`, and a skill's other file whole by naming it in `file`. Lines are taken whole and verbatim, never written or reworded: the only text you add is a unit's `id`, a skill's `description`, an optional `heading` line, and `paths`. Every numbered line of a cut candidate that is not blank goes into exactly one unit, a repeat into a unit judged `duplicate` and `drop`. A candidate is judged whole or cut, never both; permission sets, hooks and tool servers are always judged whole.\n\n")
	fmt.Fprintf(&brief, "A unit's `shape`: `always` for a rule the agent reads in every session; `paths` for a rule it reads when working on files matching the `paths` globs; `skill` for a procedure it loads when a task needs it, the `description` saying when. A rule's `kind` is `rules`, a skill's `skills`.\n\n")
	fmt.Fprintf(&brief, "Every entry and every unit is judged:\n\n")
	fmt.Fprintf(&brief, "- verdict: `new` when nothing existing covers it; `duplicate` when an existing declaration says the same; `overlaps` when an existing one covers part of it or says something similar; `contradicts` when following both is impossible.\n")
	fmt.Fprintf(&brief, "- related: the addresses of the existing declarations the verdict rests on, from the headings below, none for `new`.\n")
	fmt.Fprintf(&brief, "- quotes: for `duplicate`, `overlaps` and `contradicts`, the passage of the related declaration that shows it, copied verbatim from below, with its address.\n")
	fmt.Fprintf(&brief, "- suggestion: `keep` the candidate at this level; `drop` it because an existing declaration already covers it; `lift` it to a higher level because it is general, giving `level` (organization, team, project or user) and `applyIf` labels when it should apply to some projects only.\n")
	if len(corpus.Labels) > 0 {
		fmt.Fprintf(&brief, "- applyIf labels come from the organisation's registry and nowhere else: %s. A stack-specific declaration, such as one for a language or a kind of project, is lifted with the label that names that stack.\n", strings.Join(corpus.Labels, ", "))
	}
	fmt.Fprintf(&brief, "- reason: one sentence.\n\n")
	fmt.Fprintf(&brief, "Judge meaning, not wording. Declarations that apply to this level are marked `applies`; the others belong to other teams and are here because they look related, which matters for `lift`. Answer with JSON only, no prose around it, in this exact shape:\n\n")
	fmt.Fprintf(&brief, "```json\n{\"version\": %d, \"candidates\": [{\"candidate\": \"rules/example\", \"verdict\": \"overlaps\", \"related\": [\"organizations.acme.rules.style\"], \"quotes\": [{\"address\": \"organizations.acme.rules.style\", \"text\": \"verbatim passage\"}], \"suggestion\": \"drop\", \"level\": \"\", \"applyIf\": [], \"reason\": \"one sentence\"}], \"units\": [{\"id\": \"deploy-branch\", \"kind\": \"rules\", \"from\": [{\"candidate\": \"rules/git\", \"lines\": \"5-6\"}], \"shape\": \"paths\", \"paths\": [\"deploy/**\"], \"heading\": \"\", \"description\": \"\", \"verdict\": \"new\", \"related\": [], \"quotes\": [], \"suggestion\": \"keep\", \"level\": \"\", \"applyIf\": [], \"reason\": \"one sentence\"}]}\n```\n\n", AnswerVersion)
	fmt.Fprintf(&brief, "Every candidate below is judged once, keyed by its `kind/id` heading, or cut into units. A rule's and a skill's lines are numbered.\n\n# Candidates\n\n")
	for _, candidate := range candidates {
		fmt.Fprintf(&brief, "## %s/%s\n\n", candidate.Kind, candidate.ID)
		var paths []string
		for relative := range candidate.Files {
			paths = append(paths, relative)
		}
		sort.Strings(paths)
		prose, isProse := proseFile(candidate)
		if isProse {
			frontmatter, lines := bodyLines(candidate.Files[prose])
			if frontmatter != "" {
				fmt.Fprintf(&brief, "Frontmatter, not numbered:\n%s\n\n", strings.TrimSpace(frontmatter))
			}
			for number, line := range lines {
				fmt.Fprintf(&brief, "%4d | %s\n", number+1, line)
			}
			fmt.Fprintf(&brief, "\n")
		}
		for _, relative := range paths {
			if isProse && relative == prose {
				continue
			}
			if len(paths) > 1 {
				fmt.Fprintf(&brief, "--- %s ---\n", strings.TrimPrefix(relative, path.Dir(prose)+"/"))
			}
			fmt.Fprintf(&brief, "%s\n\n", strings.TrimSpace(candidate.Files[relative]))
		}
	}
	fmt.Fprintf(&brief, "# Existing declarations\n\n")
	if len(corpus.Passages) == 0 {
		fmt.Fprintf(&brief, "None yet.\n")
	}
	for _, passage := range corpus.Passages {
		marks := "other team"
		if passage.Resolved {
			marks = "applies"
		}
		if passage.Enforcement != "" {
			marks += ", " + passage.Enforcement
		}
		if len(passage.ApplyIf) > 0 {
			marks += ", applyIf " + strings.Join(passage.ApplyIf, " ")
		}
		fmt.Fprintf(&brief, "## %s (%s)\n\n%s\n\n", passage.Address, marks, passage.Text)
	}
	return brief.String()
}

var fencePattern = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// ParseAnswer reads the judge's JSON, with or without a code fence or prose
// around it.
func ParseAnswer(text string) (Answer, error) {
	body := text
	match := fencePattern.FindStringSubmatch(text)
	if match != nil {
		body = match[1]
	}
	start := strings.Index(body, "{")
	end := strings.LastIndex(body, "}")
	if start < 0 || end < start {
		return Answer{}, fmt.Errorf("the answer holds no JSON object")
	}
	var answer Answer
	err := json.Unmarshal([]byte(body[start:end+1]), &answer)
	if err != nil {
		return Answer{}, fmt.Errorf("the answer is not the JSON shape asked for: %v", err)
	}
	return answer, nil
}

// Validate lists everything wrong with an answer: a missing or unknown
// candidate, a word outside the vocabulary, an address not in the corpus,
// a quote that is not verbatim. An empty list means the answer is believed.
func Validate(answer Answer, candidates []Candidate, corpus Corpus) (problems []string) {
	if answer.Version != AnswerVersion {
		problems = append(problems, fmt.Sprintf("version %d; this build reads %d", answer.Version, AnswerVersion))
	}
	known := map[string]bool{}
	for _, candidate := range candidates {
		known[candidate.Kind+"/"+candidate.ID] = false
	}
	texts := map[string]string{}
	for _, passage := range corpus.Passages {
		texts[passage.Address] = normalise(passage.Text)
	}
	byKey := map[string]Candidate{}
	for _, candidate := range candidates {
		byKey[candidate.Kind+"/"+candidate.ID] = candidate
	}
	taken := map[string]bool{}
	for _, entry := range answer.Candidates {
		seen, found := known[entry.Candidate]
		switch {
		case !found:
			problems = append(problems, fmt.Sprintf("%q is not a candidate", entry.Candidate))
			continue
		case seen:
			problems = append(problems, fmt.Sprintf("%s is answered twice", entry.Candidate))
			continue
		}
		known[entry.Candidate] = true
		taken[entry.Candidate] = true
		problems = append(problems, judgementProblems(entry.Candidate, entry.Judgement, texts)...)
	}
	cut := map[string]bool{}
	lineCounts := map[string]map[int]int{}
	fileCounts := map[string]map[string]int{}
	for _, unit := range answer.Units {
		label := "unit " + unit.ID
		problems = append(problems, unitProblems(unit, label, taken)...)
		for _, from := range unit.From {
			source, found := byKey[from.Candidate]
			if !found {
				problems = append(problems, fmt.Sprintf("%s: %q is not a candidate", label, from.Candidate))
				continue
			}
			prose, isProse := proseFile(source)
			if !isProse {
				problems = append(problems, fmt.Sprintf("%s: %s is not a rule or a skill", label, from.Candidate))
				continue
			}
			cut[from.Candidate] = true
			relative := prose
			if from.File != "" {
				relative = path.Join(path.Dir(prose), from.File)
			}
			if relative != prose {
				_, exists := source.Files[relative]
				switch {
				case !exists || source.Kind != address.Skills:
					problems = append(problems, fmt.Sprintf("%s: %s has no file %s", label, from.Candidate, from.File))
				case from.Lines != "":
					problems = append(problems, fmt.Sprintf("%s: %s of %s travels whole, without lines", label, from.File, from.Candidate))
				default:
					if fileCounts[from.Candidate] == nil {
						fileCounts[from.Candidate] = map[string]int{}
					}
					fileCounts[from.Candidate][from.File]++
				}
				continue
			}
			spans, err := parseLines(from.Lines)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: lines %q of %s are not numbers and ranges", label, from.Lines, from.Candidate))
				continue
			}
			_, lines := bodyLines(source.Files[prose])
			if lineCounts[from.Candidate] == nil {
				lineCounts[from.Candidate] = map[int]int{}
			}
			for _, span := range spans {
				for number := span[0]; number <= span[1]; number++ {
					if number > len(lines) {
						problems = append(problems, fmt.Sprintf("%s: line %d of %s is past its %d lines", label, number, from.Candidate, len(lines)))
						break
					}
					lineCounts[from.Candidate][number]++
				}
			}
		}
		problems = append(problems, judgementProblems(label, unit.Judgement, texts)...)
	}
	var keys []string
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch {
		case known[key] && cut[key]:
			problems = append(problems, fmt.Sprintf("%s is answered whole and cut into units", key))
		case cut[key]:
			known[key] = true
			problems = append(problems, coverageProblems(key, byKey[key], lineCounts[key], fileCounts[key])...)
		}
	}
	var missing []string
	for key, seen := range known {
		if !seen {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		problems = append(problems, fmt.Sprintf("%s is not answered", key))
	}
	return problems
}

// judgementProblems is everything wrong with one judgement: a word outside
// the vocabulary, an address not in the corpus, a quote that is not
// verbatim, a missing reason.
func judgementProblems(label string, entry Judgement, texts map[string]string) (problems []string) {
	{
		if !contains(JudgeVerdicts, entry.Verdict) {
			problems = append(problems, fmt.Sprintf("%s: verdict %q is not one of %s", label, entry.Verdict, strings.Join(JudgeVerdicts, ", ")))
		}
		if !contains(JudgeSuggestions, entry.Suggestion) {
			problems = append(problems, fmt.Sprintf("%s: suggestion %q is not one of %s", label, entry.Suggestion, strings.Join(JudgeSuggestions, ", ")))
		}
		if entry.Suggestion == "lift" && !contains(JudgeLevels, entry.Level) {
			problems = append(problems, fmt.Sprintf("%s: lift needs a level, one of %s", label, strings.Join(JudgeLevels, ", ")))
		}
		if entry.Verdict != "new" && len(entry.Related) == 0 {
			problems = append(problems, fmt.Sprintf("%s: verdict %s names no related declaration", label, entry.Verdict))
		}
		for _, related := range entry.Related {
			_, found := texts[related]
			if !found {
				problems = append(problems, fmt.Sprintf("%s: related %q is not an existing declaration in the brief", label, related))
			}
		}
		for _, quote := range entry.Quotes {
			text, found := texts[quote.Address]
			switch {
			case !found:
				problems = append(problems, fmt.Sprintf("%s: quote from %q, which is not in the brief", label, quote.Address))
			case !strings.Contains(text, normalise(quote.Text)):
				problems = append(problems, fmt.Sprintf("%s: the quote %q is not verbatim in %s", label, shorten(quote.Text), quote.Address))
			}
		}
		if strings.TrimSpace(entry.Reason) == "" {
			problems = append(problems, fmt.Sprintf("%s: no reason", label))
		}
	}
	return problems
}

// unitProblems is everything wrong with a unit's own fields: its id, its
// kind and shape, and the text it may add.
func unitProblems(unit Unit, label string, taken map[string]bool) (problems []string) {
	key := unit.Kind + "/" + unit.ID
	switch {
	case !address.IsSlug(unit.ID):
		problems = append(problems, fmt.Sprintf("%s: the id is not a slug", label))
	case taken[key]:
		problems = append(problems, fmt.Sprintf("%s: the id is taken", label))
	}
	taken[key] = true
	fits := (unit.Kind == address.Rules && (unit.Shape == "always" || unit.Shape == "paths")) || (unit.Kind == address.Skills && unit.Shape == "skill")
	if !fits {
		problems = append(problems, fmt.Sprintf("%s: shape %q does not fit %s", label, unit.Shape, unit.Kind))
	}
	if unit.Shape == "paths" && len(unit.Paths) == 0 {
		problems = append(problems, fmt.Sprintf("%s: shape paths needs paths", label))
	}
	if unit.Shape == "skill" && strings.TrimSpace(unit.Description) == "" {
		problems = append(problems, fmt.Sprintf("%s: a skill needs a description", label))
	}
	if len(unit.Description) > maxDescriptionLength {
		problems = append(problems, fmt.Sprintf("%s: the description is longer than %d characters", label, maxDescriptionLength))
	}
	if unit.Heading != "" && (!strings.HasPrefix(unit.Heading, "#") || strings.Contains(unit.Heading, "\n")) {
		problems = append(problems, fmt.Sprintf("%s: a heading is one line that starts with #", label))
	}
	if len(unit.From) == 0 {
		problems = append(problems, fmt.Sprintf("%s: takes nothing", label))
	}
	return problems
}

// coverageProblems is every line of a cut candidate that no unit or two
// units took, and every other file of a skill that travels nowhere.
func coverageProblems(key string, candidate Candidate, lineCounts map[int]int, fileCounts map[string]int) (problems []string) {
	prose, _ := proseFile(candidate)
	_, lines := bodyLines(candidate.Files[prose])
	var missing []int
	for index, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch lineCounts[index+1] {
		case 0:
			missing = append(missing, index+1)
		case 1:
		default:
			problems = append(problems, fmt.Sprintf("%s: line %d is in two units", key, index+1))
		}
	}
	switch {
	case len(missing) == 1:
		problems = append(problems, fmt.Sprintf("%s: line %d is in no unit", key, missing[0]))
	case len(missing) > 1:
		problems = append(problems, fmt.Sprintf("%s: lines %s are in no unit", key, spanText(missing)))
	}
	var others []string
	for relative := range candidate.Files {
		if relative != prose {
			others = append(others, strings.TrimPrefix(relative, path.Dir(prose)+"/"))
		}
	}
	sort.Strings(others)
	for _, other := range others {
		switch fileCounts[other] {
		case 0:
			problems = append(problems, fmt.Sprintf("%s: %s is in no unit", key, other))
		case 1:
		default:
			problems = append(problems, fmt.Sprintf("%s: %s is in two units", key, other))
		}
	}
	return problems
}

// proseFile is the file of a rule or a skill whose lines the distiller
// numbers and may cut; a skill's other files travel whole.
func proseFile(candidate Candidate) (string, bool) {
	switch candidate.Kind {
	case address.Rules:
		if len(candidate.Files) == 1 {
			for relative := range candidate.Files {
				return relative, true
			}
		}
	case address.Skills:
		relative := path.Join(address.Skills, candidate.ID, "SKILL.md")
		_, found := candidate.Files[relative]
		return relative, found
	}
	return "", false
}

// bodyLines is a declaration file's frontmatter and the lines of its body,
// numbered from one by their position.
func bodyLines(content string) (frontmatter string, lines []string) {
	frontmatter, body := splitFrontmatter(content)
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return frontmatter, nil
	}
	return frontmatter, strings.Split(body, "\n")
}

// parseLines reads "3-7, 9" into spans of line numbers.
func parseLines(text string) ([][2]int, error) {
	var spans [][2]int
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		first, last, isRange := strings.Cut(part, "-")
		start, err := strconv.Atoi(strings.TrimSpace(first))
		if err != nil || start < 1 {
			return nil, fmt.Errorf("%q is not a line number", part)
		}
		end := start
		if isRange {
			end, err = strconv.Atoi(strings.TrimSpace(last))
			if err != nil || end < start {
				return nil, fmt.Errorf("%q is not a range", part)
			}
		}
		spans = append(spans, [2]int{start, end})
	}
	return spans, nil
}

// spanText writes line numbers back as "3-5, 7".
func spanText(numbers []int) string {
	var parts []string
	for index := 0; index < len(numbers); {
		end := index
		for end+1 < len(numbers) && numbers[end+1] == numbers[end]+1 {
			end++
		}
		if end == index {
			parts = append(parts, strconv.Itoa(numbers[index]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", numbers[index], numbers[end]))
		}
		index = end + 1
	}
	return strings.Join(parts, ", ")
}

// Apply attaches the judgements to the candidates judged whole and puts
// the units in place of the candidates they were cut from.
func Apply(answer Answer, candidates []Candidate) []Candidate {
	byKey := map[string]Candidate{}
	for _, candidate := range candidates {
		byKey[candidate.Kind+"/"+candidate.ID] = candidate
	}
	judged := map[string]Judgement{}
	for _, entry := range answer.Candidates {
		judged[entry.Candidate] = entry.Judgement
	}
	cut := map[string]bool{}
	for _, unit := range answer.Units {
		for _, from := range unit.From {
			cut[from.Candidate] = true
		}
	}
	var result []Candidate
	for _, candidate := range candidates {
		key := candidate.Kind + "/" + candidate.ID
		if cut[key] {
			continue
		}
		judgement, found := judged[key]
		if found {
			candidate.Judgement = &judgement
		}
		result = append(result, candidate)
	}
	for _, unit := range answer.Units {
		result = append(result, unitCandidate(unit, byKey))
	}
	return result
}

// skillFrontmatter is what a skill cut out by the distiller opens with.
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// unitCandidate builds the declaration a unit describes: its lines in the
// order taken, a blank line where they jump, under the heading and the
// frontmatter its shape asks for.
func unitCandidate(unit Unit, byKey map[string]Candidate) Candidate {
	var body []string
	others := map[string]string{}
	var sources, taken []string
	for _, from := range unit.From {
		source := byKey[from.Candidate]
		sources = append(sources, source.Sources...)
		prose, _ := proseFile(source)
		relative := prose
		if from.File != "" {
			relative = path.Join(path.Dir(prose), from.File)
		}
		if relative != prose {
			others[from.File] = source.Files[relative]
			taken = append(taken, from.File+" of "+from.Candidate)
			continue
		}
		_, lines := bodyLines(source.Files[prose])
		spans, _ := parseLines(from.Lines)
		previous := -1
		for _, span := range spans {
			if len(body) > 0 && span[0] != previous+1 {
				body = append(body, "")
			}
			body = append(body, lines[span[0]-1:span[1]]...)
			previous = span[1]
		}
		word := "lines"
		if len(spans) == 1 && spans[0][0] == spans[0][1] {
			word = "line"
		}
		taken = append(taken, fmt.Sprintf("%s %s of %s", word, spanText(expand(spans)), from.Candidate))
	}
	text := strings.Trim(strings.Join(body, "\n"), "\n") + "\n"
	if unit.Heading != "" {
		text = unit.Heading + "\n\n" + text
	}
	files := map[string]string{}
	switch unit.Shape {
	case "paths":
		frontmatter, _ := yaml.Marshal(map[string][]string{"paths": unit.Paths})
		files[path.Join(address.Rules, unit.ID+".md")] = "---\n" + string(frontmatter) + "---\n" + text
	case "skill":
		frontmatter, _ := yaml.Marshal(skillFrontmatter{Name: unit.ID, Description: unit.Description})
		files[path.Join(address.Skills, unit.ID, "SKILL.md")] = "---\n" + string(frontmatter) + "---\n\n" + text
		for name, content := range others {
			files[path.Join(address.Skills, unit.ID, name)] = content
		}
	default:
		files[path.Join(address.Rules, unit.ID+".md")] = text
	}
	sort.Strings(sources)
	var unique []string
	for _, source := range sources {
		if len(unique) == 0 || unique[len(unique)-1] != source {
			unique = append(unique, source)
		}
	}
	judgement := unit.Judgement
	return Candidate{Kind: unit.Kind, ID: unit.ID, Sources: unique, Notes: []string{"cut by the distiller from " + strings.Join(taken, ", ")}, Files: files, Judgement: &judgement}
}

// expand lists every line number of the spans, in order.
func expand(spans [][2]int) []int {
	var numbers []int
	for _, span := range spans {
		for number := span[0]; number <= span[1]; number++ {
			numbers = append(numbers, number)
		}
	}
	return numbers
}

var whitespace = regexp.MustCompile(`\s+`)

func normalise(text string) string {
	return strings.ToLower(whitespace.ReplaceAllString(strings.TrimSpace(text), " "))
}

func shorten(text string) string {
	if len(text) > 60 {
		return text[:57] + "..."
	}
	return text
}

func contains(list []string, item string) bool {
	for _, candidate := range list {
		if candidate == item {
			return true
		}
	}
	return false
}
