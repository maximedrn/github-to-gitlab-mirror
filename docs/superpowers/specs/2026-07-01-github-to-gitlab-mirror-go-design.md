# GitHub to GitLab Mirror — Go rewrite

## Context

Rewrite the bash script `mirror-repos.sh` into a well-typed Go binary with proper libraries.
The current script synchronizes all GitHub repos (personal + organizations, public + private) to a GitLab group daily via GitHub Actions.

## Architecture

```
.
├── main.go
├── go.mod
├── go.sum
├── internal/
│   ├── github/
│   │   └── client.go         # go-github wrapper: list repos (owner + orgs)
│   ├── gitlab/
│   │   └── client.go         # go-gitlab wrapper: ensure project, set default branch
│   ├── sync/
│   │   └── sync.go           # Business logic: compare refs, decide sync or skip
│   └── mirror/
│       └── mirror.go         # go-git operations: ls-remote, clone --mirror, push --mirror
├── .github/workflows/
│   └── mirror.yml
└── README.md
```

## Dependencies

| Concern | Library |
|---------|---------|
| GitHub API | `go-github` (Google/GitHub official) |
| GitLab API | `go-gitlab` (xanzy, de facto standard) |
| Git operations | `go-git` (native Go, no system dep) |
| HTTP mocking (tests) | `httpmock` |
| Linting | `golangci-lint` |

No shell scripts remain. The workflow calls `go run .` directly.

## Key types

```go
// internal/github/client.go
type Repo struct {
    FullName      string // "martmull/github-to-gitlab-mirror"
    Private       bool
    DefaultBranch string // "main"
}

// internal/sync/sync.go
type SyncResult struct {
    Repo   string
    Status SyncStatus
    Err    error
}

type SyncStatus int
const (
    StatusSkipped SyncStatus = iota  // refs identical
    StatusSynced                     // synced successfully
    StatusFailed                     // error
)
```

## Flow per repo

1. `mirror.GetRefs(githubURL)` vs `mirror.GetRefs(gitlabURL)` → if equal → skip
2. `gitlab.EnsureProject(name, private)` → create project if 404
3. `mirror.MirrorClone(githubURL, tmpDir)` → clone --mirror to temp dir
4. `mirror.MirrorPush(tmpDir, gitlabURL)` → push --mirror
5. `gitlab.SetDefaultBranch(project, defaultBranch)`

## Concurrency

Goroutines in `Syncer`. Number of workers configurable via `WORKERS` env var (default: 5).
There is no GitHub Actions matrix for repo splitting — all parallelism happens in-process.

## Error handling

- Per-repo errors are collected in `SyncResult.Err`, reported in the final summary
- A failed repo does not stop other repos
- Binary exits with code 1 if at least one repo failed (so CI goes red)
- Global timeout via `context.WithTimeout`, default 30 minutes

## Testing

- `internal/mirror` — tests against real `git init --bare` locally, no network
- `internal/github` / `internal/gitlab` — mock HTTP clients via interfaces or `httpmock`
- `internal/sync` — inject interfaces, test decision logic (same refs → skip, different refs → sync, etc.)

## CI Workflow

```yaml
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.24" }
      - run: go fmt ./...
      - uses: golangci/golangci-lint-action@v6

  mirror:
    needs: lint
    runs-on: ubuntu-latest
    timeout-minutes: 180
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.24" }
      - run: go run .
        env:
          GH_PAT: ${{ secrets.GH_PAT }}
          GITLAB_TOKEN: ${{ secrets.GITLAB_TOKEN }}
          GITLAB_GROUP: ${{ vars.GITLAB_GROUP }}
          GITLAB_HOST: ${{ vars.GITLAB_HOST }}
```

Single Go version, single mirror job, concurrency inside Go.

## What gets deleted

- `mirror-repos.sh`
- Any remaining shell scripts
