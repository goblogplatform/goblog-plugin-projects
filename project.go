package main

import (
	"encoding/json"
	"html"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Project is one entry on the Projects page: a GitHub repository or a
// line from the extra_projects setting.
type Project struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Stars       int    `json:"stars"`
	PushedAt    string `json:"pushed_at"` // RFC 3339; GitHub repos only
}

type githubRepo struct {
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Stars       int    `json:"stargazers_count"`
	PushedAt    string `json:"pushed_at"`
	Fork        bool   `json:"fork"`
	Archived    bool   `json:"archived"`
	Private     bool   `json:"private"`
}

// parseReposPage decodes one page of /users/{user}/repos. It returns the
// number of repos on the page (for pagination) and the ones worth listing:
// forks, archived and private repos and names in exclude are dropped.
func parseReposPage(b []byte, exclude map[string]bool) (int, []Project, error) {
	var raw []githubRepo
	if err := json.Unmarshal(b, &raw); err != nil {
		return 0, nil, err
	}
	var out []Project
	for _, r := range raw {
		if r.Fork || r.Archived || r.Private || exclude[strings.ToLower(r.Name)] {
			continue
		}
		out = append(out, Project{Name: r.Name, URL: r.HTMLURL, Description: r.Description, Language: r.Language, Stars: r.Stars, PushedAt: r.PushedAt})
	}
	return len(raw), out, nil
}

// sortProjects orders most-starred first, then most recently pushed, then by name.
func sortProjects(ps []Project) {
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].Stars != ps[j].Stars {
			return ps[i].Stars > ps[j].Stars
		}
		if ps[i].PushedAt != ps[j].PushedAt {
			return ps[i].PushedAt > ps[j].PushedAt
		}
		return ps[i].Name < ps[j].Name
	})
}

// parseExclude turns "a, B ,c" into a case-insensitive set of repo names.
func parseExclude(s string) map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Split(s, ",") {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			m[n] = true
		}
	}
	return m
}

// parseExtraProjects reads the extra_projects setting: one project per
// line as "Name | URL | Description", URL and description optional. Blank
// lines and lines starting with # are skipped.
func parseExtraProjects(s string) []Project {
	var out []Project
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		for len(parts) < 3 {
			parts = append(parts, "")
		}
		p := Project{Name: strings.TrimSpace(parts[0]), URL: strings.TrimSpace(parts[1]), Description: strings.TrimSpace(parts[2])}
		if p.Name != "" {
			out = append(out, p)
		}
	}
	return out
}

// safeHref allows only absolute http(s) URLs into href attributes.
func safeHref(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return html.EscapeString(u.String())
}

// renderProjectsHTML lists projects in the same plain markup the scholar
// plugin uses, so it fits any theme.
func renderProjectsHTML(projects []Project) string {
	if len(projects) == 0 {
		return `<p>No projects found.</p>`
	}
	var b strings.Builder
	for _, p := range projects {
		b.WriteString(`<div style="margin-bottom: 12px; padding-bottom: 12px; border-bottom: 1px solid #eee;">`)
		if href := safeHref(p.URL); href != "" {
			b.WriteString(`<div><a href="` + href + `">` + html.EscapeString(p.Name) + `</a></div>`)
		} else {
			b.WriteString(`<div>` + html.EscapeString(p.Name) + `</div>`)
		}
		if p.Description != "" {
			b.WriteString(`<div style="color: #666; font-size: 13px;">` + html.EscapeString(p.Description) + `</div>`)
		}
		var meta []string
		if p.Language != "" {
			meta = append(meta, html.EscapeString(p.Language))
		}
		if p.Stars > 0 {
			meta = append(meta, "&#9733; "+strconv.Itoa(p.Stars))
		}
		if len(meta) > 0 {
			b.WriteString(`<div style="color: #888; font-size: 13px;">` + strings.Join(meta, " &middot; ") + `</div>`)
		}
		b.WriteString(`</div>`)
	}
	return b.String()
}

// cachedRepos is what the plugin keeps under the store key "repos". User
// records whose repos these are, so changing github_user drops the cache.
type cachedRepos struct {
	User      string    `json:"user"`
	FetchedAt time.Time `json:"fetched_at"`
	Repos     []Project `json:"repos"`
}

func (c cachedRepos) fresh(now time.Time, cacheHours int) bool {
	if c.FetchedAt.IsZero() {
		return false
	}
	return now.Sub(c.FetchedAt) < time.Duration(cacheHours)*time.Hour
}

// failureBackoff is how long render_page avoids a synchronous fetch after
// one has failed, so a GitHub outage does not turn every page view into a
// slow (or, on transport errors, aborted) request.
const failureBackoff = 10 * time.Minute

// shouldFetch decides whether render_page fetches synchronously: only when
// there is no cache at all (a stale cache is served as-is; the job refreshes
// it) and no fetch has failed within failureBackoff.
func shouldFetch(have bool, lastFailure, now time.Time) bool {
	if have {
		return false
	}
	return lastFailure.IsZero() || now.Sub(lastFailure) >= failureBackoff
}

// staleFor decides whether the refresh job fetches: when there is no cache
// or it is older than cacheHours.
func staleFor(cache cachedRepos, have bool, now time.Time, cacheHours int) bool {
	return !have || !cache.fresh(now, cacheHours)
}

// maxRepoLimit caps repo_limit: more than this is never useful on a page
// and bounds the render_page payload.
const maxRepoLimit = 500

// parseIntSetting reads a positive integer setting with a default, capped
// at max when max > 0.
func parseIntSetting(s string, def, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return def
	}
	if max > 0 && n > max {
		return max
	}
	return n
}

const (
	unavailableHTML   = `<div class="alert alert-warning" role="alert">GitHub projects are temporarily unavailable. Please check back later.</div>`
	notConfiguredHTML = `<div class="alert alert-warning" role="alert">No projects configured. Set a GitHub user or add extra projects in the Projects plugin settings.</div>`
)
