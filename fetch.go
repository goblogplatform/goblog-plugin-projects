package main

import (
	"errors"
	"fmt"
	"net/url"
)

const (
	apiBase  = "https://api.github.com"
	pageSize = 100
)

// getter performs an HTTP GET; the wasm build uses the PDK, tests inject a fake.
type getter func(url string, headers map[string]string) (status int, body []byte, err error)

// maxPages bounds pagination; 10 pages x 100 repos is far beyond any repo_limit.
const maxPages = 10

// fetchAll pages through a user's (or organization's) public repos, drops
// the ones parseReposPage filters, sorts them, and keeps the first limit.
// All pages are read before truncating so "limit" means the top N overall.
func fetchAll(get getter, user, token string, exclude map[string]bool, limit int) ([]Project, error) {
	if user == "" {
		return nil, errors.New("github user is empty")
	}
	// GitHub rejects requests without a User-Agent.
	headers := map[string]string{"Accept": "application/vnd.github+json", "User-Agent": "goblog-plugin-projects"}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	var out []Project
	for page := 1; page <= maxPages; page++ {
		u := fmt.Sprintf("%s/users/%s/repos?type=owner&per_page=%d&page=%d", apiBase, url.PathEscape(user), pageSize, page)
		status, body, err := get(u, headers)
		if err != nil {
			return nil, err
		}
		if status != 200 {
			return nil, fmt.Errorf("github returned HTTP %d: %s", status, truncate(string(body), 200))
		}
		n, ps, err := parseReposPage(body, exclude)
		if err != nil {
			return nil, fmt.Errorf("decode github response: %w", err)
		}
		out = append(out, ps...)
		if n < pageSize {
			break
		}
	}
	sortProjects(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// truncate cuts s to at most n runes.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
