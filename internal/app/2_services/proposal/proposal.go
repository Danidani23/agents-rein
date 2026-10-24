// Package proposal is the format of a proposal: a folder whose
// proposal.yaml says where every candidate goes and why, beside one entry
// per candidate with its content (P2.44). distill writes it, a person edits
// it, propose reads it and does what it says, and afterwards it is kept as
// history. A proposal written before folders, one YAML document, still reads.
package proposal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
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

// Version is the format version this build writes: a folder.
const Version = 2

// FileVersion is the format of a proposal written before folders, one YAML
// file holding the content too, which this build still reads.
const FileVersion = 1

// IndexFile holds everything of a proposal folder but the content.
const IndexFile = "proposal.yaml"

// Status is where a proposal stands.
type Status string

// The statuses.
const (
	StatusPending  Status = "pending"
	StatusProposed Status = "proposed"
)

// Header is the comment at the top of every proposal.yaml.
const Header = `# Written by rein distill. Review and edit it, then rein propose. A candidate with
# include: false stays out; level, applyIf, doNotApplyIf and enforcement are yours to
# change here, its content in the entry beside this file that it names. Never edit a
# proposed one; distill again instead.
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
	Kind         string     `yaml:"kind"`
	ID           string     `yaml:"id"`
	Include      bool       `yaml:"include"`
	Level        string     `yaml:"level"`
	ApplyIf      []string   `yaml:"applyIf"`
	DoNotApplyIf []string   `yaml:"doNotApplyIf"`
	Enforcement  string     `yaml:"enforcement"`
	Sources      []string   `yaml:"sources"`
	Compared     Comparison `yaml:"compared"`
	Judge        *Judgement `yaml:"judge"`
	Notes        []string   `yaml:"notes"`
	// Entry names the file or folder beside proposal.yaml that holds the
	// content; a proposal written before folders has Files inline instead.
	Entry string            `yaml:"entry,omitempty"`
	Files map[string]string `yaml:"files,omitempty"`
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

// FolderName is a pending proposal's folder name.
func FolderName(id string) string {
	return "proposal-" + id
}

// FileName is the name a pending proposal had before folders.
func FileName(id string) string {
	return FolderName(id) + ".yaml"
}

// HistoryName is a proposed proposal's folder name, the time first so the
// history lists in order.
func HistoryName(id string, proposedAt time.Time) string {
	return proposedAt.UTC().Format("20060102-150405") + "-" + id
}

// entryName is where a candidate's content goes in a proposal folder:
// <kind>_<id>, a file for a declaration of one file, a folder otherwise.
func entryName(candidate Candidate) string {
	name := strings.TrimSuffix(candidate.Kind, "s") + "_" + candidate.ID
	if len(candidate.Files) != 1 {
		return name
	}
	switch candidate.Kind {
	case address.Rules:
		return name + ".md"
	case address.Permissions, address.MCPServers:
		return name + ".yaml"
	}
	return name
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

// Load reads a proposal: a folder, or a file written before folders.
func Load(proposalPath string) (Proposal, error) {
	info, err := os.Stat(proposalPath)
	if err != nil {
		return Proposal{}, err
	}
	if info.IsDir() {
		return readFolder(proposalPath)
	}
	return readFile(proposalPath)
}

// readFile parses a proposal written before folders.
func readFile(filePath string) (Proposal, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return Proposal{}, err
	}
	var document Proposal
	err = yaml.Unmarshal(content, &document)
	if err != nil {
		return Proposal{}, fmt.Errorf("%s is not a proposal: %w", filePath, err)
	}
	if document.Version != FileVersion {
		return Proposal{}, fmt.Errorf("%s is a proposal of version %d; this rein reads a file of version %d", filePath, document.Version, FileVersion)
	}
	return document, nil
}

// readFolder parses proposal.yaml and reads each candidate's entry.
func readFolder(folder string) (Proposal, error) {
	indexPath := filepath.Join(folder, IndexFile)
	content, err := os.ReadFile(indexPath)
	if err != nil {
		return Proposal{}, err
	}
	var document Proposal
	err = yaml.Unmarshal(content, &document)
	if err != nil {
		return Proposal{}, fmt.Errorf("%s is not a proposal: %w", indexPath, err)
	}
	if document.Version != Version {
		return Proposal{}, fmt.Errorf("%s is a proposal of version %d; this rein reads version %d", indexPath, document.Version, Version)
	}
	for index := range document.Candidates {
		candidate := &document.Candidates[index]
		files, err := readEntry(folder, *candidate)
		if err != nil {
			return Proposal{}, err
		}
		candidate.Files = files
	}
	return document, nil
}

// readEntry reads the file or the folder a candidate's entry names, which
// must sit right beside proposal.yaml.
func readEntry(folder string, candidate Candidate) (map[string]string, error) {
	if candidate.Entry == "" || candidate.Entry != filepath.Base(candidate.Entry) || candidate.Entry == ".." || candidate.Entry == IndexFile {
		return nil, fmt.Errorf("%s/%s: the entry %q is not a name beside %s", candidate.Kind, candidate.ID, candidate.Entry, IndexFile)
	}
	target := filepath.Join(folder, candidate.Entry)
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("%s/%s: the entry %s is missing: %w", candidate.Kind, candidate.ID, candidate.Entry, err)
	}
	files := map[string]string{}
	if !info.IsDir() {
		content, err := os.ReadFile(target)
		if err != nil {
			return nil, err
		}
		files[strings.TrimPrefix(candidate.Entry, strings.TrimSuffix(candidate.Kind, "s")+"_")] = string(content)
		return files, nil
	}
	err = filepath.WalkDir(target, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(target, current)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(content)
		return nil
	})
	return files, err
}

// WriteFolder writes the document as a folder: proposal.yaml with every
// field but the content, and each candidate's files in its entry beside
// it. The folder is built beside its place and renamed into it, so a
// reader never sees half of it.
func (proposal Proposal) WriteFolder(folder string) error {
	building := folder + ".writing"
	err := os.RemoveAll(building)
	if err != nil {
		return err
	}
	err = os.MkdirAll(building, 0o700)
	if err != nil {
		return err
	}
	index := proposal
	index.Candidates = make([]Candidate, len(proposal.Candidates))
	taken := map[string]bool{}
	for position, candidate := range proposal.Candidates {
		entry := entryName(candidate)
		extension := path.Ext(entry)
		for count := 2; taken[entry]; count++ {
			entry = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(entryName(candidate), extension), count, extension)
		}
		taken[entry] = true
		for key, content := range candidate.Files {
			target := filepath.Join(building, entry)
			if extension == "" {
				target = filepath.Join(building, entry, filepath.FromSlash(key))
			}
			err = os.MkdirAll(filepath.Dir(target), 0o700)
			if err != nil {
				return err
			}
			err = os.WriteFile(target, []byte(content), 0o600)
			if err != nil {
				return err
			}
		}
		candidate.Entry = entry
		candidate.Files = nil
		index.Candidates[position] = candidate
	}
	encoded, err := yaml.Marshal(index)
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(building, IndexFile), append([]byte(Header), encoded...), 0o600)
	if err != nil {
		return err
	}
	err = os.RemoveAll(folder)
	if err != nil {
		return err
	}
	return os.Rename(building, folder)
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
			problems = append(problems, fmt.Sprintf("%s: level %q is not one; organizations.<org>, organizations.<org>.teams.<team>, organizations.<org>.projects.<project> or organizations.<org>.users.<user>", where, candidate.Level))
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
