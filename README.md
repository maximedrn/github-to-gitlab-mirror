# GitHub to GitLab Mirror

Daily automatic synchronization of all your GitHub repositories (personal
and organization memberships, public and private) to a GitLab group. Only
repositories that have actually changed since the last run are pushed.
Written in **Go**.

## Compatibility

| OS                 | Status |
| ------------------ | ------ |
| macOS              | ✅      |
| Linux              | ✅      |
| Windows (via WSL2) | ✅      |
| Native Windows     | ✅      |

## Prerequisites

- [Go](https://go.dev) 1.24+
- [golangci-lint](https://golangci-lint.run) (for linting)
- GitHub Personal Access Token (classic) with scopes: `repo`, `read:org`, `workflow`
- GitLab Personal Access Token with scope: `api`
- A GitLab group to host the mirrored repositories

## Setup

### 1. Create this repository

Create a new GitHub repository (preferably private, since it contains the
automation configuration) and add this project's files to it.

### 2. Create a destination GitLab group

Create an empty GitLab **group** (e.g. `github-mirror`) that will host all
mirrored projects. The tool needs an existing group, not a personal namespace.

### 3. Generate access tokens

**GitHub token** — Settings → Developer settings → Personal access tokens →
Tokens (classic), with scopes:

- `repo` (full access to repositories, including private)
- `read:org` (list organization repositories)
- `workflow` (required to push workflow files to this repository)

**GitLab token** — Preferences → Access Tokens, with scope:

- `api`

### 4. Configure secrets and variables

In the GitHub repository **Settings → Secrets and variables → Actions**:

**Secrets** (*Secrets* tab):

| Name | Value |
| ---- | ----- |
| `GH_PAT` | GitHub token from step 3 |
| `GITLAB_TOKEN` | GitLab token from step 3 |

**Variables** (*Variables* tab):

| Name | Value |
| ---- | ----- |
| `GITLAB_GROUP` | Full path of the target GitLab group (e.g. `my-group` or `my-group/subgroup`) |
| `GITLAB_HOST` | Optional — GitLab host for self-hosted instances (defaults to `gitlab.com`) |

### 5. First run

Go to **Actions** → *Mirror GitHub → GitLab* workflow → **Run workflow**,
to test before waiting for the next daily trigger.

## Usage

### Build

```bash
go build -o mirror .
```

### Run

```bash
GH_PAT=<github-token> GITLAB_TOKEN=<gitlab-token> GITLAB_GROUP=my-group go run .
```

Optional environment variables:

| Variable | Default | Description |
| -------- | ------- | ----------- |
| `GITLAB_HOST` | `gitlab.com` | GitLab host for self-hosted instances |
| `WORKERS` | `5` | Number of concurrent goroutines syncing repos |

### Test

```bash
go test ./...
```

### Lint

```bash
golangci-lint run ./...
```

### Format

```bash
go fmt ./...
```

## How it works

The GitHub Actions workflow (`.github/workflows/mirror-to-gitlab.yml`) runs
the Go binary daily at 03:00 UTC (and on demand from the **Actions** tab).
The binary:

1. Lists all repositories accessible to the token (owner + organization
   member), public and private, via the GitHub API.
2. For each repository, compares Git refs between GitHub and GitLab — if
   they are identical, the repository is skipped without cloning or
   transferring.
3. Otherwise: creates the GitLab project if it doesn't exist yet (with the
   same visibility as on GitHub), performs a mirror clone and mirror push
   (all branches, tags, and history), and aligns the default branch.
4. Displays a summary (synced / skipped / failed) and exits with code 1 if
   any repository failed, so it's visible in Actions.

> **The GitLab repository becomes an exact copy of GitHub.** Never modify
> code directly on GitLab: any change will be overwritten (or deleted) on
> the next sync.

## Important: workflow disabling due to inactivity

GitHub automatically disables scheduled workflows (`schedule`) for public
repositories that have been inactive for 60 days (the same behavior is
regularly reported for private repositories). Since this repository may
never receive commits on its own, the workflow could silently stop after
two months. Two options:

- Periodically check the **Actions** tab to ensure it is still *Enabled*,
  and click **Enable workflow** if needed.
- Add an occasional commit (e.g. to this README), which resets the
  inactivity counter.

## Going further

- **Concurrency**: the number of worker goroutines syncing in parallel is
  configurable via the `WORKERS` variable (default: 5).
- **Timeout**: configurable in `main.go` (default 30 minutes).
- **Notifications**: add a `if: failure()` step to post to Slack or email
  on failure.
- **Exclude forks**: add a `!r.GetFork()` filter in
  `internal/github/client.go`.

## License

This project is licensed under the [MIT License](LICENSE).
