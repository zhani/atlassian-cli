package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

var allVars = []string{
	"JIRA_URL", "JIRA_EMAIL", "JIRA_API_TOKEN", "JIRA_PERSONAL_TOKEN", "JIRA_DEFAULT_PROJECT",
	"CONFLUENCE_URL", "CONFLUENCE_EMAIL", "CONFLUENCE_API_TOKEN", "CONFLUENCE_PERSONAL_TOKEN",
	"ATLASSIAN_URL", "ATLASSIAN_EMAIL", "ATLASSIAN_API_TOKEN", "ATLASSIAN_PERSONAL_TOKEN",
}

// Not parallel: t.Setenv modifies the process environment.
func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		confluence bool
		want       *AtlassianConfig
		wantErr    bool
	}{
		{
			name: "product variables",
			env:  map[string]string{"JIRA_URL": "https://jira.example.com", "JIRA_PERSONAL_TOKEN": "pat", "JIRA_DEFAULT_PROJECT": "P"},
			want: &AtlassianConfig{URL: "https://jira.example.com", Token: "pat", DefaultProject: "P"},
		},
		{
			name: "shared cloud credentials",
			env:  map[string]string{"ATLASSIAN_URL": "https://acme.atlassian.net", "ATLASSIAN_EMAIL": "me@acme.com", "ATLASSIAN_API_TOKEN": "tok"},
			want: &AtlassianConfig{URL: "https://acme.atlassian.net", Email: "me@acme.com", Token: "tok"},
		},
		{
			name:       "confluence derives /wiki on cloud",
			env:        map[string]string{"ATLASSIAN_URL": "https://acme.atlassian.net/", "ATLASSIAN_EMAIL": "me@acme.com", "ATLASSIAN_API_TOKEN": "tok"},
			confluence: true,
			want:       &AtlassianConfig{URL: "https://acme.atlassian.net/wiki", Email: "me@acme.com", Token: "tok"},
		},
		{
			name:       "confluence needs its own URL on server",
			env:        map[string]string{"ATLASSIAN_URL": "https://jira.example.com", "ATLASSIAN_PERSONAL_TOKEN": "pat"},
			confluence: true,
			wantErr:    true,
		},
		{
			name: "product variables win over shared ones",
			env: map[string]string{
				"JIRA_URL": "https://jira.example.com", "JIRA_API_TOKEN": "jira-tok", "JIRA_EMAIL": "jira@x.com",
				"ATLASSIAN_URL": "https://acme.atlassian.net", "ATLASSIAN_EMAIL": "me@acme.com", "ATLASSIAN_API_TOKEN": "tok",
			},
			want: &AtlassianConfig{URL: "https://jira.example.com", Email: "jira@x.com", Token: "jira-tok"},
		},
		{
			name: "shared email is not paired with a personal token",
			env:  map[string]string{"JIRA_URL": "https://jira.example.com", "JIRA_PERSONAL_TOKEN": "pat", "ATLASSIAN_EMAIL": "me@acme.com"},
			want: &AtlassianConfig{URL: "https://jira.example.com", Token: "pat"},
		},
		{
			name:    "missing token",
			env:     map[string]string{"JIRA_URL": "https://jira.example.com"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, v := range allVars {
				t.Setenv(v, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			load := LoadJiraConfig
			if tt.confluence {
				load = LoadConfluenceConfig
			}
			got, err := load()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("config diff (-want +got):\n%s", diff)
			}
		})
	}
}
