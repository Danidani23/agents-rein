// Package messagesapi is the api backend of the judge port (D23): an
// endpoint speaking Anthropic's Messages API, Anthropic's own or one that
// serves the same API, such as DeepSeek's or Moonshot's for Kimi (D42). The
// brief goes as one message, no tools, and the answer streams back
// ([docs](https://platform.claude.com/docs/en/build-with-claude/streaming)).
package messagesapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// defaultMaxTokens caps an answer when the provider names no cap: room for
// a large harness's judgements and units.
const defaultMaxTokens = 16384

// timeout is how long the distiller may think, as for the agent CLI.
const timeout = 15 * time.Minute

// apiVersion is the Messages API version rein speaks.
const apiVersion = "2023-06-01"

// Judge asks the organisation's provider.
type Judge struct {
	Provider declaration.Provider
	// Lookup reads an environment variable; nil means os.LookupEnv.
	Lookup func(name string) (string, bool)
}

var _ ports.Judge = Judge{}

// Name is the backend's name for the report: the endpoint's host.
func (judge Judge) Name() string {
	endpoint, err := url.Parse(judge.Provider.URL)
	if err != nil {
		return "api"
	}
	return "api:" + endpoint.Host
}

// key is the provider's key from the developer's environment.
func (judge Judge) key() (string, bool) {
	lookup := judge.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, found := lookup(judge.Provider.KeyVariable)
	return value, found && value != ""
}

// Available needs the key's variable set.
func (judge Judge) Available() (string, bool) {
	_, found := judge.key()
	if !found {
		return fmt.Sprintf("%s, which holds the key for %s, is not set; export it", judge.Provider.KeyVariable, strings.TrimPrefix(judge.Name(), "api:")), false
	}
	return "", true
}

// ResumeCommand is empty: an API call keeps no conversation to open.
func (Judge) ResumeCommand(string) string {
	return ""
}

// request is the Messages API's request body, as much of it as rein sends.
type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// streamed is one event of the stream, or the body of a refusal: the
// fields rein reads of either.
type streamed struct {
	Type  string `json:"type"`
	Delta struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Ask sends the brief to the provider's model, or to model when one is
// named, and reads the answer as it streams, telling progress how far it
// has come. A refusal, an error in the stream, an answer cut at its cap and
// a stream that ends early are each an error that says so.
func (judge Judge) Ask(brief string, model string, progress func(ports.JudgeProgress)) (string, string, error) {
	key, found := judge.key()
	if !found {
		reason, _ := judge.Available()
		return "", "", fmt.Errorf("%s", reason)
	}
	if model == "" {
		model = judge.Provider.Model
	}
	maxTokens := judge.Provider.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}
	body, err := json.Marshal(request{Model: model, MaxTokens: maxTokens, Stream: true, Messages: []message{{Role: "user", Content: brief}}})
	if err != nil {
		return "", "", err
	}
	sent, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(judge.Provider.URL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	sent.Header.Set("content-type", "application/json")
	sent.Header.Set("anthropic-version", apiVersion)
	if judge.Provider.Auth == declaration.AuthBearer {
		sent.Header.Set("authorization", "Bearer "+key)
	} else {
		sent.Header.Set("x-api-key", key)
	}
	host := strings.TrimPrefix(judge.Name(), "api:")
	response, err := (&http.Client{Timeout: timeout}).Do(sent)
	if err != nil {
		return "", "", fmt.Errorf("%s did not answer: %w", host, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		refused, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		var parsed streamed
		reason := strings.TrimSpace(string(refused))
		if json.Unmarshal(refused, &parsed) == nil && parsed.Error.Message != "" {
			reason = parsed.Error.Message
		}
		return "", "", fmt.Errorf("%s answered %s: %s", host, response.Status, reason)
	}
	var written strings.Builder
	stopped, cut := false, false
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		data, isData := strings.CutPrefix(scanner.Text(), "data:")
		var event streamed
		if !isData || json.Unmarshal([]byte(strings.TrimSpace(data)), &event) != nil {
			continue
		}
		switch event.Type {
		case "content_block_delta":
			switch event.Delta.Type {
			case "thinking_delta":
				if written.Len() == 0 && progress != nil {
					progress(ports.JudgeProgress{Thinking: true})
				}
			case "text_delta":
				written.WriteString(event.Delta.Text)
				if progress != nil {
					progress(ports.JudgeProgress{Written: written.String()})
				}
			}
		case "message_delta":
			cut = event.Delta.StopReason == "max_tokens"
		case "message_stop":
			stopped = true
		case "error":
			return "", "", fmt.Errorf("%s reported %s: %s", host, event.Error.Type, event.Error.Message)
		}
	}
	err = scanner.Err()
	if err != nil {
		return "", "", fmt.Errorf("%s's stream broke off: %w", host, err)
	}
	switch {
	case cut:
		return "", "", fmt.Errorf("the answer stopped at max_tokens, %d; raise max_tokens in %s, or distill with a model that writes less", maxTokens, declaration.ProvidersFile)
	case !stopped:
		return "", "", fmt.Errorf("%s's stream ended before its message_stop", host)
	}
	return written.String(), "", nil
}
