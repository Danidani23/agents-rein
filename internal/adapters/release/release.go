// Package release reads rein's own releases from GitHub Releases, without a
// login: the repository is public.
package release

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// Repository is where rein is released.
const Repository = "Danidani23/agents-rein"

// Client talks to the GitHub API for the releases of Repository.
type Client struct {
	APIURL string
	HTTP   *http.Client
}

var _ ports.Releases = Client{}

// NewClient is a client of the GitHub API at apiURL, with a short time
// limit, since an update check runs beside other work.
func NewClient(apiURL string) Client {
	return Client{APIURL: strings.TrimRight(apiURL, "/"), HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Latest is the newest release of rein.
func (client Client) Latest() (ports.Release, error) {
	response, err := client.HTTP.Get(client.APIURL + "/repos/" + Repository + "/releases/latest")
	if err != nil {
		return ports.Release{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ports.Release{}, fmt.Errorf("GitHub answered %d for the latest release of %s", response.StatusCode, Repository)
	}
	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	err = json.NewDecoder(response.Body).Decode(&payload)
	if err != nil {
		return ports.Release{}, fmt.Errorf("the release listing is not JSON: %w", err)
	}
	release := ports.Release{Version: payload.TagName, Assets: map[string]string{}}
	for _, asset := range payload.Assets {
		release.Assets[asset.Name] = asset.URL
	}
	return release, nil
}

// Download fetches one asset.
func (client Client) Download(url string) ([]byte, error) {
	downloader := &http.Client{Timeout: 5 * time.Minute}
	response, err := downloader.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %d for %s", response.StatusCode, url)
	}
	return io.ReadAll(response.Body)
}
