package mcptools

import "sort"

var toolGroups = map[string][]string{
	"jira_user":              {"jira_get_myself", "jira_search_users"},
	"jira_issue":             {"jira_get_issue", "jira_get_subtasks"},
	"jira_search":            {"jira_search_issues"},
	"jira_create":            {"jira_create_issue", "jira_clone_issue"},
	"jira_update":            {"jira_update_issue", "jira_assign_issue"},
	"jira_delete":            {"jira_delete_issue"},
	"jira_transition":        {"jira_transition_issue", "jira_get_transitions"},
	"jira_comment":           {"jira_list_comments", "jira_add_comment"},
	"jira_link":              {"jira_link_issues", "jira_unlink_issues", "jira_add_remote_link", "jira_get_link_types"},
	"jira_worklog":           {"jira_add_worklog"},
	"jira_fields":            {"jira_list_fields"},
	"jira_development":       {"jira_get_development_info"},
	"jira_feature":           {"jira_get_feature_children"},
	"jira_attachment":        {"jira_list_attachments", "jira_get_attachment", "jira_download_attachment", "jira_upload_attachment", "jira_delete_attachment"},
	"jira_api":               {"jira_api_request"},
	"confluence_page":        {"confluence_get_page", "confluence_create_page", "confluence_update_page", "confluence_copy_page", "confluence_move_page"},
	"confluence_search":      {"confluence_search", "confluence_get_children"},
	"confluence_label":       {"confluence_list_labels", "confluence_add_labels", "confluence_remove_label"},
	"confluence_restriction": {"confluence_list_restrictions", "confluence_add_restriction", "confluence_remove_restriction"},
	"confluence_attachment":  {"confluence_list_attachments", "confluence_download_attachment", "confluence_upload_attachment", "confluence_delete_attachment"},
	"confluence_api":         {"confluence_api_request"},
	"bookmarks":              {"bookmark_list", "bookmark_add", "bookmark_remove"},
}

// optInGroups are left out unless named in --tools: raw API access is broad
// and rarely needed, so it should not cost context by default.
var optInGroups = map[string]bool{
	"jira_api":       true,
	"confluence_api": true,
}

func isToolEnabled(name string, enabledGroups []string) bool {
	if len(enabledGroups) == 0 {
		for group, tools := range toolGroups {
			for _, t := range tools {
				if t == name {
					return !optInGroups[group]
				}
			}
		}
		return false
	}
	for _, group := range enabledGroups {
		for _, t := range toolGroups[group] {
			if t == name {
				return true
			}
		}
	}
	return false
}

// AllGroups returns all available tool group names, sorted.
func AllGroups() []string {
	groups := make([]string, 0, len(toolGroups))
	for g := range toolGroups {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups
}

// OptInGroups returns the groups that are only enabled when named, sorted.
func OptInGroups() []string {
	groups := make([]string, 0, len(optInGroups))
	for g := range optInGroups {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups
}
