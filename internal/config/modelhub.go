package config

import "strings"

// ModelHubKey represents the configuration for ModelHub API keys.
// ModelHub accepts OpenAI-compatible request and response payloads but uses
// a provider-specific endpoint and query-string API key authentication.
type ModelHubKey struct {
	// APIKey is the authentication key for accessing the ModelHub API.
	APIKey string `yaml:"api-key" json:"api-key"`

	// Priority controls selection preference when multiple credentials match.
	// Higher values are preferred; defaults to 0.
	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`

	// Prefix optionally namespaces model aliases for this credential.
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`

	// BaseURL is the final ModelHub endpoint that receives OpenAI-compatible requests.
	BaseURL string `yaml:"base-url,omitempty" json:"base-url,omitempty"`

	// ProxyURL optionally overrides the global proxy for this API key.
	ProxyURL string `yaml:"proxy-url,omitempty" json:"proxy-url,omitempty"`

	// Headers optionally adds extra HTTP headers for requests sent with this key.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// Models defines the model configurations including aliases for routing.
	Models []ModelHubModel `yaml:"models,omitempty" json:"models,omitempty"`

	// ExcludedModels lists model IDs that should be excluded for this provider.
	ExcludedModels []string `yaml:"excluded-models,omitempty" json:"excluded-models,omitempty"`
}

func (k ModelHubKey) GetAPIKey() string  { return k.APIKey }
func (k ModelHubKey) GetBaseURL() string { return k.BaseURL }

// ModelHubModel represents a model configuration for ModelHub.
type ModelHubModel = OpenAICompatibilityModel

// SanitizeModelHubKeys deduplicates and normalizes ModelHub API key credentials.
func (cfg *Config) SanitizeModelHubKeys() {
	if cfg == nil {
		return
	}

	seen := make(map[string]struct{}, len(cfg.ModelHubAPIKey))
	out := cfg.ModelHubAPIKey[:0]
	for i := range cfg.ModelHubAPIKey {
		entry := cfg.ModelHubAPIKey[i]
		entry.APIKey = strings.TrimSpace(entry.APIKey)
		if entry.APIKey == "" {
			continue
		}
		entry.Prefix = normalizeModelPrefix(entry.Prefix)
		entry.BaseURL = strings.TrimSpace(entry.BaseURL)
		entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
		entry.Headers = NormalizeHeaders(entry.Headers)
		entry.ExcludedModels = NormalizeExcludedModels(entry.ExcludedModels)

		sanitizedModels := make([]ModelHubModel, 0, len(entry.Models))
		for _, model := range entry.Models {
			model.Alias = strings.TrimSpace(model.Alias)
			model.Name = strings.TrimSpace(model.Name)
			if model.Name != "" {
				sanitizedModels = append(sanitizedModels, model)
			}
		}
		entry.Models = sanitizedModels

		uniqueKey := entry.APIKey + "|" + entry.BaseURL
		if _, exists := seen[uniqueKey]; exists {
			continue
		}
		seen[uniqueKey] = struct{}{}
		out = append(out, entry)
	}
	cfg.ModelHubAPIKey = out
}
