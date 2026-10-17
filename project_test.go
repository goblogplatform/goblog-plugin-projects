package main

import (
	"strings"
	"testing"
	"time"
)

const reposJSON = `[
 {"name":"tool","html_url":"https://github.com/u/tool","description":"A <tool> & more","language":"Go","stargazers_count":12,"pushed_at":"2026-01-01T00:00:00Z"},
 {"name":"forked","html_url":"https://github.com/u/forked","fork":true,"stargazers_count":99},
 {"name":"old","html_url":"https://github.com/u/old","archived":true,"stargazers_count":99},
 {"name":"Hidden","html_url":"https://github.com/u/Hidden","stargazers_count":99},
 {"name":"bad","html_url":"javascript:alert(1)","description":null,"language":null}
]`

func TestParseReposPage(t *testing.T) {
	n, ps, err := parseReposPage([]byte(reposJSON), parseExclude(" hidden ,"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || len(ps) != 2 {
		t.Fatalf("n=%d projects=%+v", n, ps)
	}
	p := ps[0]
	if p.Name != "tool" || p.URL != "https://github.com/u/tool" || p.Language != "Go" || p.Stars != 12 || p.PushedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("project = %+v", p)
	}
	if _, _, err := parseReposPage([]byte("{"), nil); err == nil {
		t.Error("bad JSON should error")
	}
}

func TestSortProjects(t *testing.T) {
	ps := []Project{
		{Name: "b", Stars: 1, PushedAt: "2020"},
		{Name: "c", Stars: 5},
		{Name: "a", Stars: 1, PushedAt: "2021"},
		{Name: "d", Stars: 1, PushedAt: "2021"},
	}
	sortProjects(ps)
	if got := ps[0].Name + ps[1].Name + ps[2].Name + ps[3].Name; got != "cadb" {
		t.Errorf("order = %s, want cadb (stars desc, pushed desc, name asc)", got)
	}
}

func TestParseExtraProjects(t *testing.T) {
	ps := parseExtraProjects("# comment\n\n  Site | https://example.com | My site | with pipe \nJust a name\n | https://nameless.example\r\nTwo | https://two.example\n")
	if len(ps) != 3 {
		t.Fatalf("got %+v", ps)
	}
	if ps[0] != (Project{Name: "Site", URL: "https://example.com", Description: "My site | with pipe"}) {
		t.Errorf("full line = %+v", ps[0])
	}
	if ps[1] != (Project{Name: "Just a name"}) {
		t.Errorf("name only = %+v", ps[1])
	}
	if ps[2] != (Project{Name: "Two", URL: "https://two.example"}) {
		t.Errorf("CRLF/no description = %+v", ps[2])
	}
}

func TestRenderProjectsHTML(t *testing.T) {
	if got := renderProjectsHTML(nil); got != "<p>No projects found.</p>" {
		t.Errorf("empty = %q", got)
	}
	_, ps, _ := parseReposPage([]byte(reposJSON), parseExclude("hidden"))
	out := renderProjectsHTML(ps)
	for _, want := range []string{`href="https://github.com/u/tool"`, "A &lt;tool&gt; &amp; more", "Go &middot; &#9733; 12", "<div>bad</div>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "javascript:") {
		t.Errorf("unsafe href rendered:\n%s", out)
	}
}

func TestCacheDecisions(t *testing.T) {
	now := time.Now()
	if !shouldFetch(false, time.Time{}, now) || shouldFetch(true, time.Time{}, now) {
		t.Error("fetch only without a cache")
	}
	if shouldFetch(false, now.Add(-time.Minute), now) || !shouldFetch(false, now.Add(-failureBackoff), now) {
		t.Error("failure backoff not honoured")
	}
	c := cachedRepos{FetchedAt: now.Add(-2 * time.Hour)}
	if staleFor(c, true, now, 24) || !staleFor(c, true, now, 1) || !staleFor(c, false, now, 24) || !staleFor(cachedRepos{}, true, now, 24) {
		t.Error("staleFor wrong")
	}
	if parseIntSetting("", 30, 500) != 30 || parseIntSetting("0", 30, 500) != 30 || parseIntSetting("9999", 30, 500) != 500 || parseIntSetting(" 7 ", 30, 0) != 7 {
		t.Error("parseIntSetting wrong")
	}
}
