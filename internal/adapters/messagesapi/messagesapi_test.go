package messagesapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// received is what the test server was sent.
type received struct {
	Path    string
	Header  http.Header
	Request struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Stream    bool   `json:"stream"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
}

// serve answers every request with status and the stream's events.
func serve(t *testing.T, status int, events ...string) (*httptest.Server, *received) {
	t.Helper()
	seen := &received{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen.Path, seen.Header = request.URL.Path, request.Header.Clone()
		_ = json.NewDecoder(request.Body).Decode(&seen.Request)
		writer.Header().Set("content-type", "text/event-stream")
		writer.WriteHeader(status)
		for _, event := range events {
			fmt.Fprint(writer, event)
		}
	}))
	t.Cleanup(server.Close)
	return server, seen
}

// event is one server-sent event as the Messages API writes it.
func event(name string, data string) string {
	return "event: " + name + "\ndata: " + data + "\n\n"
}

func lookup(name string) (string, bool) {
	return "secret", name == "TEST_KEY"
}

func TestJudge(t *testing.T) {
	t.Run("the brief goes as one streamed message, the answer comes back with its progress", func(t *testing.T) {
		server, seen := serve(t, http.StatusOK,
			event("message_start", `{"type":"message_start","message":{"id":"m"}}`),
			event("ping", `{"type":"ping"}`),
			event("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hm"}}`),
			event("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"{\"version\""}}`),
			event("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":":2}"}}`),
			event("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`),
			event("message_stop", `{"type":"message_stop"}`))
		judge := Judge{Provider: declaration.Provider{Protocol: "anthropic", URL: server.URL + "/anthropic/", Model: "deepseek-v4-pro", KeyVariable: "TEST_KEY"}, Lookup: lookup}
		var updates []ports.JudgeProgress
		answer, session, err := judge.Ask("the brief", "", func(update ports.JudgeProgress) { updates = append(updates, update) })
		if err != nil || answer != `{"version":2}` || session != "" {
			t.Fatalf("answer %q session %q err %v", answer, session, err)
		}
		if seen.Path != "/anthropic/v1/messages" || seen.Header.Get("x-api-key") != "secret" || seen.Header.Get("anthropic-version") != "2023-06-01" || seen.Header.Get("authorization") != "" {
			t.Fatalf("path %q header %v", seen.Path, seen.Header)
		}
		if seen.Request.Model != "deepseek-v4-pro" || seen.Request.MaxTokens != defaultMaxTokens || !seen.Request.Stream || len(seen.Request.Messages) != 1 || seen.Request.Messages[0].Role != "user" || seen.Request.Messages[0].Content != "the brief" {
			t.Fatalf("request %+v", seen.Request)
		}
		if len(updates) != 3 || !updates[0].Thinking || updates[1].Written != `{"version"` || updates[2].Written != `{"version":2}` {
			t.Fatalf("progress %+v", updates)
		}
		if !strings.HasPrefix(judge.Name(), "api:127.0.0.1:") || judge.ResumeCommand("x") != "" {
			t.Fatalf("name %q", judge.Name())
		}
	})
	t.Run("a reply sends the brief, the answer and the message as one conversation (P2.58)", func(t *testing.T) {
		server, seen := serve(t, http.StatusOK, event("content_block_delta", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"mended"}}`), event("message_stop", `{"type":"message_stop"}`))
		judge := Judge{Provider: declaration.Provider{Protocol: "anthropic", URL: server.URL, Model: "deepseek-v4-pro", KeyVariable: "TEST_KEY"}, Lookup: lookup}
		answer, session, err := judge.Reply(ports.JudgeConversation{Brief: "the brief", Answer: "the first answer"}, "mend it", "", nil)
		if err != nil || answer != "mended" || session != "" {
			t.Fatalf("answer %q session %q err %v", answer, session, err)
		}
		var turns []string
		for _, sent := range seen.Request.Messages {
			turns = append(turns, sent.Role+": "+sent.Content)
		}
		if strings.Join(turns, " | ") != "user: the brief | assistant: the first answer | user: mend it" || seen.Request.Model != "deepseek-v4-pro" {
			t.Fatalf("turns %q request %+v", turns, seen.Request)
		}
	})
	t.Run("a bearer token, a named model and max_tokens", func(t *testing.T) {
		server, seen := serve(t, http.StatusOK, event("content_block_delta", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}`), event("message_stop", `{"type":"message_stop"}`))
		judge := Judge{Provider: declaration.Provider{Protocol: "anthropic", URL: server.URL, Model: "kimi-k2", Auth: declaration.AuthBearer, KeyVariable: "TEST_KEY", MaxTokens: 64000}, Lookup: lookup}
		answer, _, err := judge.Ask("brief", "kimi-k2-thinking", nil)
		if err != nil || answer != "ok" || seen.Header.Get("authorization") != "Bearer secret" || seen.Header.Get("x-api-key") != "" || seen.Request.Model != "kimi-k2-thinking" || seen.Request.MaxTokens != 64000 {
			t.Fatalf("answer %q err %v header %v request %+v", answer, err, seen.Header, seen.Request)
		}
	})
	t.Run("a refusal, an error in the stream and a cut answer are said as they are", func(t *testing.T) {
		provider := declaration.Provider{Protocol: "anthropic", Model: "m", KeyVariable: "TEST_KEY"}
		for expected, events := range map[string][]string{
			"answered 401 Unauthorized: invalid x-api-key":   {`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`},
			"overloaded_error: Overloaded":                   {event("error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)},
			"stopped at max_tokens, 16384; raise max_tokens": {event("content_block_delta", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"{"}}`), event("message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`), event("message_stop", `{"type":"message_stop"}`)},
			"ended before its message_stop":                  {event("content_block_delta", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"{"}}`)},
		} {
			status := http.StatusOK
			if strings.HasPrefix(expected, "answered 401") {
				status = http.StatusUnauthorized
			}
			server, _ := serve(t, status, events...)
			provider.URL = server.URL
			_, _, err := Judge{Provider: provider, Lookup: lookup}.Ask("brief", "", nil)
			if err == nil || !strings.Contains(err.Error(), expected) {
				t.Fatalf("%s: err %v", expected, err)
			}
		}
	})
	t.Run("without its key it is not available, and says why", func(t *testing.T) {
		judge := Judge{Provider: declaration.Provider{URL: "https://api.deepseek.com/anthropic", KeyVariable: "MISSING_KEY"}, Lookup: lookup}
		reason, available := judge.Available()
		if available || !strings.Contains(reason, "MISSING_KEY") || !strings.Contains(reason, "api.deepseek.com") || !strings.Contains(reason, "rein key set MISSING_KEY") {
			t.Fatalf("available %v reason %q", available, reason)
		}
	})
}
