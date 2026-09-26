package jira

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseAPIError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name:   "errorMessages",
			status: 400,
			body:   `{"errorMessages":["Issue does not exist or you do not have permission to see it."],"errors":{}}`,
			want:   "jira API error 400: Issue does not exist or you do not have permission to see it.",
		},
		{
			name:   "field errors",
			status: 400,
			body:   `{"errorMessages":[],"errors":{"priority":"Priority is required."}}`,
			want:   "jira API error 400: priority: Priority is required.",
		},
		{
			name:   "both",
			status: 400,
			body:   `{"errorMessages":["Something broke"],"errors":{"summary":"too long"}}`,
			want:   "jira API error 400: Something broke; summary: too long",
		},
		{
			name:   "non-JSON body",
			status: 502,
			body:   "Bad Gateway",
			want:   "jira API error 502: Bad Gateway",
		},
		{
			name:   "empty body",
			status: 503,
			body:   "",
			want:   "jira API error 503: Service Unavailable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAPIError(tc.status, []byte(tc.body)).Error()
			if got != tc.want {
				t.Errorf("parseAPIError(%d, %q)\n  got:  %s\n  want: %s", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

func TestParseAPIErrorTruncatesLongBodies(t *testing.T) {
	long := strings.Repeat("x", 1000)
	got := parseAPIError(500, []byte(long)).Error()
	if len(got) > 400 {
		t.Errorf("expected truncated error, got %d chars", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected truncation marker, got: %s", got[len(got)-20:])
	}
}

func TestSearchPaginates(t *testing.T) {
	var requests []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		requests = append(requests, body)

		page := len(requests)
		// Return a full page, like the real API does when more data exists.
		pageSize := int(body["maxResults"].(float64))
		items := make([]string, pageSize)
		for i := range items {
			items[i] = fmt.Sprintf(`{"key":"PROJ-%d","fields":{"summary":"a","status":{"name":"Done"},"issuetype":{"name":"Task"}}}`, (page-1)*100+i)
		}
		isLast := page >= 3
		next := fmt.Sprintf(`"token-%d"`, page)
		if isLast {
			next = `""`
		}
		fmt.Fprintf(w, `{"issues":[%s],"isLast":%t,"nextPageToken":%s}`, strings.Join(items, ","), isLast, next)
	}))
	defer server.Close()

	c := NewBasicClient(server.URL, "test@example.com", "token")
	result, err := c.Search("project = PROJ", 250)
	if err != nil {
		t.Fatal(err)
	}

	if len(requests) != 3 {
		t.Errorf("expected 3 page requests, got %d", len(requests))
	}
	if len(result.Issues) != 250 {
		t.Errorf("expected 250 accumulated issues, got %d", len(result.Issues))
	}
	if !result.IsLast {
		t.Error("expected IsLast=true after exhausting pages")
	}
	// First page must not send a token; later pages must carry it forward.
	if _, ok := requests[0]["nextPageToken"]; ok {
		t.Error("first request should not include nextPageToken")
	}
	if tok := requests[1]["nextPageToken"]; tok != "token-1" {
		t.Errorf("second request token = %v, want token-1", tok)
	}
	// Page sizes: 100, 100, 50 to honor maxResults=250.
	if size := requests[2]["maxResults"].(float64); size != 50 {
		t.Errorf("third page size = %v, want 50", size)
	}
}

func TestSearchStopsAtMaxResults(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"issues":[{"key":"PROJ-1","fields":{"summary":"a","status":{"name":"Done"},"issuetype":{"name":"Task"}}}],"isLast":false,"nextPageToken":"t"}`)
	}))
	defer server.Close()

	c := NewBasicClient(server.URL, "test@example.com", "token")
	result, err := c.Search("project = PROJ", 1)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("expected 1 request for maxResults=1, got %d", calls)
	}
	if result.IsLast {
		t.Error("expected IsLast=false when more pages exist")
	}
}

func TestDoRetriesOn429(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"errorMessages":["rate limited"]}`)
			return
		}
		fmt.Fprint(w, `{"accountId":"abc","displayName":"Test"}`)
	}))
	defer server.Close()

	c := NewBasicClient(server.URL, "test@example.com", "token")
	me, err := c.GetMyself()
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
	if me.AccountID != "abc" {
		t.Errorf("unexpected result: %+v", me)
	}
}

func TestRetryDelay(t *testing.T) {
	if d := retryDelay("3", 0); d != 3*time.Second {
		t.Errorf("Retry-After 3 → want 3s, got %v", d)
	}
	if d := retryDelay("9999", 0); d != 10*time.Second {
		t.Errorf("Retry-After capped at 10s, got %v", d)
	}
	if d := retryDelay("", 0); d != 500*time.Millisecond {
		t.Errorf("attempt 0 → want 500ms, got %v", d)
	}
	if d := retryDelay("", 1); d != time.Second {
		t.Errorf("attempt 1 → want 1s, got %v", d)
	}
	if d := retryDelay("garbage", 2); d != 2*time.Second {
		t.Errorf("attempt 2 → want 2s, got %v", d)
	}
}

// Several processes (agents) share one profile. With an expired token they
// must refresh once: Atlassian refresh tokens rotate, so reusing the old one
// fails and would log everyone out.
func TestTokenRefreshIsSharedAcrossProcesses(t *testing.T) {
	var mu sync.Mutex
	current := "rt-0"
	refreshes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		if r.Form.Get("refresh_token") != current {
			w.WriteHeader(403)
			w.Write([]byte(`{"error":"unauthorized_client","error_description":"refresh_token is invalid"}`))
			return
		}
		refreshes++
		current = fmt.Sprintf("rt-%d", refreshes)
		fmt.Fprintf(w, `{"access_token":"at-%d","refresh_token":%q,"expires_in":3600}`, refreshes, current)
	}))
	defer srv.Close()
	old := refreshURL
	refreshURL = srv.URL
	defer func() { refreshURL = old }()

	// The "disk": what every process reads and writes, behind the lock.
	var disk struct {
		sync.Mutex
		at, rt string
		exp    time.Time
	}
	disk.at, disk.rt, disk.exp = "at-0", "rt-0", time.Now().Add(-time.Hour)
	var fileLock sync.Mutex

	newProc := func() *Client {
		disk.Lock()
		at, rt, exp := disk.at, disk.rt, disk.exp
		disk.Unlock()
		c := NewOAuthClient("cloud", "https://x", at, rt, "id", "secret", exp, func(at, rt string, exp time.Time) {
			disk.Lock()
			disk.at, disk.rt, disk.exp = at, rt, exp
			disk.Unlock()
		})
		c.SetTokenSync(&TokenSync{
			Lock: func() (func(), error) { fileLock.Lock(); return fileLock.Unlock, nil },
			Reload: func() (string, string, time.Time, error) {
				disk.Lock()
				defer disk.Unlock()
				return disk.at, disk.rt, disk.exp, nil
			},
		})
		return c
	}

	procs := []*Client{newProc(), newProc(), newProc(), newProc()}
	var wg sync.WaitGroup
	errs := make([]error, len(procs))
	for i, c := range procs {
		wg.Add(1)
		go func(i int, c *Client) {
			defer wg.Done()
			errs[i] = c.ensureValidToken()
		}(i, c)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("process %d: %v", i, err)
		}
		if got := procs[i].currentAccessToken(); got != "at-1" {
			t.Errorf("process %d uses %q, want the shared at-1", i, got)
		}
	}
	if refreshes != 1 {
		t.Fatalf("expected exactly one refresh, got %d", refreshes)
	}
}
