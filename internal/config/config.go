package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/zhani/atlassian-cli/internal/mirror"
)

// AtlassianConfig holds connection settings for an Atlassian service.
// Email が設定されていれば Basic 認証 (Cloud)、未設定なら Bearer 認証 (Server/DC)。
type AtlassianConfig struct {
	URL            string
	Email          string
	Token          string
	DefaultProject string

	// Jira explorer settings (unused for Confluence).
	DefaultBoard     string   // JIRA_DEFAULT_BOARD: agile board ID for sprints
	FeatureType      string   // JIRA_FEATURE_TYPE: issue type of features
	FeatureLinkTypes []string // JIRA_FEATURE_LINK_TYPES: link phrases from a feature to its children
	EpicLinkField    string   // JIRA_EPIC_LINK_FIELD: JQL field linking issues to epics
	DetailFields     []string // JIRA_DETAIL_FIELDS: custom fields shown in the TUI issue view
}

// loadConfig reads <prefix>_* variables, falling back to the shared
// ATLASSIAN_* variables for anything unset. urlFallback derives the service
// URL from ATLASSIAN_URL ("" when it cannot be derived).
func loadConfig(prefix string, urlFallback func(string) string) (*AtlassianConfig, error) {
	urlSet := os.Getenv(prefix+"_URL") != "" || os.Getenv("ATLASSIAN_URL") != ""
	if dir, ok := mirror.ActiveDir(urlSet); ok {
		return mirrorConfig(prefix, dir)
	}
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

	cfg := &AtlassianConfig{
		URL:            u,
		Email:          email,
		Token:          token,
		DefaultProject: os.Getenv(prefix + "_DEFAULT_PROJECT"),
		DefaultBoard:   os.Getenv(prefix + "_DEFAULT_BOARD"),
		FeatureType:    os.Getenv(prefix + "_FEATURE_TYPE"),
		EpicLinkField:  os.Getenv(prefix + "_EPIC_LINK_FIELD"),
	}
	cfg.FeatureLinkTypes = splitList(os.Getenv(prefix + "_FEATURE_LINK_TYPES"))
	cfg.DetailFields = splitList(os.Getenv(prefix + "_DETAIL_FIELDS"))
	return cfg, nil
}

// mirrorConfig points a client at the offline mirror in dir. Explorer
// settings left unset here fall back to the exporting machine's.
func mirrorConfig(prefix, dir string) (*AtlassianConfig, error) {
	m, err := mirror.ReadManifest(dir)
	if err != nil {
		return nil, err
	}
	mirror.Activate(dir)
	cfg := &AtlassianConfig{URL: mirror.JiraBase, Token: "mirror"}
	if prefix == "CONFLUENCE" {
		cfg.URL = mirror.ConfluenceBase
	}
	if m.Cloud {
		cfg.Email = "mirror@mirror.invalid" // selects the Cloud API flavour
	}
	or := func(env, fallback string) string {
		if v := os.Getenv(prefix + env); v != "" {
			return v
		}
		return fallback
	}
	orList := func(env string, fallback []string) []string {
		if v := splitList(os.Getenv(prefix + env)); len(v) > 0 {
			return v
		}
		return fallback
	}
	st := m.Settings
	cfg.DefaultProject = or("_DEFAULT_PROJECT", st.DefaultProject)
	cfg.DefaultBoard = or("_DEFAULT_BOARD", st.DefaultBoard)
	cfg.FeatureType = or("_FEATURE_TYPE", st.FeatureType)
	cfg.EpicLinkField = or("_EPIC_LINK_FIELD", st.EpicLinkField)
	cfg.FeatureLinkTypes = orList("_FEATURE_LINK_TYPES", st.FeatureLinkTypes)
	cfg.DetailFields = orList("_DETAIL_FIELDS", st.DetailFields)
	return cfg, nil
}

// MirrorDir returns the active offline mirror's directory, if any.
func MirrorDir() (string, bool) {
	set := os.Getenv("JIRA_URL") != "" || os.Getenv("CONFLUENCE_URL") != "" || os.Getenv("ATLASSIAN_URL") != ""
	return mirror.ActiveDir(set)
}

// splitList splits a comma-separated variable, dropping empty entries.
func splitList(v string) []string {
	var out []string
	for _, t := range strings.Split(v, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Defaults for the feature explorer.
var (
	DefaultFeatureType      = "New Feature"
	DefaultFeatureLinkTypes = []string{"is implemented by", "gets values from"}
)

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
