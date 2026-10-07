package jira

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	apiURL     string
	browseURL  string
	httpClient *http.Client
	auth       authConfig

	// tokenMu serializes OAuth token refreshes. The TUI fires requests from
	// several goroutines at once; without it, an expired token triggers
	// concurrent refreshes and Atlassian's rotating refresh tokens reject
	// every call but the first.
	tokenMu sync.Mutex
	// tokenSync extends that to other processes (parallel agents).
	tokenSync *TokenSync
}

// TokenSync coordinates OAuth refreshes across processes sharing one
// profile. Lock serializes refreshers; Reload returns the tokens currently
// stored, which another process may have refreshed a moment ago — reusing
// them avoids spending a refresh token someone else already rotated.
type TokenSync struct {
	Lock   func() (unlock func(), err error)
	Reload func() (accessToken, refreshToken string, expiry time.Time, err error)
}

// SetTokenSync enables cross-process refresh coordination.
func (c *Client) SetTokenSync(s *TokenSync) { c.tokenSync = s }

// SetTransport sends every request through rt: the demo site answers in
// memory this way, without a network.
func (c *Client) SetTransport(rt http.RoundTripper) { c.httpClient.Transport = rt }

type authConfig struct {
	method       string // "basic" or "oauth"
	email        string
	token        string
	accessToken  string
	refreshToken string
	clientID     string
	clientSecret string
	tokenExpiry  time.Time
	onRefresh    func(accessToken, refreshToken string, expiry time.Time)
}

func NewBasicClient(baseURL, email, token string) *Client {
	return &Client{
		apiURL:     baseURL,
		browseURL:  baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		auth: authConfig{
			method: "basic",
			email:  email,
			token:  token,
		},
	}
}

func NewOAuthClient(cloudID, browseURL, accessToken, refreshToken, clientID, clientSecret string, expiry time.Time, onRefresh func(string, string, time.Time)) *Client {
	return &Client{
		apiURL:     fmt.Sprintf("https://api.atlassian.com/ex/jira/%s", cloudID),
		browseURL:  browseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		auth: authConfig{
			method:       "oauth",
			accessToken:  accessToken,
			refreshToken: refreshToken,
			clientID:     clientID,
			clientSecret: clientSecret,
			tokenExpiry:  expiry,
			onRefresh:    onRefresh,
		},
	}
}

func (c *Client) do(method, path string, body interface{}) ([]byte, error) {
	if c.auth.method == "oauth" {
		if err := c.ensureValidToken(); err != nil {
			return nil, err
		}
	}

	var bodyData []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyData = data
	}

	const maxAttempts = 3
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		var bodyReader io.Reader
		if bodyData != nil {
			bodyReader = bytes.NewReader(bodyData)
		}

		req, err := http.NewRequest(method, c.apiURL+path, bodyReader)
		if err != nil {
			return nil, err
		}

		if c.auth.method == "oauth" {
			req.Header.Set("Authorization", "Bearer "+c.currentAccessToken())
		} else {
			req.SetBasicAuth(c.auth.email, c.auth.token)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == 401 && c.auth.method == "oauth" {
			return nil, fmt.Errorf("unauthorized — run 'jira setup' to sign in again")
		}

		// Retry on rate limiting (any method — the request was not processed)
		// and on server errors for read-only requests.
		if resp.StatusCode == 429 || (resp.StatusCode >= 500 && method == "GET") {
			lastErr = parseAPIError(resp.StatusCode, respBody)
			if attempt < maxAttempts-1 {
				time.Sleep(retryDelay(resp.Header.Get("Retry-After"), attempt))
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode >= 400 {
			return nil, parseAPIError(resp.StatusCode, respBody)
		}

		return respBody, nil
	}

	return nil, lastErr
}

// parseAPIError turns Jira's error payload ({"errorMessages":[...],"errors":{...}})
// into a readable one-line error instead of a raw JSON dump.
func parseAPIError(status int, body []byte) error {
	var payload struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		var parts []string
		parts = append(parts, payload.ErrorMessages...)
		for field, msg := range payload.Errors {
			parts = append(parts, fmt.Sprintf("%s: %s", field, msg))
		}
		if len(parts) > 0 {
			return fmt.Errorf("jira API error %d: %s", status, strings.Join(parts, "; "))
		}
	}
	raw := strings.TrimSpace(string(body))
	if len(raw) > 300 {
		raw = raw[:300] + "…"
	}
	if raw == "" {
		raw = http.StatusText(status)
	}
	return fmt.Errorf("jira API error %d: %s", status, raw)
}

// retryDelay honors the Retry-After header (seconds) when present, otherwise
// exponential backoff: 500ms, 1s, 2s...
func retryDelay(retryAfter string, attempt int) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs > 0 {
		if secs > 10 {
			secs = 10
		}
		return time.Duration(secs) * time.Second
	}
	return time.Duration(500*(1<<attempt)) * time.Millisecond
}

func (c *Client) currentAccessToken() string {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	return c.auth.accessToken
}

// tokenFresh reports whether the access token is good for 2 more minutes.
func (c *Client) tokenFresh() bool {
	return c.auth.tokenExpiry.IsZero() || time.Now().Before(c.auth.tokenExpiry.Add(-2*time.Minute))
}

func (c *Client) ensureValidToken() error {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.tokenFresh() {
		return nil
	}

	if s := c.tokenSync; s != nil {
		if s.Lock != nil {
			if unlock, err := s.Lock(); err == nil {
				defer unlock()
			}
		}
		if s.Reload != nil {
			if at, rt, exp, err := s.Reload(); err == nil && at != "" && rt != "" {
				c.auth.accessToken, c.auth.refreshToken, c.auth.tokenExpiry = at, rt, exp
				if c.tokenFresh() {
					return nil // another process refreshed while we waited
				}
			}
		}
	}

	if c.auth.refreshToken == "" {
		return fmt.Errorf("token expired — run 'jira setup' to sign in again")
	}

	tokens, err := RefreshToken(c.auth.clientID, c.auth.clientSecret, c.auth.refreshToken)
	if err != nil {
		return fmt.Errorf("token refresh failed: %w — run 'jira setup' to sign in again", err)
	}

	c.auth.accessToken = tokens.AccessToken
	if tokens.RefreshToken != "" {
		c.auth.refreshToken = tokens.RefreshToken
	}
	c.auth.tokenExpiry = time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)

	if c.auth.onRefresh != nil {
		c.auth.onRefresh(c.auth.accessToken, c.auth.refreshToken, c.auth.tokenExpiry)
	}

	return nil
}

var searchFields = []string{"summary", "status", "priority", "issuetype", "assignee", "reporter", "created", "updated", "duedate", "resolution", "description", "labels", "parent", "components", "issuelinks", "subtasks", "attachment"}

// Search runs a JQL query and returns up to maxResults issues, transparently
// paginating (the API caps each page at 100) until done or exhausted.
func (c *Client) Search(jql string, maxResults int) (*SearchResult, error) {
	out := &SearchResult{IsLast: true}
	pageToken := ""

	for len(out.Issues) < maxResults {
		pageSize := maxResults - len(out.Issues)
		if pageSize > 100 {
			pageSize = 100
		}
		page, err := c.searchPage(jql, pageSize, pageToken)
		if err != nil {
			return nil, err
		}
		out.Issues = append(out.Issues, page.Issues...)
		out.IsLast = page.IsLast
		out.NextPageToken = page.NextPageToken
		if page.IsLast || page.NextPageToken == "" || len(page.Issues) == 0 {
			break
		}
		pageToken = page.NextPageToken
	}
	return out, nil
}

func (c *Client) searchPage(jql string, maxResults int, pageToken string) (*SearchResult, error) {
	body := map[string]interface{}{
		"jql":        jql,
		"maxResults": maxResults,
		"fields":     searchFields,
	}
	if pageToken != "" {
		body["nextPageToken"] = pageToken
	}

	data, err := c.do("POST", "/rest/api/3/search/jql", body)
	if err != nil {
		return nil, err
	}

	var result SearchResult
	return &result, json.Unmarshal(data, &result)
}

func (c *Client) GetIssue(key string) (*Issue, error) {
	data, err := c.do("GET", "/rest/api/3/issue/"+key, nil)
	if err != nil {
		return nil, err
	}

	var issue Issue
	return &issue, json.Unmarshal(data, &issue)
}

// GetComments returns up to max comments in chronological order (oldest
// first), so a full read of the thread makes sense top-to-bottom. max is
// clamped to the API page limit of 100; <=0 means 100.
func (c *Client) GetComments(key string, max int) (*CommentsResult, error) {
	if max <= 0 || max > 100 {
		max = 100
	}
	data, err := c.do("GET", fmt.Sprintf("/rest/api/3/issue/%s/comment?orderBy=created&maxResults=%d", key, max), nil)
	if err != nil {
		return nil, err
	}

	var result CommentsResult
	return &result, json.Unmarshal(data, &result)
}

func (c *Client) AddComment(key, body string) error {
	adf, err := c.MarkdownToADF(body)
	if err != nil {
		return err
	}
	_, err = c.do("POST", "/rest/api/3/issue/"+key+"/comment", map[string]interface{}{"body": adf})
	return err
}

func (c *Client) GetTransitions(key string) ([]Transition, error) {
	data, err := c.do("GET", "/rest/api/3/issue/"+key+"/transitions", nil)
	if err != nil {
		return nil, err
	}

	var result TransitionsResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result.Transitions, nil
}

func (c *Client) DoTransition(key, transitionID string) error {
	body := map[string]interface{}{
		"transition": map[string]string{
			"id": transitionID,
		},
	}

	_, err := c.do("POST", "/rest/api/3/issue/"+key+"/transitions", body)
	return err
}

func (c *Client) GetMyself() (*Myself, error) {
	data, err := c.do("GET", "/rest/api/3/myself", nil)
	if err != nil {
		return nil, err
	}

	var myself Myself
	return &myself, json.Unmarshal(data, &myself)
}

func (c *Client) GetProjects() ([]Project, error) {
	data, err := c.do("GET", "/rest/api/3/project?orderBy=name&maxResults=50", nil)
	if err != nil {
		return nil, err
	}

	var projects []Project
	return projects, json.Unmarshal(data, &projects)
}

func (c *Client) AssignIssue(key, accountID string) error {
	body := map[string]interface{}{
		"accountId": accountID,
	}
	if accountID == "" {
		body["accountId"] = nil
	}
	_, err := c.do("PUT", "/rest/api/3/issue/"+key+"/assignee", body)
	return err
}

func (c *Client) CreateIssue(projectKey, issueType, summary, parentKey, description, dueDate string) (*Issue, error) {
	fields := map[string]interface{}{
		"project":   map[string]string{"key": projectKey},
		"issuetype": map[string]string{"name": issueType},
		"summary":   summary,
	}
	if parentKey != "" {
		fields["parent"] = map[string]string{"key": parentKey}
	}
	if description != "" {
		adf, err := c.MarkdownToADF(description)
		if err != nil {
			return nil, err
		}
		fields["description"] = adf
	}
	if dueDate != "" {
		fields["duedate"] = dueDate
	}

	data, err := c.do("POST", "/rest/api/3/issue", map[string]interface{}{"fields": fields})
	if err != nil {
		return nil, err
	}

	var result Issue
	return &result, json.Unmarshal(data, &result)
}

func (c *Client) GetPriorities() ([]NameField, error) {
	data, err := c.do("GET", "/rest/api/3/priority", nil)
	if err != nil {
		return nil, err
	}

	var priorities []NameField
	return priorities, json.Unmarshal(data, &priorities)
}

func (c *Client) SearchUsers(projectKey string) ([]UserField, error) {
	data, err := c.do("GET", "/rest/api/3/user/assignable/search?project="+projectKey+"&maxResults=100", nil)
	if err != nil {
		return nil, err
	}

	var users []UserField
	return users, json.Unmarshal(data, &users)
}

func (c *Client) GetIssueTypes(projectKey string) ([]IssueTypeMeta, error) {
	data, err := c.do("GET", "/rest/api/3/issue/createmeta/"+projectKey+"/issuetypes", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		IssueTypes []IssueTypeMeta `json:"issueTypes"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		// Fallback: try parsing as plain array
		var types []IssueTypeMeta
		if err2 := json.Unmarshal(data, &types); err2 != nil {
			return nil, err
		}
		return types, nil
	}
	return result.IssueTypes, nil
}

func (c *Client) DeleteIssue(key string) error {
	_, err := c.do("DELETE", "/rest/api/3/issue/"+key, nil)
	return err
}

func (c *Client) EditIssue(key string, fields map[string]interface{}) error {
	_, err := c.do("PUT", "/rest/api/3/issue/"+key, map[string]interface{}{"fields": fields})
	return err
}

func (c *Client) BrowseURL(key string) string {
	return c.browseURL + "/browse/" + key
}

func (c *Client) GetLinkTypes() ([]IssueLinkType, error) {
	data, err := c.do("GET", "/rest/api/3/issueLinkType", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		IssueLinkTypes []IssueLinkType `json:"issueLinkTypes"`
	}
	return result.IssueLinkTypes, json.Unmarshal(data, &result)
}

func (c *Client) LinkIssues(inwardKey, outwardKey, linkTypeName string) error {
	body := map[string]interface{}{
		"type":         map[string]string{"name": linkTypeName},
		"inwardIssue":  map[string]string{"key": inwardKey},
		"outwardIssue": map[string]string{"key": outwardKey},
	}
	_, err := c.do("POST", "/rest/api/3/issueLink", body)
	return err
}
