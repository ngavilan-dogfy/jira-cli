package jira

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// mentionScanRe finds what ResolveMentions has to look at on a line: code
// spans (skipped), mentions that already carry an account id (kept), and
// bare `@[Name]` mentions (resolved).
var mentionScanRe = regexp.MustCompile("`[^`]+`" + `|@\[([^\]]+)\](\([^)\s]+\))?`)

// ResolveMentions rewrites every `@[Name]` in md as `@[Display Name](accountId)`,
// the form MarkdownToADF turns into a real mention (the person is notified).
// Each name must match exactly one active person; otherwise it returns an
// error naming the candidates, so a comment never goes out mentioning the
// wrong person or silently mentioning no one. Code spans and fenced code
// are left alone, and markdown without `@[` costs no request.
func (c *Client) ResolveMentions(md string) (string, error) {
	if !strings.Contains(md, "@[") {
		return md, nil
	}
	resolved := map[string]UserField{}
	lines := strings.Split(md, "\n")
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		var firstErr error
		lines[i] = mentionScanRe.ReplaceAllStringFunc(line, func(tok string) string {
			m := mentionScanRe.FindStringSubmatch(tok)
			if firstErr != nil || m[1] == "" || m[2] != "" {
				return tok
			}
			name := strings.TrimSpace(m[1])
			u, ok := resolved[strings.ToLower(name)]
			if !ok {
				var err error
				if u, err = c.findUserByName(name); err != nil {
					firstErr = err
					return tok
				}
				resolved[strings.ToLower(name)] = u
			}
			return fmt.Sprintf("@[%s](%s)", u.DisplayName, u.AccountID)
		})
		if firstErr != nil {
			return "", firstErr
		}
	}
	return strings.Join(lines, "\n"), nil
}

// findUserByName returns the one active person a name refers to: an exact
// display-name match if there is one, else the only search result.
func (c *Client) findUserByName(name string) (UserField, error) {
	data, err := c.do("GET", "/rest/api/3/user/search?maxResults=20&query="+url.QueryEscape(name), nil)
	if err != nil {
		return UserField{}, fmt.Errorf("looking up @[%s]: %w", name, err)
	}
	var found []struct {
		UserField
		AccountType string `json:"accountType"`
		Active      bool   `json:"active"`
	}
	if err := json.Unmarshal(data, &found); err != nil {
		return UserField{}, fmt.Errorf("looking up @[%s]: %w", name, err)
	}
	var people, exact []UserField
	for _, u := range found {
		if !u.Active || (u.AccountType != "" && u.AccountType != "atlassian") {
			continue
		}
		people = append(people, u.UserField)
		if strings.EqualFold(u.DisplayName, name) {
			exact = append(exact, u.UserField)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	switch len(people) {
	case 0:
		return UserField{}, fmt.Errorf("@[%s] matches nobody in Jira — check the name, or write @[Name](accountId)", name)
	case 1:
		return people[0], nil
	}
	names := make([]string, len(people))
	for i, u := range people {
		names[i] = fmt.Sprintf("%s (%s)", u.DisplayName, u.AccountID)
	}
	return UserField{}, fmt.Errorf("@[%s] matches %d people: %s — use the full name, or write @[Name](accountId)",
		name, len(people), strings.Join(names, ", "))
}

// MarkdownToADF converts markdown like the package-level MarkdownToADF, after
// resolving `@[Name]` mentions against this Jira.
func (c *Client) MarkdownToADF(md string) (map[string]interface{}, error) {
	md, err := c.ResolveMentions(md)
	if err != nil {
		return nil, err
	}
	return MarkdownToADF(md), nil
}
