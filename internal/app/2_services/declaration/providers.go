package declaration

import (
	"fmt"
	"net/url"
	"regexp"
)

// ProvidersFile is the organisation's providers record, beside its own.
const ProvidersFile = "providers.yaml"

// Providers is the organisation's providers.yaml: the endpoint its
// distiller asks, which wins over each developer's agent CLI (D23, D42).
type Providers struct {
	Distiller *Provider `yaml:"distiller,omitempty"`
}

// Provider is an endpoint speaking a model API, and the name of the
// variable that holds its key in each developer's environment, never the
// key itself (D22).
type Provider struct {
	// Protocol is the API the endpoint speaks: anthropic, the Messages API,
	// which Anthropic, DeepSeek and Moonshot for Kimi serve.
	Protocol string `yaml:"protocol"`
	URL      string `yaml:"url"`
	Model    string `yaml:"model"`
	// Auth says how the key is sent: x-api-key, the default, or bearer.
	Auth        string `yaml:"auth,omitempty"`
	KeyVariable string `yaml:"key_variable"`
	// MaxTokens caps the answer; 0 leaves the backend's default.
	MaxTokens int `yaml:"max_tokens,omitempty"`
}

// The protocol and the ways to send the key a provider may name.
const (
	ProtocolAnthropic = "anthropic"
	AuthAPIKey        = "x-api-key"
	AuthBearer        = "bearer"
)

// variablePattern is an environment variable's name: what key_variable
// holds, so a key pasted there is refused.
var variablePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ParseProviders reads providers.yaml: unknown fields, a protocol other
// than anthropic, an endpoint that is not https unless it is this machine,
// no model, an unknown auth, a key_variable that is no variable's name and
// a max_tokens below zero are problems.
func ParseProviders(content []byte) (Providers, []Problem) {
	var providers Providers
	problems := decodeStrict(content, &providers, ProvidersFile)
	if len(problems) > 0 || providers.Distiller == nil {
		return providers, problems
	}
	provider := providers.Distiller
	fail := func(format string, values ...any) {
		problems = append(problems, Problem{Severity: SeverityError, Message: "distiller: " + fmt.Sprintf(format, values...)})
	}
	if provider.Protocol != ProtocolAnthropic {
		fail("protocol %q is not anthropic, the Messages API", provider.Protocol)
	}
	endpoint, err := url.Parse(provider.URL)
	local := err == nil && endpoint.Scheme == "http" && (endpoint.Hostname() == "localhost" || endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "::1")
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && !local) {
		fail("url %q is not https, which a key may only travel over, nor an endpoint on this machine", provider.URL)
	}
	if provider.Model == "" {
		fail("model is required")
	}
	if provider.Auth != "" && provider.Auth != AuthAPIKey && provider.Auth != AuthBearer {
		fail("auth %q is neither x-api-key nor bearer", provider.Auth)
	}
	if !variablePattern.MatchString(provider.KeyVariable) {
		fail("key_variable %q is not the name of an environment variable; it names where the key is, never the key itself", provider.KeyVariable)
	}
	if provider.MaxTokens < 0 {
		fail("max_tokens %d is below zero", provider.MaxTokens)
	}
	if len(problems) > 0 {
		return Providers{}, problems
	}
	return providers, nil
}
