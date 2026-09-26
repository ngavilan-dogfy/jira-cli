package config

import (
	"os"
	"testing"
	"time"
)

func TestLockProfileExcludesOtherHolders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	unlock, err := LockProfile("p", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockProfile("p", 100*time.Millisecond); err == nil {
		t.Fatal("a second holder must wait for the first")
	}
	unlock()
	again, err := LockProfile("p", time.Second)
	if err != nil {
		t.Fatalf("lock should be free after release: %v", err)
	}
	again()
}

func TestSaveIsAtomicAndRoundTrips(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := &Profile{Name: "p", Project: "PROJ", AccessToken: "a", TokenExpiry: "2030-01-01T00:00:00Z"}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load("p")
	if err != nil || got.AccessToken != "a" || got.Project != "PROJ" {
		t.Fatalf("got %+v, %v", got, err)
	}
	entries, _ := os.ReadDir(ProfileDir())
	for _, e := range entries {
		if e.Name() != "p.yaml" {
			t.Errorf("leftover file %s", e.Name())
		}
	}
}

func TestEnvOnlyProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JIRA_DOMAIN", "https://acme.atlassian.net/")
	t.Setenv("JIRA_EMAIL", "me@acme.com")
	t.Setenv("JIRA_TOKEN", "tok")
	t.Setenv("JIRA_PROJECT", "OPS")
	p, err := LoadActive()
	if err != nil {
		t.Fatal(err)
	}
	if p.Domain != "acme" || !p.IsAuthenticated() || p.Project != "OPS" || p.AuthMethod != "token" {
		t.Fatalf("got %+v", p)
	}
	t.Setenv("JIRA_TOKEN", "")
	if _, err := LoadActive(); err == nil {
		t.Fatal("incomplete env and no profile should fail")
	}
}
