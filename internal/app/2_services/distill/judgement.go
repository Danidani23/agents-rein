package distill

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
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

// Answer is the judge's whole reply.
type Answer struct {
	Version    int               `json:"version"`
	Candidates []AnswerCandidate `json:"candidates"`
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
)

// AnswerVersion is the version of the answer format this build reads.
const AnswerVersion = 1

// Brief is the prompt: the task, the answer format, the candidates and the
// corpus. Everything the judge may quote is in it.
func Brief(level string, candidates []Candidate, corpus Corpus) string {
	var brief strings.Builder
	fmt.Fprintf(&brief, "You are the judge of rein, a tool that keeps the settings of AI coding agents as declarations in one repository per organisation, at four levels: organisation, team, project, user.\n\n")
	fmt.Fprintf(&brief, "# Task\n\nCandidate declarations were distilled from a project and would be added at the level `%s`. Compare each candidate with the declarations the organisation already has, below, and judge it:\n\n", level)
	fmt.Fprintf(&brief, "- verdict: `new` when nothing existing covers it; `duplicate` when an existing declaration says the same; `overlaps` when an existing one covers part of it or says something similar; `contradicts` when following both is impossible.\n")
	fmt.Fprintf(&brief, "- related: the addresses of the existing declarations the verdict rests on, from the headings below, none for `new`.\n")
	fmt.Fprintf(&brief, "- quotes: for `duplicate`, `overlaps` and `contradicts`, the passage of the related declaration that shows it, copied verbatim from below, with its address.\n")
	fmt.Fprintf(&brief, "- suggestion: `keep` the candidate at this level; `drop` it because an existing declaration already covers it; `lift` it to a higher level because it is general, giving `level` (organization, team, project or user) and `applyIf` labels when it should apply to some projects only.\n")
	if len(corpus.Labels) > 0 {
		fmt.Fprintf(&brief, "- applyIf labels come from the organisation's registry and nowhere else: %s. A stack-specific declaration, such as one for a language or a kind of project, is lifted with the label that names that stack.\n", strings.Join(corpus.Labels, ", "))
	}
	fmt.Fprintf(&brief, "- reason: one sentence.\n\n")
	fmt.Fprintf(&brief, "Judge meaning, not wording. Declarations that apply to this level are marked `applies`; the others belong to other teams and are here because they look related, which matters for `lift`. Answer with JSON only, no prose around it, in this exact shape:\n\n")
	fmt.Fprintf(&brief, "```json\n{\"version\": %d, \"candidates\": [{\"candidate\": \"rules/example\", \"verdict\": \"overlaps\", \"related\": [\"organizations.acme.rules.style\"], \"quotes\": [{\"address\": \"organizations.acme.rules.style\", \"text\": \"verbatim passage\"}], \"suggestion\": \"drop\", \"level\": \"\", \"applyIf\": [], \"reason\": \"one sentence\"}]}\n```\n\n", AnswerVersion)
	fmt.Fprintf(&brief, "Every candidate below gets exactly one entry, keyed by its `kind/id` heading.\n\n# Candidates\n\n")
	for _, candidate := range candidates {
		fmt.Fprintf(&brief, "## %s/%s\n\n", candidate.Kind, candidate.ID)
		var paths []string
		for relative := range candidate.Files {
			paths = append(paths, relative)
		}
		sort.Strings(paths)
		for _, relative := range paths {
			if len(paths) > 1 {
				fmt.Fprintf(&brief, "--- %s ---\n", relative)
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
		if !contains(JudgeVerdicts, entry.Verdict) {
			problems = append(problems, fmt.Sprintf("%s: verdict %q is not one of %s", entry.Candidate, entry.Verdict, strings.Join(JudgeVerdicts, ", ")))
		}
		if !contains(JudgeSuggestions, entry.Suggestion) {
			problems = append(problems, fmt.Sprintf("%s: suggestion %q is not one of %s", entry.Candidate, entry.Suggestion, strings.Join(JudgeSuggestions, ", ")))
		}
		if entry.Suggestion == "lift" && !contains(JudgeLevels, entry.Level) {
			problems = append(problems, fmt.Sprintf("%s: lift needs a level, one of %s", entry.Candidate, strings.Join(JudgeLevels, ", ")))
		}
		if entry.Verdict != "new" && len(entry.Related) == 0 {
			problems = append(problems, fmt.Sprintf("%s: verdict %s names no related declaration", entry.Candidate, entry.Verdict))
		}
		for _, related := range entry.Related {
			_, found := texts[related]
			if !found {
				problems = append(problems, fmt.Sprintf("%s: related %q is not an existing declaration in the brief", entry.Candidate, related))
			}
		}
		for _, quote := range entry.Quotes {
			text, found := texts[quote.Address]
			switch {
			case !found:
				problems = append(problems, fmt.Sprintf("%s: quote from %q, which is not in the brief", entry.Candidate, quote.Address))
			case !strings.Contains(text, normalise(quote.Text)):
				problems = append(problems, fmt.Sprintf("%s: the quote %q is not verbatim in %s", entry.Candidate, shorten(quote.Text), quote.Address))
			}
		}
		if strings.TrimSpace(entry.Reason) == "" {
			problems = append(problems, fmt.Sprintf("%s: no reason", entry.Candidate))
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

// Apply attaches the judgements to the candidates.
func Apply(answer Answer, candidates []Candidate) {
	byKey := map[string]Judgement{}
	for _, entry := range answer.Candidates {
		byKey[entry.Candidate] = entry.Judgement
	}
	for index := range candidates {
		judgement, found := byKey[candidates[index].Kind+"/"+candidates[index].ID]
		if found {
			copied := judgement
			candidates[index].Judgement = &copied
		}
	}
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
