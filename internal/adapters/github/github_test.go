package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// NewRepositoryFor is a private repository request.
func NewRepositoryFor(owner string, name string) ports.NewRepository {
	return ports.NewRepository{Owner: owner, Name: name, Description: "x", Private: true}
}

func pullRequestFor(branch string) ports.PullRequest {
	return ports.PullRequest{Branch: branch, Title: "t", Body: "b"}
}

// server is a GitHub that answers the calls rein makes.
func server(t *testing.T) *httptest.Server {
	t.Helper()
	polls := 0
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/login/device/code":
			_ = json.NewEncoder(writer).Encode(map[string]any{"device_code": "d", "user_code": "ABCD-1234", "verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 1})
		case request.URL.Path == "/login/oauth/access_token":
			_ = request.ParseForm()
			if request.Form.Get("grant_type") == "refresh_token" {
				_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": "renewed", "refresh_token": "r2", "expires_in": 28800})
				return
			}
			polls++
			if polls == 1 {
				_ = json.NewEncoder(writer).Encode(map[string]any{"error": "authorization_pending"})
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": "ghu_token", "refresh_token": "r1", "expires_in": 28800, "refresh_token_expires_in": 15552000})
		case request.URL.Path == "/user":
			if request.Header.Get("Authorization") != "Bearer ghu_token" {
				writer.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(writer).Encode(map[string]any{"message": "Bad credentials"})
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"login": "Danidani23"})
		case request.URL.Path == "/user/orgs":
			_ = json.NewEncoder(writer).Encode([]map[string]any{{"login": "acme-inc"}})
		case request.URL.Path == "/repos/Danidani23/missing":
			writer.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(writer).Encode(map[string]any{"message": "Not Found"})
		case request.URL.Path == "/repos/Danidani23/empty":
			_ = json.NewEncoder(writer).Encode(map[string]any{"clone_url": "https://github.com/Danidani23/empty.git"})
		case request.URL.Path == "/repos/Danidani23/empty/commits":
			writer.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(writer).Encode(map[string]any{"message": "Git Repository is empty."})
		case request.URL.Path == "/repos/Danidani23/full":
			_ = json.NewEncoder(writer).Encode(map[string]any{"clone_url": "https://github.com/Danidani23/full.git"})
		case request.URL.Path == "/repos/Danidani23/full/commits":
			_ = json.NewEncoder(writer).Encode([]map[string]any{{"sha": "abc"}})
		case request.URL.Path == "/user/repos" && request.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body["name"] == "forbidden" {
				writer.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(writer).Encode(map[string]any{"message": "Resource not accessible by integration"})
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"clone_url": "https://github.com/Danidani23/" + body["name"].(string) + ".git"})
		case strings.HasSuffix(request.URL.Path, "/branches/main/protection"):
			if strings.Contains(request.URL.Path, "/private/") {
				writer.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(writer).Encode(map[string]any{"message": "Upgrade to GitHub Pro or make this repository public to enable this feature."})
				return
			}
			writer.WriteHeader(http.StatusOK)
		case strings.HasSuffix(request.URL.Path, "/pulls"):
			_ = json.NewEncoder(writer).Encode(map[string]any{"html_url": "https://github.com/Danidani23/x/pull/7"})
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestClient(t *testing.T) {
	fakeGitHub := server(t)
	defer fakeGitHub.Close()
	client := NewClient("client-id", fakeGitHub.URL, fakeGitHub.URL)
	code, err := client.StartDeviceFlow()
	if err != nil || code.UserCode != "ABCD-1234" || code.Interval.Seconds() != 1 {
		t.Fatalf("code %+v err %v", code, err)
	}
	_, pending, err := client.PollDeviceFlow(code)
	if err != nil || !pending {
		t.Fatalf("first poll pending %v err %v", pending, err)
	}
	token, pending, err := client.PollDeviceFlow(code)
	if err != nil || pending || token.AccessToken != "ghu_token" || token.ExpiresAt.IsZero() || token.RefreshExpiresAt.IsZero() {
		t.Fatalf("token %+v pending %v err %v", token, pending, err)
	}
	renewed, err := client.RefreshToken(token)
	if err != nil || renewed.AccessToken != "renewed" {
		t.Fatalf("renewed %+v err %v", renewed, err)
	}
	identity, err := client.Identity("ghu_token")
	if err != nil || identity.Login != "Danidani23" || identity.Slug != "danidani23" {
		t.Fatalf("identity %+v err %v", identity, err)
	}
	_, err = client.Identity("wrong")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err %v", err)
	}
	owners, err := client.Owners("ghu_token")
	if err != nil || strings.Join(owners, ",") != "Danidani23,acme-inc" {
		t.Fatalf("owners %v err %v", owners, err)
	}
	for name, expected := range map[string][2]bool{"missing": {false, false}, "empty": {true, true}, "full": {true, false}} {
		info, err := client.Repository("ghu_token", "Danidani23", name)
		if err != nil || info.Exists != expected[0] || info.Empty != expected[1] {
			t.Fatalf("%s: %+v err %v", name, info, err)
		}
	}
	cloneURL, err := client.CreateRepository("ghu_token", NewRepositoryFor("Danidani23", "shop"))
	if err != nil || cloneURL != "https://github.com/Danidani23/shop.git" {
		t.Fatalf("clone %q err %v", cloneURL, err)
	}
	_, err = client.CreateRepository("ghu_token", NewRepositoryFor("Danidani23", "forbidden"))
	if err == nil || !strings.Contains(err.Error(), "install Agents Rein on Danidani23 at "+fakeGitHub.URL+"/apps/agents-rein/installations/new") {
		t.Fatalf("err %v", err)
	}
	reason, err := client.ProtectMainBranch("ghu_token", "Danidani23", "private")
	if err != nil || !strings.Contains(reason, "GitHub Pro") {
		t.Fatalf("reason %q err %v", reason, err)
	}
	reason, err = client.ProtectMainBranch("ghu_token", "Danidani23", "shop")
	if err != nil || reason != "" {
		t.Fatalf("reason %q err %v", reason, err)
	}
	url, err := client.CreatePullRequest("ghu_token", "Danidani23", "x", pullRequestFor("rein/propose-x"))
	if err != nil || url != "https://github.com/Danidani23/x/pull/7" {
		t.Fatalf("url %q err %v", url, err)
	}
}

func TestRepositoryOf(t *testing.T) {
	for cloneURL, expected := range map[string]string{"https://github.com/Danidani23/shop.git": "Danidani23/shop", "git@github.com:Danidani23/shop.git": "Danidani23/shop", "ssh://git@github.com/Danidani23/shop": "Danidani23/shop", "/a/local/path": ""} {
		owner, name, ok := RepositoryOf(cloneURL)
		if (expected == "") == ok || (ok && owner+"/"+name != expected) {
			t.Fatalf("%s: %s/%s %v", cloneURL, owner, name, ok)
		}
	}
}
