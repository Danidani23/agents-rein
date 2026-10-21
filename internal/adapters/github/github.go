// Package github is the Host port over GitHub's REST API, with a token the
// app "Agents Rein" obtained through the device flow (D25). GitHub is the
// host (D8); the app's client ID is public and compiled in; rein never
// deletes a repository (D26).
package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// ClientID identifies the GitHub App "Agents Rein", registered 2026-10-21.
const ClientID = "Iv23liA2ozfi2MzGuobI"

// AppSlug is the app's name in GitHub's addresses.
const AppSlug = "agents-rein"

// Client talks to GitHub. WebURL is github.com, APIURL api.github.com; a
// test or a GitHub Enterprise points them elsewhere.
type Client struct {
	ClientID string
	WebURL   string
	APIURL   string
	HTTP     *http.Client
}

var _ ports.Host = Client{}

// NewClient is the client for github.com.
func NewClient(clientID string, webURL string, apiURL string) Client {
	return Client{ClientID: clientID, WebURL: strings.TrimRight(webURL, "/"), APIURL: strings.TrimRight(apiURL, "/"), HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// Name is github.
func (Client) Name() string {
	return "github"
}

// InstallURL is where the app is installed on an account.
func (client Client) InstallURL() string {
	return client.WebURL + "/apps/" + AppSlug + "/installations/new"
}

// deviceResponse is GitHub's answer to the device code request.
type deviceResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// tokenResponse is GitHub's answer to a token request.
type tokenResponse struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

// StartDeviceFlow asks for a device code.
func (client Client) StartDeviceFlow() (ports.DeviceCode, error) {
	var response deviceResponse
	err := client.form(client.WebURL+"/login/device/code", url.Values{"client_id": {client.ClientID}}, &response)
	if err != nil {
		return ports.DeviceCode{}, err
	}
	interval := time.Duration(response.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}
	return ports.DeviceCode{DeviceCode: response.DeviceCode, UserCode: response.UserCode, VerificationURI: response.VerificationURI, Interval: interval, ExpiresAt: time.Now().Add(time.Duration(response.ExpiresIn) * time.Second)}, nil
}

// PollDeviceFlow asks once for the token.
func (client Client) PollDeviceFlow(code ports.DeviceCode) (ports.Token, bool, error) {
	var response tokenResponse
	err := client.form(client.WebURL+"/login/oauth/access_token", url.Values{"client_id": {client.ClientID}, "device_code": {code.DeviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &response)
	if err != nil {
		return ports.Token{}, false, err
	}
	switch response.Error {
	case "":
		return tokenOf(response), false, nil
	case "authorization_pending", "slow_down":
		return ports.Token{}, true, nil
	case "expired_token":
		return ports.Token{}, false, errors.New("the code expired before it was entered; run rein login again")
	case "access_denied":
		return ports.Token{}, false, errors.New("the authorisation was declined on GitHub")
	}
	return ports.Token{}, false, fmt.Errorf("GitHub answered %s: %s", response.Error, response.ErrorDescription)
}

// RefreshToken renews the token with its refresh token.
func (client Client) RefreshToken(token ports.Token) (ports.Token, error) {
	var response tokenResponse
	err := client.form(client.WebURL+"/login/oauth/access_token", url.Values{"client_id": {client.ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {token.RefreshToken}}, &response)
	if err != nil {
		return ports.Token{}, err
	}
	if response.Error != "" {
		return ports.Token{}, fmt.Errorf("GitHub would not renew the token for %s: %s; run rein login again", token.Login, response.ErrorDescription)
	}
	renewed := tokenOf(response)
	renewed.Login = token.Login
	return renewed, nil
}

func tokenOf(response tokenResponse) ports.Token {
	token := ports.Token{AccessToken: response.AccessToken, RefreshToken: response.RefreshToken}
	if response.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(response.ExpiresIn) * time.Second)
	}
	if response.RefreshTokenExpiresIn > 0 {
		token.RefreshExpiresAt = time.Now().Add(time.Duration(response.RefreshTokenExpiresIn) * time.Second)
	}
	return token
}

// Identity is the token's user.
func (client Client) Identity(accessToken string) (ports.Identity, error) {
	var user struct {
		Login string `json:"login"`
	}
	err := client.api(accessToken, http.MethodGet, "/user", nil, &user)
	if err != nil {
		return ports.Identity{}, err
	}
	if user.Login == "" {
		return ports.Identity{}, errors.New("GitHub returned no account for the token; run rein login again")
	}
	return ports.Identity{Login: user.Login, Slug: SlugOf(user.Login)}, nil
}

// Owners is the login and its organisations.
func (client Client) Owners(accessToken string) ([]string, error) {
	identity, err := client.Identity(accessToken)
	if err != nil {
		return nil, err
	}
	var organizations []struct {
		Login string `json:"login"`
	}
	err = client.api(accessToken, http.MethodGet, "/user/orgs?per_page=100", nil, &organizations)
	if err != nil {
		return nil, err
	}
	owners := []string{identity.Login}
	for _, organization := range organizations {
		owners = append(owners, organization.Login)
	}
	return owners, nil
}

// Repository says whether owner/name exists and whether it holds a commit.
func (client Client) Repository(accessToken string, owner string, name string) (ports.RepositoryInfo, error) {
	var repository struct {
		CloneURL string `json:"clone_url"`
	}
	err := client.api(accessToken, http.MethodGet, "/repos/"+owner+"/"+name, nil, &repository)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return ports.RepositoryInfo{}, nil
	}
	if err != nil {
		return ports.RepositoryInfo{}, err
	}
	var commits []any
	err = client.api(accessToken, http.MethodGet, "/repos/"+owner+"/"+name+"/commits?per_page=1", nil, &commits)
	if errors.As(err, &status) && status.code == http.StatusConflict {
		// GitHub answers 409 for a repository without commits
		return ports.RepositoryInfo{Exists: true, Empty: true, CloneURL: repository.CloneURL}, nil
	}
	if err != nil {
		return ports.RepositoryInfo{}, err
	}
	return ports.RepositoryInfo{Exists: true, Empty: len(commits) == 0, CloneURL: repository.CloneURL}, nil
}

// CreateRepository creates owner/name under the user or an organisation.
func (client Client) CreateRepository(accessToken string, repository ports.NewRepository) (string, error) {
	identity, err := client.Identity(accessToken)
	if err != nil {
		return "", err
	}
	path := "/orgs/" + repository.Owner + "/repos"
	if strings.EqualFold(repository.Owner, identity.Login) {
		path = "/user/repos"
	}
	body := map[string]any{"name": repository.Name, "description": repository.Description, "private": repository.Private, "auto_init": false}
	var created struct {
		CloneURL string `json:"clone_url"`
	}
	err = client.api(accessToken, http.MethodPost, path, body, &created)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusForbidden {
		return "", fmt.Errorf("GitHub refused to create %s/%s for the app: install Agents Rein on %s at %s, with access to all repositories, then try again (GitHub said: %s)", repository.Owner, repository.Name, repository.Owner, client.InstallURL(), status.message)
	}
	if err != nil {
		return "", err
	}
	return created.CloneURL, nil
}

// ProtectMainBranch puts the protection on main; a 403 is GitHub refusing
// the feature for the plan, private repositories on a free account, and
// comes back as the reason.
func (client Client) ProtectMainBranch(accessToken string, owner string, name string) (string, error) {
	body := map[string]any{
		"required_status_checks":        nil,
		"enforce_admins":                false,
		"required_pull_request_reviews": map[string]any{"require_code_owner_reviews": true, "required_approving_review_count": 1},
		"restrictions":                  nil,
	}
	err := client.api(accessToken, http.MethodPut, "/repos/"+owner+"/"+name+"/branches/main/protection", body, nil)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusForbidden {
		return status.message, nil
	}
	return "", err
}

// CreatePullRequest opens the pull request against main.
func (client Client) CreatePullRequest(accessToken string, owner string, name string, request ports.PullRequest) (string, error) {
	var created struct {
		URL string `json:"html_url"`
	}
	err := client.api(accessToken, http.MethodPost, "/repos/"+owner+"/"+name+"/pulls", map[string]any{"title": request.Title, "head": request.Branch, "base": "main", "body": request.Body}, &created)
	if err != nil {
		return "", err
	}
	return created.URL, nil
}

// statusError is an API answer other than success.
type statusError struct {
	code    int
	message string
}

func (err *statusError) Error() string {
	return fmt.Sprintf("GitHub answered %d: %s", err.code, err.message)
}

// api calls the REST API with the token.
func (client Client) api(accessToken string, method string, path string, body any, into any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, client.APIURL+path, payload)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return client.do(request, into)
}

// form posts a form, as the device flow wants, and reads JSON back.
func (client Client) form(address string, values url.Values, into any) error {
	request, err := http.NewRequest(http.MethodPost, address, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return client.do(request, into)
}

func (client Client) do(request *http.Request, into any) error {
	response, err := client.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("GitHub could not be reached: %w", err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode >= 300 {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(content, &failure)
		if failure.Message == "" {
			failure.Message = strings.TrimSpace(string(content))
		}
		return &statusError{code: response.StatusCode, message: failure.Message}
	}
	if into == nil || len(content) == 0 {
		return nil
	}
	return json.Unmarshal(content, into)
}

// SlugOf derives the address segment from a login: lowercase, because macOS
// and Windows file systems are case-insensitive and GitHub logins are unique
// regardless of case, so no two accounts can share a slug.
func SlugOf(login string) string {
	return strings.ToLower(login)
}

// RepositoryOf splits a github.com clone URL into owner and name.
func RepositoryOf(cloneURL string) (owner string, name string, ok bool) {
	trimmed := strings.TrimSuffix(cloneURL, ".git")
	for _, prefix := range []string{"https://github.com/", "git@github.com:", "ssh://git@github.com/"} {
		if strings.HasPrefix(trimmed, prefix) {
			parts := strings.Split(strings.TrimPrefix(trimmed, prefix), "/")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1], true
			}
		}
	}
	return "", "", false
}
