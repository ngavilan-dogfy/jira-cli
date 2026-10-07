package jira

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// AddWatcher subscribes accountID (or the current user if empty) to issue key.
func (c *Client) AddWatcher(key, accountID string) error {
	// The API expects a raw JSON string body — not an object.
	var body interface{}
	if accountID != "" {
		body = accountID
	}
	_, err := c.do("POST", "/rest/api/3/issue/"+key+"/watchers", body)
	return err
}

// RemoveWatcher unsubscribes accountID from issue key.
func (c *Client) RemoveWatcher(key, accountID string) error {
	path := "/rest/api/3/issue/" + key + "/watchers"
	if accountID != "" {
		path += "?accountId=" + url.QueryEscape(accountID)
	}
	_, err := c.do("DELETE", path, nil)
	return err
}

// AddAttachment uploads files to an issue. Returns the attachment metadata.
func (c *Client) AddAttachment(key string, paths []string) ([]Attachment, error) {
	if c.auth.method == "oauth" {
		if err := c.ensureValidToken(); err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", p, err)
		}
		part, err := w.CreateFormFile("file", filepath.Base(p))
		if err != nil {
			f.Close()
			return nil, err
		}
		if _, err := io.Copy(part, f); err != nil {
			f.Close()
			return nil, err
		}
		f.Close()
	}
	w.Close()

	req, err := http.NewRequest("POST", c.apiURL+"/rest/api/3/issue/"+key+"/attachments", &buf)
	if err != nil {
		return nil, err
	}
	if c.auth.method == "oauth" {
		req.Header.Set("Authorization", "Bearer "+c.currentAccessToken())
	} else {
		req.SetBasicAuth(c.auth.email, c.auth.token)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Atlassian-Token", "no-check")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	var atts []Attachment
	return atts, json.Unmarshal(data, &atts)
}

// EditComment replaces the body of an existing comment.
func (c *Client) EditComment(key, commentID, body string) error {
	adf, err := c.MarkdownToADF(body)
	if err != nil {
		return err
	}
	_, err = c.do("PUT", "/rest/api/3/issue/"+key+"/comment/"+commentID, map[string]interface{}{"body": adf})
	return err
}

// DeleteComment removes a comment.
func (c *Client) DeleteComment(key, commentID string) error {
	_, err := c.do("DELETE", "/rest/api/3/issue/"+key+"/comment/"+commentID, nil)
	return err
}

// GetRemoteLinks returns external links attached to an issue (Confluence
// pages, web links, integration-created links).
func (c *Client) GetRemoteLinks(key string) ([]RemoteLink, error) {
	data, err := c.do("GET", "/rest/api/3/issue/"+key+"/remotelink", nil)
	if err != nil {
		return nil, err
	}
	var links []RemoteLink
	return links, json.Unmarshal(data, &links)
}

// GetWatchers returns who watches an issue.
func (c *Client) GetWatchers(key string) (*WatchersResult, error) {
	data, err := c.do("GET", "/rest/api/3/issue/"+key+"/watchers", nil)
	if err != nil {
		return nil, err
	}
	var result WatchersResult
	return &result, json.Unmarshal(data, &result)
}

// GetWorklogs returns time logged on an issue.
func (c *Client) GetWorklogs(key string, max int) ([]Worklog, error) {
	if max <= 0 || max > 100 {
		max = 100
	}
	data, err := c.do("GET", fmt.Sprintf("/rest/api/3/issue/%s/worklog?maxResults=%d", key, max), nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Worklogs []Worklog `json:"worklogs"`
	}
	return result.Worklogs, json.Unmarshal(data, &result)
}

// GetFields returns all Jira fields (system + custom), useful to discover
// custom field IDs (story points, teams...).
func (c *Client) GetFields() ([]FieldMeta, error) {
	data, err := c.do("GET", "/rest/api/3/field", nil)
	if err != nil {
		return nil, err
	}
	var fields []FieldMeta
	return fields, json.Unmarshal(data, &fields)
}

// GetDevStatus returns branches, pull requests and commits linked to an
// issue via the development-tool integration (GitHub etc.). It uses an
// internal Jira endpoint; callers should treat errors as "no dev info".
func (c *Client) GetDevStatus(issueID string) (*DevStatus, error) {
	out := &DevStatus{}

	type prJSON struct {
		Name       string `json:"name"`
		URL        string `json:"url"`
		Status     string `json:"status"`
		LastUpdate string `json:"lastUpdate"`
		Author     struct {
			Name string `json:"name"`
		} `json:"author"`
		Source struct {
			Branch string `json:"branch"`
		} `json:"source"`
	}
	type branchJSON struct {
		Name       string `json:"name"`
		URL        string `json:"url"`
		Repository struct {
			Name string `json:"name"`
		} `json:"repository"`
	}
	type commitJSON struct {
		Message string `json:"message"`
		URL     string `json:"url"`
		Author  struct {
			Name string `json:"name"`
		} `json:"author"`
		AuthorTimestamp string `json:"authorTimestamp"`
	}

	fetch := func(dataType string) ([]byte, error) {
		return c.do("GET", fmt.Sprintf("/rest/dev-status/1.0/issue/detail?issueId=%s&applicationType=GitHub&dataType=%s", issueID, dataType), nil)
	}

	if data, err := fetch("pullrequest"); err == nil {
		var resp struct {
			Detail []struct {
				PullRequests []prJSON `json:"pullRequests"`
			} `json:"detail"`
		}
		if json.Unmarshal(data, &resp) == nil {
			for _, d := range resp.Detail {
				for _, pr := range d.PullRequests {
					out.PullRequests = append(out.PullRequests, DevPullRequest{
						Name:         pr.Name,
						URL:          pr.URL,
						Status:       pr.Status,
						Author:       pr.Author.Name,
						LastUpdate:   pr.LastUpdate,
						SourceBranch: pr.Source.Branch,
					})
				}
			}
		}
	}

	if data, err := fetch("branch"); err == nil {
		var resp struct {
			Detail []struct {
				Branches []branchJSON `json:"branches"`
			} `json:"detail"`
		}
		if json.Unmarshal(data, &resp) == nil {
			for _, d := range resp.Detail {
				for _, b := range d.Branches {
					out.Branches = append(out.Branches, DevBranch{
						Name:       b.Name,
						URL:        b.URL,
						Repository: b.Repository.Name,
					})
				}
			}
		}
	}

	if data, err := fetch("repository"); err == nil {
		var resp struct {
			Detail []struct {
				Repositories []struct {
					Name    string       `json:"name"`
					Commits []commitJSON `json:"commits"`
				} `json:"repositories"`
			} `json:"detail"`
		}
		if json.Unmarshal(data, &resp) == nil {
			for _, d := range resp.Detail {
				for _, repo := range d.Repositories {
					for _, cm := range repo.Commits {
						out.Commits = append(out.Commits, DevCommit{
							Message:    cm.Message,
							URL:        cm.URL,
							Author:     cm.Author.Name,
							Timestamp:  cm.AuthorTimestamp,
							Repository: repo.Name,
						})
					}
				}
			}
		}
	}

	return out, nil
}

// DownloadAttachment fetches an attachment's binary content to destPath.
// It goes through the API gateway (works for both OAuth and basic auth);
// the endpoint redirects to a signed media URL, and Go drops the auth
// header on the cross-host redirect, which is what the media host expects.
func (c *Client) DownloadAttachment(attachmentID, destPath string) error {
	if c.auth.method == "oauth" {
		if err := c.ensureValidToken(); err != nil {
			return err
		}
	}

	req, err := http.NewRequest("GET", c.apiURL+"/rest/api/3/attachment/content/"+attachmentID, nil)
	if err != nil {
		return err
	}
	if c.auth.method == "oauth" {
		req.Header.Set("Authorization", "Bearer "+c.currentAccessToken())
	} else {
		req.SetBasicAuth(c.auth.email, c.auth.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("download failed (%d): %s", resp.StatusCode, string(body))
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

// GetChangelog returns the history of field changes for an issue.
func (c *Client) GetChangelog(key string, maxResults int) ([]Changelog, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	path := fmt.Sprintf("/rest/api/3/issue/%s/changelog?maxResults=%d", key, maxResults)
	data, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Values []Changelog `json:"values"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Values, nil
}

// GetChangelogPage returns one page of an issue's change history (oldest
// first, as the API orders it) plus the total number of entries, so callers
// can jump straight to the most recent page.
func (c *Client) GetChangelogPage(key string, startAt, maxResults int) ([]Changelog, int, error) {
	path := fmt.Sprintf("/rest/api/3/issue/%s/changelog?startAt=%d&maxResults=%d", key, startAt, maxResults)
	data, err := c.do("GET", path, nil)
	if err != nil {
		return nil, 0, err
	}
	var result struct {
		Total  int         `json:"total"`
		Values []Changelog `json:"values"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, 0, err
	}
	return result.Values, result.Total, nil
}

// GetProjectStatuses returns the statuses of a project's workflows (the
// union across issue types), each with its status category.
func (c *Client) GetProjectStatuses(projectKey string) ([]StatusField, error) {
	data, err := c.do("GET", "/rest/api/3/project/"+projectKey+"/statuses", nil)
	if err != nil {
		return nil, err
	}
	var byType []struct {
		Statuses []StatusField `json:"statuses"`
	}
	if err := json.Unmarshal(data, &byType); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []StatusField
	for _, t := range byType {
		for _, s := range t.Statuses {
			if !seen[s.Name] {
				seen[s.Name] = true
				out = append(out, s)
			}
		}
	}
	return out, nil
}

// PollStatus waits until the issue reaches one of the target statuses or the
// timeout elapses. Returns the final status name.
func (c *Client) PollStatus(key string, targets []string, timeout, interval time.Duration) (string, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	matchSet := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		matchSet[normalizeStatus(t)] = struct{}{}
	}

	for {
		issue, err := c.GetIssue(key)
		if err != nil {
			return "", err
		}
		cur := issue.Fields.Status.Name
		if _, ok := matchSet[normalizeStatus(cur)]; ok {
			return cur, nil
		}
		if time.Now().After(deadline) {
			return cur, fmt.Errorf("timeout waiting for %s to reach %v (last status: %s)", key, targets, cur)
		}
		time.Sleep(interval)
	}
}

func normalizeStatus(s string) string {
	// case-insensitive, trim spaces
	out := ""
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			out += string(r + 32)
		} else if r == ' ' || r == '_' || r == '-' {
			continue
		} else {
			out += string(r)
		}
	}
	return out
}
