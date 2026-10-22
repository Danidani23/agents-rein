// Package proposal is the format of a proposal: one YAML document that
// holds every candidate with its content, where it goes and why. distill
// writes it, a person edits it, propose reads it and does what it says, and
// afterwards it is kept as history.
package proposal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
)

// Version is the format version this build writes and reads.
const Version = 1

// Status is where a proposal stands.
type Status string

// The statuses.
const (
	StatusPending  Status = "pending"
	StatusProposed Status = "proposed"
)

// Header is the comment at the top of every proposal file.
const Header = `# Written by rein distill. Review and edit it, then rein propose. A candidate with
# include: false stays out; level, applyIf, doNotApplyIf, enforcement and the files
# are yours to change. Never edit a proposed one; distill again instead.
`

// Proposal is the document.
type Proposal struct {
	Version       int              `yaml:"version"`
	ID            string           `yaml:"id"`
	Status        Status           `yaml:"status"`
	Organization  string           `yaml:"organization"`
	DistilledFrom string           `yaml:"distilled_from"`
	Snapshot      Snapshot         `yaml:"snapshot"`
	DistilledAt   time.Time        `yaml:"distilled_at"`
	Judge         distill.JudgeRun `yaml:"judge"`
	Skipped       []string         `yaml:"skipped"`
	ProposedAt    time.Time        `yaml:"proposed_at,omitempty"`
	PullRequest   string           `yaml:"pull_request"`
	Branch        string           `yaml:"branch"`
	Candidates    []Candidate      `yaml:"candidates"`
}

// Snapshot names what the proposal was distilled from.
type Snapshot struct {
	Path    string    `yaml:"path"`
	SHA256  string    `yaml:"sha256"`
	TakenAt time.Time `yaml:"taken_at"`
}

// Candidate is one declaration: where it goes, what the comparison and the
// judge said, and its files, keyed by their name inside the declaration's
// folder: SKILL.md, hook.yaml, or docs.md for the rule docs.
type Candidate struct {
	Kind         string            `yaml:"kind"`
	ID           string            `yaml:"id"`
	Include      bool              `yaml:"include"`
	Level        string            `yaml:"level"`
	ApplyIf      []string          `yaml:"applyIf"`
	DoNotApplyIf []string          `yaml:"doNotApplyIf"`
	Enforcement  string            `yaml:"enforcement"`
	Sources      []string          `yaml:"sources"`
	Compared     Comparison        `yaml:"compared"`
	Judge        *Judgement        `yaml:"judge"`
	Notes        []string          `yaml:"notes"`
	Files        map[string]string `yaml:"files"`
}

// Comparison is what the organisation already has, by content or by id.
type Comparison struct {
	Verdict string   `yaml:"verdict"`
	Related []string `yaml:"related"`
}

// Judgement is the judge's view with the level resolved to an address.
type Judgement struct {
	Verdict    string          `yaml:"verdict"`
	Suggestion string          `yaml:"suggestion"`
	Level      string          `yaml:"level"`
	ApplyIf    []string        `yaml:"applyIf"`
	Reason     string          `yaml:"reason"`
	Quotes     []distill.Quote `yaml:"quotes"`
}

// DigestOf is the SHA-256 of the snapshot files, in order, as hex.
func DigestOf(snapshotContents ...[]byte) string {
	digest := sha256.New()
	for _, content := range snapshotContents {
		digest.Write(content)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// IDOf is the proposal id of what was distilled: the first twelve hex
// digits of the digest.
func IDOf(snapshotContents ...[]byte) string {
	return DigestOf(snapshotContents...)[:12]
}

// FileName is a pending proposal's file name.
func FileName(id string) string {
	return "proposal-" + id + ".yaml"
}

// HistoryName is a proposed proposal's file name, the time first so the
// folder lists in order.
func HistoryName(id string, proposedAt time.Time) string {
	return proposedAt.UTC().Format("20060102-150405") + "-" + id + ".yaml"
}

// FileKeyOf is the key a candidate file has inside the document: the path
// inside the declaration's folder, or the file's name for a rule, a
// permission set or a tool server.
func FileKeyOf(kind string, id string, relative string) string {
	prefix := kind + "/" + id + "/"
	if strings.HasPrefix(relative, prefix) {
		return strings.TrimPrefix(relative, prefix)
	}
	return path.Base(relative)
}

// FromCandidate turns a distilled candidate into the document's form, at
// the level given, with the judge's view already resolved by the caller.
func FromCandidate(candidate distill.Candidate, level string, include bool, applyIf []string, judge *Judgement) Candidate {
	files := map[string]string{}
	for relative, content := range candidate.Files {
		files[FileKeyOf(candidate.Kind, candidate.ID, relative)] = content
	}
	return Candidate{
		Kind:         candidate.Kind,
		ID:           candidate.ID,
		Include:      include,
		Level:        level,
		ApplyIf:      orEmpty(applyIf),
		DoNotApplyIf: []string{},
		Enforcement:  "",
		Sources:      orEmpty(candidate.Sources),
		Compared:     Comparison{Verdict: candidate.Verdict, Related: orEmpty(candidate.Related)},
		Judge:        judge,
		Notes:        orEmpty(candidate.Notes),
		Files:        files,
	}
}

// Read parses a proposal file.
func Read(filePath string) (Proposal, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return Proposal{}, err
	}
	var document Proposal
	err = yaml.Unmarshal(content, &document)
	if err != nil {
		return Proposal{}, fmt.Errorf("%s is not a proposal: %w", filePath, err)
	}
	if document.Version != Version {
		return Proposal{}, fmt.Errorf("%s is a proposal of version %d; this rein reads version %d", filePath, document.Version, Version)
	}
	return document, nil
}

// Write writes the document with its header, through a temporary file so
// a reader never sees half of it.
func (proposal Proposal) Write(filePath string) error {
	encoded, err := yaml.Marshal(proposal)
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Dir(filePath), 0o700)
	if err != nil {
		return err
	}
	temporary := filePath + ".writing"
	err = os.WriteFile(temporary, append([]byte(Header), encoded...), 0o600)
	if err != nil {
		return err
	}
	return os.Rename(temporary, filePath)
}

// Check says what is wrong with a proposal before propose: a proposed one
// handed in again, a level that is not one, the same kind and id twice at
// a level, a label outside the registry or two of a single-valued
// category, an unknown enforcement, an included candidate without files.
func (proposal Proposal) Check(registry map[string]declaration.LabelCategory) []string {
	var problems []string
	if proposal.Status == StatusProposed {
		problems = append(problems, fmt.Sprintf("the proposal was proposed on %s as %s; distill again for a new one", proposal.ProposedAt.Format("2006-01-02"), proposal.PullRequest))
	}
	seen := map[string]bool{}
	for _, candidate := range proposal.Candidates {
		where := candidate.Kind + "/" + candidate.ID
		if !candidate.Include {
			continue
		}
		parsed, err := address.Parse(candidate.Level)
		if err != nil || parsed.IsDeclaration() {
			problems = append(problems, fmt.Sprintf("%s: level %q is not one; organizations.<org>, organizations.<org>.teams.<team>, organizations.<org>.projects.<project> or users.<user>", where, candidate.Level))
		}
		key := candidate.Level + "/" + where
		if seen[key] {
			problems = append(problems, fmt.Sprintf("%s appears twice at %s", where, candidate.Level))
		}
		seen[key] = true
		for _, label := range append(append([]string{}, candidate.ApplyIf...), candidate.DoNotApplyIf...) {
			category, value, ok := declaration.SplitLabel(label)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: label %q is not category:value", where, label))
				continue
			}
			registered, found := registry[category]
			if !found {
				problems = append(problems, fmt.Sprintf("%s: label %q names no category of the registry", where, label))
				continue
			}
			if _, found := registered.Labels[value]; !found {
				problems = append(problems, fmt.Sprintf("%s: label %q is not in the registry; %s has %s", where, label, category, strings.Join(registered.LabelValues(), ", ")))
			}
		}
		for _, conflict := range declaration.SingleValueConflicts(registry, candidate.ApplyIf) {
			problems = append(problems, where+": applyIf "+conflict)
		}
		if candidate.Enforcement != "" && candidate.Enforcement != declaration.EnforcementRequired && candidate.Enforcement != declaration.EnforcementDefault {
			problems = append(problems, fmt.Sprintf("%s: enforcement %q is neither required nor default", where, candidate.Enforcement))
		}
		if len(candidate.Files) == 0 {
			problems = append(problems, where+": no files")
		}
	}
	return problems
}

// Included lists the candidates with include: true by level, the levels
// sorted, the candidates in the document's order.
func (proposal Proposal) Included() (levels []string, byLevel map[string][]Candidate) {
	byLevel = map[string][]Candidate{}
	for _, candidate := range proposal.Candidates {
		if !candidate.Include {
			continue
		}
		if _, seen := byLevel[candidate.Level]; !seen {
			levels = append(levels, candidate.Level)
		}
		byLevel[candidate.Level] = append(byLevel[candidate.Level], candidate)
	}
	sort.Strings(levels)
	return levels, byLevel
}

// Excluded lists the candidates with include: false.
func (proposal Proposal) Excluded() []Candidate {
	var excluded []Candidate
	for _, candidate := range proposal.Candidates {
		if !candidate.Include {
			excluded = append(excluded, candidate)
		}
	}
	return excluded
}

// FilesOf is a candidate's files at their paths in the declarations
// layout, rules/<id>.md or skills/<id>/SKILL.md, with the selectors written
// into them.
func FilesOf(candidate Candidate) (map[string]string, error) {
	files := map[string]string{}
	for key, content := range candidate.Files {
		switch candidate.Kind {
		case address.Rules:
			files[path.Join(address.Rules, candidate.ID+".md")] = content
		case address.Permissions, address.MCPServers:
			files[path.Join(candidate.Kind, candidate.ID+".yaml")] = content
		default:
			files[path.Join(candidate.Kind, candidate.ID, key)] = content
		}
	}
	return declaration.AnnotateSelectors(candidate.Kind, files, declaration.Selectors{ApplyIf: candidate.ApplyIf, DoNotApplyIf: candidate.DoNotApplyIf, Enforcement: candidate.Enforcement})
}

// MarkProposed records the branch, the pull request and the time.
func (proposal *Proposal) MarkProposed(branch string, pullRequest string, at time.Time) {
	proposal.Status = StatusProposed
	proposal.Branch = branch
	proposal.PullRequest = pullRequest
	proposal.ProposedAt = at
}

// orEmpty writes an empty list rather than nothing, so every field shows.
func orEmpty(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}
