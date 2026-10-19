package distill

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
)

// Passage is one existing declaration as the judge reads it: its address,
// its selectors and its text, and whether it applies to the level the
// candidates go to or is another team's that merely looks related.
type Passage struct {
	Address     string
	Level       string
	Kind        string
	Enforcement string
	ApplyIf     []string
	Text        string
	Resolved    bool
}

// Corpus is what the judge compares the candidates with.
type Corpus struct {
	Passages []Passage
	// Dropped names the passages left out to stay within the budget.
	Dropped []string
}

// corpusBudget is the most characters of passages one brief carries.
const corpusBudget = 120000

// LoadCorpus reads every declaration of the org clone with its text and
// marks the resolved set for a level: the organisation, the level's teams,
// the level itself and the user. Other teams' declarations stay in the
// corpus only when they look related to a candidate, by a shared label or
// by shared words, and the whole stays within the budget.
func LoadCorpus(orgClonePath string, level string, teams []string, userSlug string, candidates []Candidate) (Corpus, error) {
	organizationLevel := ""
	segments := strings.Split(level, ".")
	if segments[0] == address.Organizations && len(segments) >= 2 {
		organizationLevel = segments[0] + "." + segments[1]
	}
	resolved := map[string]bool{level: true}
	if organizationLevel != "" {
		resolved[organizationLevel] = true
		for _, team := range teams {
			resolved[organizationLevel+".teams."+team] = true
		}
	}
	if userSlug != "" {
		resolved["users."+userSlug] = true
	}
	passages, err := readPassages(orgClonePath)
	if err != nil {
		return Corpus{}, err
	}
	candidateWords := map[string]bool{}
	candidateLabels := map[string]bool{}
	for _, candidate := range candidates {
		for _, content := range candidate.Files {
			for word := range words(content) {
				candidateWords[word] = true
			}
			for _, label := range selectorsOf(candidate.Kind, content).ApplyIf {
				candidateLabels[label] = true
			}
		}
	}
	var corpus Corpus
	for _, passage := range passages {
		passage.Resolved = resolved[passage.Level]
		if passage.Resolved || related(passage, candidateWords, candidateLabels) {
			corpus.Passages = append(corpus.Passages, passage)
		}
	}
	corpus.trim()
	return corpus, nil
}

// readPassages walks the declarations of a clone.
func readPassages(orgClonePath string) ([]Passage, error) {
	catalogueFiles := map[string]map[string]string{}
	catalogueLevels := map[string]string{}
	catalogueKinds := map[string]string{}
	root := filepath.Join(orgClonePath, "declarations")
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && filePath == root {
				return filepath.SkipDir
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		segments := strings.Split(filepath.ToSlash(relative), "/")
		for index := len(segments) - 2; index >= 1; index-- {
			collection := segments[index]
			if collection != address.Rules && collection != address.Skills && collection != address.MCPServers && collection != address.Permissions && collection != address.Hooks {
				continue
			}
			var key, levelPath, inside string
			if collection == address.Skills || collection == address.Hooks {
				if index+1 >= len(segments)-1 {
					continue
				}
				key = path.Join(segments[:index+2]...)
				inside = path.Join(segments[index+2:]...)
			} else {
				if index != len(segments)-2 {
					continue
				}
				key = path.Join(segments...)
				inside = segments[len(segments)-1]
			}
			levelPath = path.Join(segments[:index]...)
			content, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			if catalogueFiles[key] == nil {
				catalogueFiles[key] = map[string]string{}
				catalogueLevels[key] = levelPath
				catalogueKinds[key] = collection
			}
			catalogueFiles[key][inside] = string(content)
			break
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var keys []string
	for key := range catalogueFiles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var passages []Passage
	for _, key := range keys {
		level, err := address.FromPath(catalogueLevels[key])
		if err != nil {
			continue
		}
		kind := catalogueKinds[key]
		id := strings.TrimSuffix(strings.TrimSuffix(path.Base(key), ".md"), ".yaml")
		passage := Passage{Address: level.String() + "." + kind + "." + id, Level: level.String(), Kind: kind}
		var parts []string
		var inside []string
		for name := range catalogueFiles[key] {
			inside = append(inside, name)
		}
		sort.Strings(inside)
		for _, name := range inside {
			content := catalogueFiles[key][name]
			selectors := selectorsOf(kind, content)
			if selectors.Enforcement != "" || len(selectors.ApplyIf) > 0 {
				passage.Enforcement = selectors.Enforcement
				passage.ApplyIf = selectors.ApplyIf
			}
			if len(inside) > 1 {
				parts = append(parts, "--- "+name+" ---\n"+content)
			} else {
				parts = append(parts, content)
			}
		}
		passage.Text = strings.TrimSpace(strings.Join(parts, "\n"))
		passages = append(passages, passage)
	}
	return passages, nil
}

// selectorsOf reads the selector fields of a YAML declaration; a Markdown
// file has none the judge needs.
func selectorsOf(kind string, content string) declaration.Selectors {
	if kind == address.Rules || kind == address.Skills {
		return declaration.Selectors{}
	}
	var selectors declaration.Selectors
	_ = yaml.Unmarshal([]byte(content), &selectors)
	return selectors
}

var wordPattern = regexp.MustCompile(`[a-z]{4,}`)

func words(text string) map[string]bool {
	found := map[string]bool{}
	for _, word := range wordPattern.FindAllString(strings.ToLower(text), -1) {
		found[word] = true
	}
	return found
}

// related is the prefilter for another team's declaration: a label the
// candidates carry, or a tenth of its words in common with them.
func related(passage Passage, candidateWords map[string]bool, candidateLabels map[string]bool) bool {
	for _, label := range passage.ApplyIf {
		if candidateLabels[label] {
			return true
		}
	}
	passageWords := words(passage.Text)
	if len(passageWords) == 0 || len(candidateWords) == 0 {
		return false
	}
	shared := 0
	for word := range passageWords {
		if candidateWords[word] {
			shared++
		}
	}
	union := len(passageWords) + len(candidateWords) - shared
	return float64(shared)/float64(union) >= 0.1
}

// trim drops the related passages first, then the largest resolved ones,
// until the corpus fits the budget.
func (corpus *Corpus) trim() {
	size := func() int {
		total := 0
		for _, passage := range corpus.Passages {
			total += len(passage.Text)
		}
		return total
	}
	for size() > corpusBudget {
		victim := -1
		for index, passage := range corpus.Passages {
			if !passage.Resolved && (victim < 0 || len(passage.Text) > len(corpus.Passages[victim].Text)) {
				victim = index
			}
		}
		if victim < 0 {
			for index, passage := range corpus.Passages {
				if victim < 0 || len(passage.Text) > len(corpus.Passages[victim].Text) {
					victim = index
				}
			}
		}
		corpus.Dropped = append(corpus.Dropped, corpus.Passages[victim].Address)
		corpus.Passages = append(corpus.Passages[:victim], corpus.Passages[victim+1:]...)
	}
}
