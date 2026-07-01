# GitHub to GitLab Mirror

Daily automatic synchronization of all your GitHub repositories (personal and organization memberships, public and private) to a GitLab group. Only repositories that have actually changed since the last run are pushed.

## Compatibility

| OS                 | Status |
| ------------------ | ------ |
| macOS              | ✅      |
| Linux              | ✅      |
| Windows (via WSL2) | ✅      |
| Native Windows     | ✅      |

## Prerequisites

- [Go](https://go.dev)
- [golangci-lint](https://golangci-lint.run) (for linting)
- [golines](https://github.com/segmentio/golines) (for formatting)
- GitHub Personal Access Token (classic) with scopes: `repo`, `read:org`, `workflow`
- GitLab Personal Access Token with scope: `api`
- A GitLab group to host the mirrored repositories

## Setup

### 1. Create a destination GitLab group

Create an empty GitLab **group** (e.g. `github-mirror`) that will host all
mirrored projects. The tool needs an existing group, not a personal namespace.

### 2. Generate access tokens

**GitHub token** — Settings → Developer settings → Personal access tokens →
Tokens (classic), with scopes:

- `repo` (full access to repositories, including private)
- `read:org` (list organization repositories)
- `workflow` (required to push workflow files to this repository)

**GitLab token** — Preferences → Access Tokens, with scope:

- `api`

### 3. Configure secrets and variables

In the GitHub repository **Settings → Secrets and variables → Actions**:

**Secrets** (*Secrets* tab):

| Name | Value |
| ---- | ----- |
| `GH_PAT` | GitHub token from step 2 |
| `GITLAB_TOKEN` | GitLab token from step 2 |

**Variables** (*Variables* tab):

| Name | Value |
| ---- | ----- |
| `GITLAB_GROUP` | Full path of the target GitLab group |
| `GITLAB_HOST` | Optional - GitLab host for self-hosted instances |

### 4. First run

Go to **Actions** → *Mirror GitHub → GitLab* workflow → **Run workflow**, to test before waiting for the next daily trigger.

## Usage

### Build

```bash
go build -o mirror.so .
```

### Run

```bash
GH_PAT=<github-token> GITLAB_TOKEN=<gitlab-token> GITLAB_GROUP=<gitlab-group> go run .
```

Optional environment variables:

| Variable | Default | Description |
| -------- | ------- | ----------- |
| `GITLAB_HOST` | `gitlab.com` | GitLab host for self-hosted instances |
| `WORKERS` | `5` | Number of concurrent go-routines syncing repositories |

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
golines -w -m 100 --base-formatter=gofmt .
```

## License

This project is licensed under the [MIT License](LICENSE).
