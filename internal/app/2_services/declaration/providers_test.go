package declaration

import (
	"strings"
	"testing"
)

const validProviders = "distiller:\n  protocol: anthropic\n  url: https://api.deepseek.com/anthropic\n  model: deepseek-v4-pro\n  key_variable: DEEPSEEK_API_KEY\n"

func TestProviders(t *testing.T) {
	t.Run("the distiller's endpoint, model and key variable are read", func(t *testing.T) {
		providers, problems := ParseProviders([]byte(validProviders))
		if len(problems) != 0 || providers.Distiller == nil || providers.Distiller.URL != "https://api.deepseek.com/anthropic" || providers.Distiller.Model != "deepseek-v4-pro" || providers.Distiller.KeyVariable != "DEEPSEEK_API_KEY" {
			t.Fatalf("providers %+v problems %v", providers, problems)
		}
		// a local endpoint, private inference on this machine, may speak plain http
		_, problems = ParseProviders([]byte(strings.Replace(validProviders, "https://api.deepseek.com/anthropic", "http://localhost:8080", 1)))
		if len(problems) != 0 {
			t.Fatalf("local endpoint: %v", problems)
		}
	})
	t.Run("what cannot work is refused, a literal key above all", func(t *testing.T) {
		_, problems := ParseProviders([]byte("distiller:\n  protocol: openai\n  url: http://example.com/v1\n  model: \"\"\n  auth: basic\n  key_variable: sk-123\n  max_tokens: -1\n"))
		for _, expected := range []string{`protocol "openai" is not anthropic`, `url "http://example.com/v1" is not https`, "model is required", `auth "basic" is neither x-api-key nor bearer`, `key_variable "sk-123" is not the name of an environment variable`, "max_tokens -1 is below zero"} {
			expectProblem(t, problems, expected)
		}
		_, problems = ParseProviders([]byte("distiller:\n  key: sk-123\n"))
		expectProblem(t, problems, "field key not found")
	})
	t.Run("the tree carries the organisation's providers, and reports a broken file at its address", func(t *testing.T) {
		clone := scratchTree(t)
		writeTreeFile(t, clone, "declarations/providers.yaml", validProviders)
		tree, problems, err := LoadTree(clone)
		if err != nil || len(problems) != 0 || tree.Providers.Distiller == nil || tree.Providers.Distiller.Model != "deepseek-v4-pro" {
			t.Fatalf("tree %+v problems %v err %v", tree.Providers, problemsOf(problems), err)
		}
		writeTreeFile(t, clone, "declarations/providers.yaml", "distiller:\n  protocol: anthropic\n")
		tree, problems, _ = LoadTree(clone)
		if tree.Providers.Distiller != nil || !strings.Contains(strings.Join(problemsOf(problems), "\n"), "organizations.acme.providers: ") {
			t.Fatalf("broken: %+v %v", tree.Providers, problemsOf(problems))
		}
	})
}
