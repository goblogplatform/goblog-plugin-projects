package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fullPage is pageSize repos, so fetchAll asks for the next page.
func fullPage(prefix string) []byte {
	var rs []string
	for i := 0; i < pageSize; i++ {
		rs = append(rs, fmt.Sprintf(`{"name":"%s%d","stargazers_count":%d}`, prefix, i, i))
	}
	return []byte("[" + strings.Join(rs, ",") + "]")
}

func TestFetchAll(t *testing.T) {
	var calls []string
	get := func(url string, headers map[string]string) (int, []byte, error) {
		calls = append(calls, url)
		if headers["Authorization"] != "Bearer tok" || headers["User-Agent"] == "" {
			t.Errorf("headers = %v", headers)
		}
		if strings.HasSuffix(url, "&page=1") {
			return 200, fullPage("a"), nil
		}
		return 200, []byte(`[{"name":"top","stargazers_count":1000},{"name":"skip","stargazers_count":5000}]`), nil
	}
	ps, err := fetchAll(get, "some user", "tok", parseExclude("skip"), 3)
	if err != nil || len(ps) != 3 {
		t.Fatalf("got %d projects, %v", len(ps), err)
	}
	// limit truncates after every page has been read, so the most-starred
	// repo on page 2 still comes first.
	if ps[0].Name != "top" || ps[1].Name != "a99" {
		t.Errorf("order = %+v", ps)
	}
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "https://api.github.com/users/some%20user/repos?") || !strings.HasSuffix(calls[1], "&page=2") {
		t.Errorf("calls = %v", calls)
	}

	bad := func(string, map[string]string) (int, []byte, error) { return 403, []byte("rate limited"), nil }
	if _, err := fetchAll(bad, "u", "", nil, 5); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("403 should error with status, got %v", err)
	}
	failing := func(string, map[string]string) (int, []byte, error) { return 0, nil, errors.New("net down") }
	if _, err := fetchAll(failing, "u", "", nil, 5); err == nil {
		t.Error("transport error should propagate")
	}
	if _, err := fetchAll(get, "", "", nil, 5); err == nil {
		t.Error("empty user should error")
	}
	noToken := func(_ string, headers map[string]string) (int, []byte, error) {
		if _, ok := headers["Authorization"]; ok {
			t.Errorf("Authorization must be absent without a token: %v", headers)
		}
		return 200, []byte(`[]`), nil
	}
	if ps, err := fetchAll(noToken, "u", "", nil, 5); err != nil || len(ps) != 0 {
		t.Errorf("no repos: %v %v", ps, err)
	}
	// full pages forever stop after maxPages
	n := 0
	forever := func(string, map[string]string) (int, []byte, error) { n++; return 200, fullPage("x"), nil }
	if _, err := fetchAll(forever, "u", "", nil, 1); err != nil || n != maxPages {
		t.Errorf("endless pagination: %d calls, %v", n, err)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("héllo wörld", 5); got != "héllo…" {
		t.Errorf("truncate on rune boundary = %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate no-op = %q", got)
	}
}
