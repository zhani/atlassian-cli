package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// AtlassianConfig holds connection settings for an Atlassian service.
// Email が設定されていれば Basic 認証 (Cloud)、未設定なら Bearer 認証 (Server/DC)。
type AtlassianConfig struct {
	URL            string
	Email          string
	Token          string
	DefaultProject string
}

// loadConfig reads <prefix>_* variables, falling back to the shared
// ATLASSIAN_* variables for anything unset. urlFallback derives the service
// URL from ATLASSIAN_URL ("" when it cannot be derived).
func loadConfig(prefix string, urlFallback func(string) string) (*AtlassianConfig, error) {
	u := os.Getenv(prefix + "_URL")
	if u == "" {
		if shared := os.Getenv("ATLASSIAN_URL"); shared != "" {
			u = urlFallback(shared)
		}
	}

	// API tokens use Basic auth together with an email; personal access
	// tokens use Bearer auth, so a shared email must not be paired with them.
	var token string
	apiToken := false
	for _, c := range []struct {
		name string
		api  bool
	}{
		{prefix + "_API_TOKEN", true},
		{prefix + "_PERSONAL_TOKEN", false},
		{"ATLASSIAN_API_TOKEN", true},
		{"ATLASSIAN_PERSONAL_TOKEN", false},
	} {
		if v := os.Getenv(c.name); v != "" {
			token, apiToken = v, c.api
			break
		}
	}

	email := os.Getenv(prefix + "_EMAIL")
	if email == "" && apiToken {
		email = os.Getenv("ATLASSIAN_EMAIL")
	}

	if u == "" {
		return nil, fmt.Errorf("%s_URL environment variable is required (or ATLASSIAN_URL)", prefix)
	}
	if token == "" {
		return nil, fmt.Errorf("%s_API_TOKEN (or %s_PERSONAL_TOKEN, ATLASSIAN_API_TOKEN, ATLASSIAN_PERSONAL_TOKEN) environment variable is required", prefix, prefix)
	}

	return &AtlassianConfig{
		URL:            u,
		Email:          email,
		Token:          token,
		DefaultProject: os.Getenv(prefix + "_DEFAULT_PROJECT"),
	}, nil
}

func LoadJiraConfig() (*AtlassianConfig, error) {
	return loadConfig("JIRA", func(shared string) string { return shared })
}

// LoadConfluenceConfig derives the Confluence URL from ATLASSIAN_URL only on
// Cloud, where Confluence lives under /wiki on the same site. Server/DC
// installs run Confluence on its own host, so CONFLUENCE_URL is required.
func LoadConfluenceConfig() (*AtlassianConfig, error) {
	return loadConfig("CONFLUENCE", func(shared string) string {
		parsed, err := url.Parse(shared)
		if err != nil || !strings.HasSuffix(parsed.Hostname(), ".atlassian.net") {
			return ""
		}
		return strings.TrimRight(shared, "/") + "/wiki"
	})
}
