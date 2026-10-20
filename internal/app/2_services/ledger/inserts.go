package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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

const insertsHeader = "# Written by rein: what sync placed into each project clone of this organisation, with the hash\n# of each file as written. Edit through rein commands; rein restore and rein reset read this.\n"

// InsertsFolder is where the inserts files live.
func (service Service) InsertsFolder() string {
	return filepath.Join(service.Home, "inserts")
}

// InsertsPath is the inserts file of an organisation.
func (service Service) InsertsPath(organizationID string) string {
	return filepath.Join(service.InsertsFolder(), organizationID+".yaml")
}

// LoadInserts reads an organisation's inserts; a missing file is empty.
func (service Service) LoadInserts(organizationID string) (Inserts, error) {
	inserts := Inserts{Version: 1, Clones: map[string][]Insert{}}
	content, err := os.ReadFile(service.InsertsPath(organizationID))
	if os.IsNotExist(err) {
		return inserts, nil
	}
	if err != nil {
		return Inserts{}, err
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

// DeleteInserts removes every inserts file: the end of a reset.
func (service Service) DeleteInserts() error {
	return os.RemoveAll(service.InsertsFolder())
}

func (service Service) saveInserts(organizationID string, inserts Inserts) error {
	content, err := yaml.Marshal(inserts)
	if err != nil {
		return err
	}
	err = os.MkdirAll(service.InsertsFolder(), 0o755)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(service.InsertsFolder(), ".inserts-*")
	if err != nil {
		return err
	}
	_, err = temporary.Write(append([]byte(insertsHeader), content...))
	if err != nil {
		temporary.Close()
		os.Remove(temporary.Name())
		return err
	}
	err = temporary.Close()
	if err != nil {
		os.Remove(temporary.Name())
		return err
	}
	return os.Rename(temporary.Name(), service.InsertsPath(organizationID))
}

// Hash is the SHA-256 of content as the inserts record it.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
