package jira

import (
	"encoding/json"
	"fmt"
	"time"
)

type Issue struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Self   string `json:"self"`
	Fields Fields `json:"fields"`
}

type Fields struct {
	Summary     string          `json:"summary"`
	Status      StatusField     `json:"status"`
	Priority    *NameField      `json:"priority"`
	IssueType   IssueTypeField  `json:"issuetype"`
	Assignee    *UserField      `json:"assignee"`
	Reporter    *UserField      `json:"reporter"`
	Created     string          `json:"created"`
	Updated     string          `json:"updated"`
	DueDate     string          `json:"duedate"`
	Resolution  *NameField      `json:"resolution"`
	Description json.RawMessage `json:"description"`
	Labels      []string        `json:"labels"`
	Parent      *ParentField    `json:"parent"`
	Components  []NameField     `json:"components"`
	IssueLinks  []IssueLink     `json:"issuelinks"`
	Subtasks    []RelatedIssue  `json:"subtasks"`
	Attachments []Attachment    `json:"attachment"`
}

// IssueLink is one entry of the issuelinks field. Exactly one of InwardIssue
// or OutwardIssue is set, which determines the direction of the relation.
type IssueLink struct {
	ID           string        `json:"id"`
	Type         IssueLinkType `json:"type"`
	InwardIssue  *RelatedIssue `json:"inwardIssue"`
	OutwardIssue *RelatedIssue `json:"outwardIssue"`
}

// RelatedIssue is the trimmed issue shape Jira embeds in links and subtasks.
type RelatedIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary   string         `json:"summary"`
		Status    StatusField    `json:"status"`
		IssueType IssueTypeField `json:"issuetype"`
		Priority  *NameField     `json:"priority"`
	} `json:"fields"`
}

type NameField struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// StatusField is an issue status. StatusCategory groups statuses into Jira's
// three fixed buckets (new/indeterminate/done), which is what lets the TUI
// order and color workflows it doesn't know by name.
type StatusField struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	StatusCategory *StatusCategory `json:"statusCategory,omitempty"`
}

type StatusCategory struct {
	ID        int    `json:"id"`
	Key       string `json:"key"`       // "new", "indeterminate" or "done"
	ColorName string `json:"colorName"` // "blue-gray", "yellow", "green"
	Name      string `json:"name"`
}

// CategoryKey returns the status category key ("new", "indeterminate",
// "done"), or "" when Jira didn't include it.
func (s StatusField) CategoryKey() string {
	if s.StatusCategory == nil {
		return ""
	}
	return s.StatusCategory.Key
}

// IssueTypeField is an issue type. HierarchyLevel is -1 for sub-tasks, 0 for
// standard issues and 1 for epics.
type IssueTypeField struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Subtask        bool   `json:"subtask,omitempty"`
	HierarchyLevel int    `json:"hierarchyLevel,omitempty"`
}

type UserField struct {
	DisplayName string `json:"displayName"`
	AccountID   string `json:"accountId"`
}

type ParentField struct {
	Key    string `json:"key"`
	Fields struct {
		Summary   string         `json:"summary"`
		Status    StatusField    `json:"status"`
		IssueType IssueTypeField `json:"issuetype"`
		Priority  *NameField     `json:"priority"`
	} `json:"fields"`
}

// SearchResult is the response of POST /rest/api/3/search/jql. That endpoint
// does not return a total count — only isLast/nextPageToken for pagination.
type SearchResult struct {
	Issues        []Issue `json:"issues"`
	IsLast        bool    `json:"isLast"`
	NextPageToken string  `json:"nextPageToken"`
}

type Transition struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	To   StatusField `json:"to"`
}

type TransitionsResult struct {
	Transitions []Transition `json:"transitions"`
}

type Comment struct {
	ID      string          `json:"id"`
	Author  UserField       `json:"author"`
	Body    json.RawMessage `json:"body"`
	Created string          `json:"created"`
}

type CommentsResult struct {
	Comments []Comment `json:"comments"`
	Total    int       `json:"total"`
}

type Myself struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	Active       bool   `json:"active"`
	TimeZone     string `json:"timeZone"`
}

type Project struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type IssueTypeMeta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Subtask     bool   `json:"subtask"`
}

type IssueLinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Content  string `json:"content"`
}

// RemoteLink is an external link on an issue: Confluence pages, web links,
// and links created by integrations.
type RemoteLink struct {
	ID           int64  `json:"id"`
	Relationship string `json:"relationship"`
	Application  struct {
		Name string `json:"name"`
	} `json:"application"`
	Object struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"object"`
}

type WatchersResult struct {
	WatchCount int         `json:"watchCount"`
	IsWatching bool        `json:"isWatching"`
	Watchers   []UserField `json:"watchers"`
}

type Worklog struct {
	Author    UserField       `json:"author"`
	TimeSpent string          `json:"timeSpent"`
	Started   string          `json:"started"`
	Comment   json.RawMessage `json:"comment"`
}

// FieldMeta describes a Jira field (system or custom), for discovery of
// custom field IDs like story points.
type FieldMeta struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Custom bool   `json:"custom"`
	Schema struct {
		Type string `json:"type"`
	} `json:"schema"`
}

// DevStatus summarizes development info (branches, PRs, commits) linked to
// an issue via the GitHub/GitLab/Bitbucket integration.
type DevStatus struct {
	Branches     []DevBranch      `json:"branches,omitempty"`
	PullRequests []DevPullRequest `json:"pullRequests,omitempty"`
	Commits      []DevCommit      `json:"commits,omitempty"`
}

type DevBranch struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Repository string `json:"repository,omitempty"`
}

type DevPullRequest struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	Status       string `json:"status"` // OPEN, MERGED, DECLINED
	Author       string `json:"author,omitempty"`
	LastUpdate   string `json:"lastUpdate,omitempty"`
	SourceBranch string `json:"sourceBranch,omitempty"`
}

type DevCommit struct {
	Message    string `json:"message"`
	URL        string `json:"url"`
	Author     string `json:"author,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
	Repository string `json:"repository,omitempty"`
}

type ChangelogItem struct {
	Field      string `json:"field"`
	FieldType  string `json:"fieldtype"`
	From       string `json:"from"`
	FromString string `json:"fromString"`
	To         string `json:"to"`
	ToString   string `json:"toString"`
}

type Changelog struct {
	ID      string          `json:"id"`
	Author  UserField       `json:"author"`
	Created string          `json:"created"`
	Items   []ChangelogItem `json:"items"`
}

func ParseTime(s string) time.Time {
	layouts := []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000+0000",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func RelativeTime(s string) string {
	t := ParseTime(s)
	if t.IsZero() {
		return s
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		return t.Format("Jan 02")
	}
}
