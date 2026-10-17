# Changelog

## 1.0.0

- First release: a Projects page (`/projects`) listing a GitHub user's or organization's public repositories (forks and archived repos skipped, most-starred first) plus hand-entered projects from the `extra_projects` setting. Repos are cached in the plugin store and refreshed by an hourly job once older than `cache_hours`.
