//go:build wasip1

// Projects is a goblog WebAssembly plugin: it serves a Projects page
// listing a GitHub user's or organization's public repositories plus any
// projects entered by hand in its settings. Repos are served from the
// plugin store; an hourly job re-fetches once the cache is older than
// cache_hours, and the page only fetches itself when the store is empty.
//
// Build:  GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -ldflags="-s -w" -o plugin.wasm .
// Every export takes JSON on stdin (pdk.Input) and returns JSON (pdk.Output).
// See goblog's README "WebAssembly plugins" for the contract.
package main

import (
	"encoding/json"
	"strings"
	"time"

	pdk "github.com/extism/go-pdk"
)

//go:wasmimport extism:host/user store_get
func hostStoreGet(uint64) uint64

//go:wasmimport extism:host/user store_set
func hostStoreSet(uint64, uint64) uint64

// storeGet returns the value and whether the key exists: the host returns
// offset 0 (Extism's null) for a missing key.
func storeGet(key string) ([]byte, bool) {
	k := pdk.AllocateString(key)
	defer k.Free()
	off := hostStoreGet(k.Offset())
	if off == 0 {
		return nil, false
	}
	return pdk.ParamBytes(off), true
}

func storeSet(key string, value []byte) bool {
	k := pdk.AllocateString(key)
	defer k.Free()
	v := pdk.AllocateBytes(value)
	defer v.Free()
	return hostStoreSet(k.Offset(), v.Offset()) == 0
}

// hookInput is the ctx goblog passes to render_page; only settings and the
// sub-path are needed here.
type hookInput struct {
	Settings map[string]string `json:"settings"`
	Request  struct {
		SubPath string `json:"sub_path"`
	} `json:"request"`
}

type jobInput struct {
	Name     string            `json:"name"`
	Settings map[string]string `json:"settings"`
}

// setting mirrors goblog's settings JSON shape.
type setting struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

func outputJSON(v any) int32 {
	if err := pdk.OutputJSON(v); err != nil {
		pdk.SetErrorString("encode output: " + err.Error())
		return 1
	}
	return 0
}

// pdkGet is the getter used in the wasm build: Extism's http_request,
// limited by the host to allowed_hosts.
func pdkGet(url string, headers map[string]string) (int, []byte, error) {
	req := pdk.NewHTTPRequest(pdk.MethodGet, url)
	for k, v := range headers {
		req.SetHeader(k, v)
	}
	resp := req.Send()
	return int(resp.Status()), resp.Body(), nil
}

//go:wasmexport identity
func identity() int32 {
	return outputJSON(map[string]string{"name": "projects", "display_name": "Projects", "version": "1.0.0"})
}

//go:wasmexport settings
func settings() int32 {
	return outputJSON([]setting{
		{Key: "enabled", Type: "text", Default: "false", Label: "Enabled", Description: "Set to 'true' to enable the Projects page"},
		{Key: "github_user", Type: "text", Default: "", Label: "GitHub User or Organization", Description: "Lists this account's public repositories (forks and archived repos are skipped); leave empty to show only extra projects"},
		{Key: "github_token", Type: "text", Default: "", Label: "GitHub Token", Description: "Optional; raises the API rate limit. Needs no scopes"},
		{Key: "exclude_repos", Type: "text", Default: "", Label: "Exclude Repos", Description: "Comma-separated repository names to hide"},
		{Key: "repo_limit", Type: "text", Default: "30", Label: "Repo Limit", Description: "Maximum number of repositories to show (most-starred first, at most 500)"},
		{Key: "extra_projects", Type: "textarea", Default: "", Label: "Extra Projects", Description: "Shown above the GitHub repos, one per line: Name | URL | Description (URL and description optional)"},
		{Key: "cache_hours", Type: "text", Default: "24", Label: "Cache Hours", Description: "How long fetched repos are reused before the hourly job refreshes them (0 or invalid means the default of 24)"},
	})
}

//go:wasmexport pages
func pages() int32 {
	return outputJSON([]map[string]any{{"page_type": "projects", "title": "Projects", "slug": "projects", "show_in_nav": true, "nav_order": 25, "description": "Projects and GitHub repositories"}})
}

//go:wasmexport jobs
func jobs() int32 {
	return outputJSON([]map[string]any{{"name": "refresh", "interval_seconds": 3600}})
}

// loadCache reads the stored repos for user; ok is false when absent,
// undecodable or fetched for a different user.
func loadCache(user string) (cachedRepos, bool) {
	b, ok := storeGet("repos")
	if !ok {
		return cachedRepos{}, false
	}
	var c cachedRepos
	if err := json.Unmarshal(b, &c); err != nil || !strings.EqualFold(c.User, user) {
		return cachedRepos{}, false
	}
	return c, true
}

// lastFailure is the negative cache: when the most recent fetch failed.
type lastFailure struct {
	FailedAt time.Time `json:"failed_at"`
}

func loadLastFailure() time.Time {
	b, ok := storeGet("last_failure")
	if !ok {
		return time.Time{}
	}
	var f lastFailure
	if err := json.Unmarshal(b, &f); err != nil {
		return time.Time{}
	}
	return f.FailedAt
}

// fetchAndStore fetches the user's repos and replaces the cache. On failure
// it logs, records last_failure and returns false. A transport-level failure
// (DNS, refused connection, TLS) never returns at all: Extism's http_request
// aborts the guest call, which is why render_page only fetches when it has
// nothing to show.
func fetchAndStore(settings map[string]string) ([]Project, bool) {
	user := strings.TrimSpace(settings["github_user"])
	limit := parseIntSetting(settings["repo_limit"], 30, maxRepoLimit)
	repos, err := fetchAll(pdkGet, user, strings.TrimSpace(settings["github_token"]), parseExclude(settings["exclude_repos"]), limit)
	if err != nil {
		pdk.Log(pdk.LogWarn, "github fetch failed: "+err.Error())
		b, _ := json.Marshal(lastFailure{FailedAt: time.Now()})
		storeSet("last_failure", b)
		return nil, false
	}
	b, _ := json.Marshal(cachedRepos{User: user, FetchedAt: time.Now(), Repos: repos})
	if !storeSet("repos", b) {
		pdk.Log(pdk.LogWarn, "could not store the repos cache")
	}
	return repos, true
}

// loadOrFetch is render_page's strategy: serve whatever is cached, fresh or
// stale, and fetch synchronously only when there is no cache at all and no
// fetch failed in the last failureBackoff.
func loadOrFetch(settings map[string]string) ([]Project, bool) {
	cache, have := loadCache(strings.TrimSpace(settings["github_user"]))
	if have {
		return cache.Repos, true
	}
	if !shouldFetch(have, loadLastFailure(), time.Now()) {
		return nil, false
	}
	return fetchAndStore(settings)
}

// refreshIfStale is the job's strategy: re-fetch once the cache is older
// than cache_hours (or missing). It has the 120 s job budget and its
// failures are only logged, so it is where slow or failing fetches belong.
func refreshIfStale(settings map[string]string) {
	hours := parseIntSetting(settings["cache_hours"], 24, 0)
	cache, have := loadCache(strings.TrimSpace(settings["github_user"]))
	if staleFor(cache, have, time.Now(), hours) {
		fetchAndStore(settings)
	}
}

//go:wasmexport render_page
func renderPage() int32 {
	var in hookInput
	if err := json.Unmarshal(pdk.Input(), &in); err != nil {
		pdk.SetErrorString("render_page: " + err.Error())
		return 1
	}
	if in.Request.SubPath != "" {
		return outputJSON(map[string]any{}) // no sub-pages: goblog 404s
	}
	projects := parseExtraProjects(in.Settings["extra_projects"])
	hasUser := strings.TrimSpace(in.Settings["github_user"]) != ""
	if !hasUser && len(projects) == 0 {
		return outputJSON(map[string]any{"html": notConfiguredHTML})
	}
	var notice string
	if hasUser {
		repos, ok := loadOrFetch(in.Settings)
		if ok {
			projects = append(projects, repos...)
		} else {
			notice = unavailableHTML
		}
	}
	if notice != "" && len(projects) == 0 {
		return outputJSON(map[string]any{"html": notice})
	}
	return outputJSON(map[string]any{"html": notice + renderProjectsHTML(projects)})
}

//go:wasmexport run_job
func runJob() int32 {
	var in jobInput
	if err := json.Unmarshal(pdk.Input(), &in); err != nil {
		pdk.SetErrorString("run_job: " + err.Error())
		return 1
	}
	if in.Name == "refresh" && strings.TrimSpace(in.Settings["github_user"]) != "" {
		refreshIfStale(in.Settings)
	}
	return outputJSON(map[string]any{})
}

func main() {}
