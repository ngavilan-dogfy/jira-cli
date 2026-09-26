package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// issueTemplate represents a markdown file with optional YAML-ish frontmatter.
//
// Format:
//
//	---
//	type: Bug
//	summary: Default summary if no positional arg is passed
//	labels: incident, observability
//	priority: High
//	---
//	## Heading
//	... markdown body ...
type issueTemplate struct {
	Type     string
	Summary  string
	Labels   []string
	Priority string
	Body     string
}

func templatesDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "jira-cli", "templates")
}

// loadTemplateIfRequested returns nil when --template isn't set; otherwise
// reads, parses and returns the template (or an error).
func loadTemplateIfRequested() (*issueTemplate, error) {
	name := strings.TrimSpace(createTemplate)
	if name == "" {
		return nil, nil
	}
	// Accept "incident", "incident.md", or absolute path
	path := name
	if !filepath.IsAbs(path) && !strings.Contains(path, "/") {
		if !strings.HasSuffix(path, ".md") {
			path += ".md"
		}
		path = filepath.Join(templatesDir(), path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("template not found: %s (looked in %s): %w", name, path, err)
	}
	return parseTemplate(string(raw))
}

func parseTemplate(raw string) (*issueTemplate, error) {
	tpl := &issueTemplate{}
	body := raw

	// Frontmatter: optional, delimited by --- lines at the very start.
	if strings.HasPrefix(raw, "---\n") || strings.HasPrefix(raw, "---\r\n") {
		// Find the closing ---
		rest := strings.TrimPrefix(strings.TrimPrefix(raw, "---\r\n"), "---\n")
		end := strings.Index(rest, "\n---")
		if end >= 0 {
			fm := rest[:end]
			body = strings.TrimLeft(rest[end+len("\n---"):], "\r\n")
			for _, line := range strings.Split(fm, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				key, val, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				key = strings.TrimSpace(strings.ToLower(key))
				val = strings.TrimSpace(val)
				switch key {
				case "type":
					tpl.Type = val
				case "summary":
					tpl.Summary = val
				case "priority":
					tpl.Priority = val
				case "labels":
					for _, l := range strings.Split(val, ",") {
						l = strings.TrimSpace(l)
						if l != "" {
							tpl.Labels = append(tpl.Labels, l)
						}
					}
				}
			}
		}
	}
	tpl.Body = body
	return tpl, nil
}
