# goblog-plugin-projects

A [goblog](https://github.com/goblogplatform/goblog) WebAssembly plugin that adds a **Projects** page (`/projects`, in the nav) listing your public GitHub repositories (name linked to the repo, description, language and stars, most-starred first) plus any projects you enter by hand.

## Install

From your goblog's **Admin → Plugins → Browse**, search for *Projects* and click **Install** (goblog 0.3.0 or newer). Or download `plugin.wasm` from the [latest release](https://github.com/goblogplatform/goblog-plugin-projects/releases/latest) into `plugins/wasm/projects.wasm`, write `plugins/wasm/projects.json` containing `{"allowed_hosts":["api.github.com"]}`, and restart goblog.

## Settings

Under **Admin → Settings → Projects**:

| Setting | Default | Meaning |
|---|---|---|
| `enabled` | `false` | Set to `true` to enable the Projects page |
| `github_user` | | GitHub user or organization whose public repositories are listed; forks and archived repos are skipped. Leave empty to show only `extra_projects` |
| `github_token` | | Optional; a token (no scopes needed) raises the API rate limit from 60 to 5,000 requests an hour |
| `exclude_repos` | | Comma-separated repository names to hide (case-insensitive) |
| `repo_limit` | `30` | Maximum number of repositories to show (the most-starred N; at most 500) |
| `extra_projects` | | Projects shown above the repos, one per line: `Name \| URL \| Description`. URL and description are optional; blank lines and lines starting with `#` are skipped |
| `cache_hours` | `24` | How long fetched repos are reused before the hourly job refreshes them (`0` or anything invalid means the default) |

For example:

```
goblog.live | https://www.goblog.live | The site this plugin was built for
Thesis | https://example.com/thesis.pdf
```

## How it works

The plugin talks to `api.github.com` only (`/users/{user}/repos`, which also works for organizations), and goblog's sandbox blocks every other host.

Repos are served from the cache: fetched repos are kept in the plugin store (goblog's `plugin_store` table) and the page always shows what is there, so visitors never wait on the API. An hourly `refresh` job re-fetches once the cache is older than `cache_hours`. If GitHub is unreachable the last cached list stays up and the job simply tries again next hour. The page only fetches for itself when the store is empty (first view after install, or after changing `github_user`); if that fetch fails the page shows the extra projects with a note that GitHub projects are temporarily unavailable, and does not retry for ten minutes. Changes to `exclude_repos` and `repo_limit` apply when the cache is next refreshed (within `cache_hours`). `extra_projects` needs no network and changes show up immediately.

## Build it yourself

```bash
go test ./...   # unit tests run natively
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -ldflags="-s -w" -o plugin.wasm .
```

Needs Go 1.25 or newer (`go.mod` pins the toolchain, so `GOTOOLCHAIN=auto` fetches it). Check the result with `goblog validate-plugin plugin.wasm`. Tag a release as `vX.Y.Z`; the workflow builds and uploads `plugin.wasm` for you.

## License

Apache-2.0.
