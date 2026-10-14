package declaration

import (
	"encoding/json"
	"fmt"
)

// Settings is the part of a Claude Code settings file the lint reads.
type Settings struct {
	Path        string      `json:"-"`
	Permissions Permissions `json:"permissions"`
}

// Permissions holds the rule lists of a settings file.
type Permissions struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// ParseSettings reads a settings.json or settings.local.json.
func ParseSettings(path string, content []byte) (settings Settings, problems []Problem) {
	settings = Settings{Path: path}
	err := json.Unmarshal(content, &settings)
	if err != nil {
		return settings, []Problem{{Severity: SeverityError, Message: fmt.Sprintf("settings file is not valid JSON: %v", err)}}
	}
	return settings, nil
}
