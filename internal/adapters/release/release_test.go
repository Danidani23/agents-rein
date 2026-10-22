package release

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatestAndDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/Danidani23/agents-rein/releases/latest":
			_, _ = writer.Write([]byte(`{"tag_name":"v0.2.0","assets":[{"name":"rein_0.2.0_darwin_arm64.tar.gz","browser_download_url":"` + "http://" + request.Host + `/download/archive"}]}`))
		case "/download/archive":
			_, _ = writer.Write([]byte("archive bytes"))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL)
	release, err := client.Latest()
	if err != nil || release.Version != "v0.2.0" || release.Assets["rein_0.2.0_darwin_arm64.tar.gz"] == "" {
		t.Fatalf("release %+v err %v", release, err)
	}
	content, err := client.Download(release.Assets["rein_0.2.0_darwin_arm64.tar.gz"])
	if err != nil || string(content) != "archive bytes" {
		t.Fatalf("download %q err %v", content, err)
	}
	_, err = client.Download(server.URL + "/missing")
	if err == nil {
		t.Fatalf("a missing asset downloads")
	}
	_, err = NewClient("http://127.0.0.1:1").Latest()
	if err == nil {
		t.Fatalf("an unreachable API answers")
	}
}
