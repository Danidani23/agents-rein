package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// Insert is one file sync wrote into a project clone, with the hash of what
// it wrote, so a hand edit is seen before anything overwrites or removes it.
type Insert struct {
	Path      string    `yaml:"path"`
	Address   string    `yaml:"address"`
	SHA256    string    `yaml:"sha256"`
	WrittenAt time.Time `yaml:"written_at"`
}

// Inserts is the file of one organisation: the inserts per project clone.
type Inserts struct {
	Version int                 `yaml:"version"`
	Clones  map[string][]Insert `yaml:"clones"`
}

const insertsHeader = "# Written by rein: what sync placed into each project clone of this organisation, with the hash\n# of each file as written. Edit through rein commands; rein reset project and rein reset machine read this.\n"

// insertsPrefix starts the name of every organisation's inserts document.
const insertsPrefix = "inserts/"

// insertsDocument is the inserts document of an organisation.
func insertsDocument(organizationID string) string {
	return insertsPrefix + organizationID + ".yaml"
}

// LoadInserts reads an organisation's inserts; missing ones are empty.
func (service Service) LoadInserts(organizationID string) (Inserts, error) {
	inserts := Inserts{Version: 1, Clones: map[string][]Insert{}}
	content, found, err := service.Documents.Read(insertsDocument(organizationID))
	if err != nil {
		return Inserts{}, err
	}
	if !found {
		return inserts, nil
	}
	err = yaml.Unmarshal(content, &inserts)
	if err != nil {
		return Inserts{}, err
	}
	if inserts.Clones == nil {
		inserts.Clones = map[string][]Insert{}
	}
	return inserts, nil
}

// InsertsOf lists what sync placed into one clone.
func (service Service) InsertsOf(organizationID string, clonePath string) ([]Insert, error) {
	inserts, err := service.LoadInserts(organizationID)
	if err != nil {
		return nil, err
	}
	return inserts.Clones[clonePath], nil
}

// RecordInserts replaces the inserts of one clone, each stamped now.
func (service Service) RecordInserts(organizationID string, clonePath string, placed []Insert) error {
	inserts, err := service.LoadInserts(organizationID)
	if err != nil {
		return err
	}
	writtenAt := service.now()
	for index := range placed {
		placed[index].WrittenAt = writtenAt
	}
	sort.Slice(placed, func(left, right int) bool { return placed[left].Path < placed[right].Path })
	if len(placed) == 0 {
		delete(inserts.Clones, clonePath)
	} else {
		inserts.Clones[clonePath] = placed
	}
	return service.saveInserts(organizationID, inserts)
}

// ForgetInserts drops a clone's inserts: after restore removed them.
func (service Service) ForgetInserts(organizationID string, clonePath string) error {
	return service.RecordInserts(organizationID, clonePath, nil)
}

func (service Service) saveInserts(organizationID string, inserts Inserts) error {
	content, err := yaml.Marshal(inserts)
	if err != nil {
		return err
	}
	return service.Documents.Write(insertsDocument(organizationID), append([]byte(insertsHeader), content...))
}

// Hash is the SHA-256 of content as the inserts record it.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
