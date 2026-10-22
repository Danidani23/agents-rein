package distill

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
)

// Entry is one declaration an organisation already has.
type Entry struct {
	Address  string
	Level    string
	Kind     string
	ID       string
	Identity string
}

// Catalogue is every declaration of an org repository, for the comparison.
type Catalogue struct {
	Entries      []Entry
	organization string
}

// LoadCatalogue reads the rules, skills, tool servers, permission sets and
// hooks of an org repository clone at every level.
func LoadCatalogue(orgClonePath string) (Catalogue, error) {
	root := filepath.Join(orgClonePath, "declarations")
	_, statErr := os.Stat(root)
	if os.IsNotExist(statErr) {
		return Catalogue{}, nil
	}
	organization, err := declaration.FindOrganization(orgClonePath)
	if err != nil {
		return Catalogue{}, err
	}
	catalogue := Catalogue{organization: organization.Slug}
	folders := map[string]map[string]string{}
	folderKinds := map[string]string{}
	err = filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
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
		relative = filepath.ToSlash(relative)
		segments := strings.Split(relative, "/")
		if len(segments) < 2 {
			return nil
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		collection := segments[len(segments)-2]
		base := segments[len(segments)-1]
		// the level is the path above the collection folder
		levelPath := path.Join(segments[:len(segments)-2]...)
		switch {
		case collection == address.Rules && strings.HasSuffix(base, ".md"):
			catalogue.add(levelPath, address.Rules, strings.TrimSuffix(base, ".md"), map[string]string{base: string(content)})
		case collection == address.MCPServers && strings.HasSuffix(base, ".yaml"):
			catalogue.add(levelPath, address.MCPServers, strings.TrimSuffix(base, ".yaml"), map[string]string{base: string(content)})
		case collection == address.Permissions && strings.HasSuffix(base, ".yaml"):
			catalogue.add(levelPath, address.Permissions, strings.TrimSuffix(base, ".yaml"), map[string]string{base: string(content)})
		default:
			// a file inside a skill or hook folder: gather the folder
			for index := len(segments) - 2; index >= 1; index-- {
				if segments[index-1] == address.Skills || segments[index-1] == address.Hooks {
					folder := path.Join(segments[:index+1]...)
					if folders[folder] == nil {
						folders[folder] = map[string]string{}
						folderKinds[folder] = segments[index-1]
					}
					folders[folder][path.Join(segments[index+1:]...)] = string(content)
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return Catalogue{}, err
	}
	var folderPaths []string
	for folder := range folders {
		folderPaths = append(folderPaths, folder)
	}
	sort.Strings(folderPaths)
	for _, folder := range folderPaths {
		catalogue.add(path.Dir(path.Dir(folder)), folderKinds[folder], path.Base(folder), folders[folder])
	}
	sort.Slice(catalogue.Entries, func(left, right int) bool { return catalogue.Entries[left].Address < catalogue.Entries[right].Address })
	return catalogue, nil
}

// add records one declaration; levelPath is its level's folder under
// declarations/, such as teams/web, or "" for the organisation.
func (catalogue *Catalogue) add(levelPath string, kind string, id string, files map[string]string) {
	level, err := address.FromPath(levelPath, catalogue.organization)
	if err != nil {
		return
	}
	catalogue.Entries = append(catalogue.Entries, Entry{Address: level.String() + "." + kind + "." + id, Level: level.String(), Kind: kind, ID: id, Identity: Identity(kind, files)})
}

// Compare gives every candidate its verdict against the catalogue: identical
// content anywhere, else the same id at another level, else new. The
// candidate's own level is skipped for the same-id check, since propose
// replaces a declaration there.
func Compare(candidates []Candidate, catalogue Catalogue, level string) {
	for index := range candidates {
		candidate := &candidates[index]
		identity := Identity(candidate.Kind, candidate.Files)
		candidate.Verdict = VerdictNew
		candidate.Related = nil
		for _, entry := range catalogue.Entries {
			if entry.Kind == candidate.Kind && entry.Identity == identity {
				candidate.Verdict = VerdictIdentical
				candidate.Related = append(candidate.Related, entry.Address)
			}
		}
		if candidate.Verdict != VerdictNew {
			continue
		}
		for _, entry := range catalogue.Entries {
			if entry.Kind == candidate.Kind && entry.ID == candidate.ID && entry.Level != level {
				candidate.Verdict = VerdictSameID
				candidate.Related = append(candidate.Related, entry.Address)
			}
		}
	}
}
