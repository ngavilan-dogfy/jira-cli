package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Profile struct {
	Name string `yaml:"-"`

	// Auth method: "oauth" (default) or "token"
	AuthMethod string `yaml:"auth_method,omitempty"`

	// OAuth fields
	ClientID     string `yaml:"client_id,omitempty"`
	CloudID      string `yaml:"cloud_id,omitempty"`
	AccessToken  string `yaml:"access_token,omitempty"`
	RefreshToken string `yaml:"refresh_token,omitempty"`
	TokenExpiry  string `yaml:"token_expiry,omitempty"`

	// API Token (legacy) fields
	Email string `yaml:"email,omitempty"`
	Token string `yaml:"token,omitempty"`

	// Site
	Domain  string `yaml:"domain,omitempty"`
	SiteURL string `yaml:"site_url,omitempty"`

	// Settings
	Project    string `yaml:"project,omitempty"`
	MaxResults int    `yaml:"max_results,omitempty"`
}

func (p *Profile) APIBaseURL() string {
	if p.AuthMethod == "oauth" && p.CloudID != "" {
		return fmt.Sprintf("https://api.atlassian.com/ex/jira/%s", p.CloudID)
	}
	return fmt.Sprintf("https://%s.atlassian.net", p.Domain)
}

func (p *Profile) BrowseBaseURL() string {
	if p.SiteURL != "" {
		return p.SiteURL
	}
	return fmt.Sprintf("https://%s.atlassian.net", p.Domain)
}

func (p *Profile) IsAuthenticated() bool {
	if p.AuthMethod == "oauth" {
		return p.AccessToken != "" && p.CloudID != ""
	}
	return p.Domain != "" && p.Email != "" && p.Token != ""
}

// --- paths ---

func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "jira-cli")
}

func ProfileDir() string {
	return filepath.Join(Dir(), "profiles")
}

func activePath() string {
	return filepath.Join(Dir(), "active")
}

func profilePath(name string) string {
	return filepath.Join(ProfileDir(), name+".yaml")
}

// --- active profile ---

func ActiveName() string {
	data, err := os.ReadFile(activePath())
	if err != nil {
		return "default"
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		return "default"
	}
	return name
}

func SetActive(name string) error {
	if !Exists(name) {
		return fmt.Errorf("profile %q does not exist", name)
	}
	os.MkdirAll(Dir(), 0700)
	return os.WriteFile(activePath(), []byte(name), 0600)
}

// --- CRUD ---

func Load(name string) (*Profile, error) {
	p := &Profile{Name: name}
	data, err := os.ReadFile(profilePath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("profile %q not found", name)
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, p); err != nil {
		return nil, err
	}
	return p, nil
}

func LoadActive() (*Profile, error) {
	migrateOldConfig()
	name := ActiveName()
	p, err := Load(name)
	if err != nil {
		// No profile on disk: environment variables alone are enough
		// (CI, containers, agents): JIRA_DOMAIN + JIRA_EMAIL + JIRA_TOKEN.
		if envP := envProfile(); envP != nil {
			return envP, nil
		}
		return nil, err
	}

	if v := os.Getenv("JIRA_DOMAIN"); v != "" {
		p.Domain = v
	}
	if v := os.Getenv("JIRA_EMAIL"); v != "" {
		p.Email = v
	}
	if v := os.Getenv("JIRA_TOKEN"); v != "" {
		p.Token = v
	}
	if v := os.Getenv("JIRA_PROJECT"); v != "" {
		p.Project = v
	}

	return p, nil
}

// Save writes the profile atomically (temp file + rename): with several
// agents running jira at once, a reader must never see a half-written file.
func Save(p *Profile) error {
	os.MkdirAll(ProfileDir(), 0700)
	data, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(ProfileDir(), "."+p.Name+"-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), profilePath(p.Name))
}

func Create(name string) (*Profile, error) {
	if Exists(name) {
		return nil, fmt.Errorf("profile %q already exists", name)
	}
	p := &Profile{Name: name, MaxResults: 20, AuthMethod: "oauth"}
	if err := Save(p); err != nil {
		return nil, err
	}
	return p, nil
}

func Delete(name string) error {
	if !Exists(name) {
		return fmt.Errorf("profile %q not found", name)
	}
	if ActiveName() == name {
		return fmt.Errorf("cannot delete active profile %q — switch first", name)
	}
	return os.Remove(profilePath(name))
}

func Exists(name string) bool {
	_, err := os.Stat(profilePath(name))
	return err == nil
}

func List() ([]string, error) {
	entries, err := os.ReadDir(ProfileDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
		}
	}
	return names, nil
}

// --- property access ---

var ValidKeys = []string{"domain", "email", "project", "max_results", "auth_method", "client_id"}

func (p *Profile) Get(key string) (string, error) {
	switch key {
	case "domain":
		return p.Domain, nil
	case "email":
		return p.Email, nil
	case "token":
		if p.Token != "" {
			return "****", nil
		}
		return "", nil
	case "project":
		return p.Project, nil
	case "max_results":
		if p.MaxResults == 0 {
			return "20", nil
		}
		return fmt.Sprintf("%d", p.MaxResults), nil
	case "auth_method":
		if p.AuthMethod == "" {
			return "oauth", nil
		}
		return p.AuthMethod, nil
	case "client_id":
		return p.ClientID, nil
	case "cloud_id":
		return p.CloudID, nil
	case "site_url":
		return p.SiteURL, nil
	default:
		return "", fmt.Errorf("unknown key %q — valid keys: %s", key, strings.Join(ValidKeys, ", "))
	}
}

func (p *Profile) Set(key, value string) error {
	switch key {
	case "domain":
		p.Domain = value
	case "email":
		p.Email = value
	case "project":
		p.Project = value
	case "max_results":
		n := 20
		fmt.Sscanf(value, "%d", &n)
		p.MaxResults = n
	case "auth_method":
		if value != "oauth" && value != "token" {
			return fmt.Errorf("auth_method must be 'oauth' or 'token'")
		}
		p.AuthMethod = value
	case "client_id":
		p.ClientID = value
	case "token", "access_token", "refresh_token":
		return fmt.Errorf("use 'jira setup' to sign in")
	default:
		return fmt.Errorf("unknown key %q — valid keys: %s", key, strings.Join(ValidKeys, ", "))
	}
	return nil
}

// --- global client_id (shared across profiles) ---

func globalClientIDPath() string {
	return filepath.Join(Dir(), "client_id")
}

func GlobalClientID() string {
	data, err := os.ReadFile(globalClientIDPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func SetGlobalClientID(id string) error {
	os.MkdirAll(Dir(), 0700)
	return os.WriteFile(globalClientIDPath(), []byte(id), 0600)
}

func globalClientSecretPath() string {
	return filepath.Join(Dir(), "client_secret")
}

func GlobalClientSecret() string {
	data, err := os.ReadFile(globalClientSecretPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func SetGlobalClientSecret(secret string) error {
	os.MkdirAll(Dir(), 0700)
	return os.WriteFile(globalClientSecretPath(), []byte(secret), 0600)
}

// --- migration ---

func migrateOldConfig() {
	oldPath := filepath.Join(Dir(), "config.yaml")
	if _, err := os.Stat(oldPath); err != nil {
		return
	}
	if _, err := os.ReadDir(ProfileDir()); err == nil {
		entries, _ := os.ReadDir(ProfileDir())
		if len(entries) > 0 {
			os.Remove(oldPath)
			return
		}
	}

	data, err := os.ReadFile(oldPath)
	if err != nil {
		return
	}

	p := &Profile{Name: "default", MaxResults: 20, AuthMethod: "token"}
	if err := yaml.Unmarshal(data, p); err != nil {
		return
	}

	os.MkdirAll(ProfileDir(), 0700)
	Save(p)
	SetActive("default")
	os.Remove(oldPath)
}

// envProfile builds an API-token profile purely from environment
// variables, or returns nil when they're incomplete.
func envProfile() *Profile {
	domain, email, token := os.Getenv("JIRA_DOMAIN"), os.Getenv("JIRA_EMAIL"), os.Getenv("JIRA_TOKEN")
	if domain == "" || email == "" || token == "" {
		return nil
	}
	domain = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://"), "/")
	domain = strings.TrimSuffix(domain, ".atlassian.net")
	return &Profile{
		Name:       "env",
		AuthMethod: "token",
		Domain:     domain,
		Email:      email,
		Token:      token,
		Project:    os.Getenv("JIRA_PROJECT"),
		MaxResults: 20,
	}
}
