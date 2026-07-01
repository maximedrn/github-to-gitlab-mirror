# GitHub to GitLab Mirror — Go rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite `mirror-repos.sh` as a concurrent Go binary using go-github, go-gitlab, and go-git.

**Architecture:** `main.go` parses env vars and creates the syncer. `internal/github`, `internal/gitlab`, and `internal/mirror` each wrap one external dependency. `internal/sync` orchestrates the flow: list repos → resolve group → for each repo in goroutines: compare refs, skip if identical, otherwise clone mirror + push mirror + set default branch.

**Tech Stack:** Go 1.24, go-github v72, go-gitlab v0.127, go-git v5, golangci-lint v2

---

### Task 1: Initialize Go module and project structure

**Files:**
- Create: `go.mod`
- Create: `internal/mirror/doc.go`
- Create: `internal/github/doc.go`
- Create: `internal/gitlab/doc.go`
- Create: `internal/sync/doc.go`

- [ ] **Step 1: Initialize go module**

```bash
cd /Users/maxime/Personnel/Programmation/github-to-gitlab-mirror
go mod init github.com/martmull/github-to-gitlab-mirror
```

- [ ] **Step 2: Create directory structure**

```bash
mkdir -p internal/mirror internal/github internal/gitlab internal/sync
```

- [ ] **Step 3: Add dependencies**

```bash
go get github.com/google/go-github/v72
go get github.com/xanzy/go-gitlab
go get github.com/go-git/go-git/v5
go get github.com/go-git/go-git/v5/plumbing/transport/http
```

- [ ] **Step 4: Verify go.mod has all dependencies**

Run: `go mod tidy`
Expected: no errors, go.mod lists go-github, go-gitlab, go-git

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/
git commit -m "feat: initialize Go module and project structure"
```

---

### Task 2: Create internal/mirror — go-git wrapper

**Files:**
- Create: `internal/mirror/mirror.go`
- Create: `internal/mirror/mirror_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/mirror/mirror_test.go`:

```go
package mirror_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/martmull/github-to-gitlab-mirror/internal/mirror"
)

func TestGetRefs_LocalBareRepo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Create a bare repo with one commit
	bare := filepath.Join(dir, "source.git")
	runGit(t, dir, "init", "--bare", bare)

	// Clone the bare repo, make a commit, push
	tmp := filepath.Join(dir, "tmp")
	runGit(t, dir, "clone", bare, tmp)
	runGit(t, tmp, "config", "user.email", "test@test.com")
	runGit(t, tmp, "config", "user.name", "Test")
	writeFile(t, filepath.Join(tmp, "README.md"), "# hello")
	runGit(t, tmp, "add", "README.md")
	runGit(t, tmp, "commit", "-m", "initial")
	runGit(t, tmp, "push", "origin", "main")

	client := mirror.New()
	refs, err := client.GetRefs(ctx, "file://"+bare, "", "")
	if err != nil {
		t.Fatalf("GetRefs: %v", err)
	}

	if _, ok := refs["refs/heads/main"]; !ok {
		t.Errorf("expected refs/heads/main to exist, got %v", refs)
	}
}

func TestMirrorClonePush(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Source bare repo
	src := filepath.Join(dir, "source.git")
	runGit(t, dir, "init", "--bare", src)

	// Clone, commit, push
	tmp := filepath.Join(dir, "tmp")
	runGit(t, dir, "clone", src, tmp)
	runGit(t, tmp, "config", "user.email", "test@test.com")
	runGit(t, tmp, "config", "user.name", "Test")
	writeFile(t, filepath.Join(tmp, "README.md"), "# hello")
	runGit(t, tmp, "add", "README.md")
	runGit(t, tmp, "commit", "-m", "initial")
	runGit(t, tmp, "push", "origin", "main")

	// Destination bare repo
	dst := filepath.Join(dir, "dest.git")
	runGit(t, dir, "init", "--bare", dst)

	// Mirror clone
	client := mirror.New()
	cloneDir := filepath.Join(dir, "clone.git")
	if err := client.MirrorClone(ctx, "file://"+src, "", "", cloneDir); err != nil {
		t.Fatalf("MirrorClone: %v", err)
	}

	// Mirror push
	if err := client.MirrorPush(ctx, cloneDir, "file://"+dst, "", ""); err != nil {
		t.Fatalf("MirrorPush: %v", err)
	}

	// Verify dst has the ref
	refs, err := client.GetRefs(ctx, "file://"+dst, "", "")
	if err != nil {
		t.Fatalf("GetRefs on dst: %v", err)
	}
	if _, ok := refs["refs/heads/main"]; !ok {
		t.Errorf("expected refs/heads/main in dst, got %v", refs)
	}
}

func runGit(t *testing.T, workDir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/mirror/...
```
Expected: compilation error (package mirror not yet implemented)

- [ ] **Step 3: Write the implementation**

Create `internal/mirror/mirror.go`:

```go
package mirror

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

type Client struct{}

func New() *Client {
	return &Client{}
}

func (c *Client) GetRefs(ctx context.Context, url, user, token string) (map[string]string, error) {
	ep, err := transport.NewEndpoint(url)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	ep.User = user

	r := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})

	var auth transport.AuthMethod
	if user != "" {
		auth = &http.BasicAuth{Username: user, Password: token}
	}

	refs, err := r.ListContext(ctx, &git.ListOptions{Auth: auth})
	if err != nil {
		return nil, fmt.Errorf("list remote: %w", err)
	}

	result := make(map[string]string, len(refs))
	for _, ref := range refs {
		result[ref.Name().String()] = ref.Hash().String()
	}
	return result, nil
}

func (c *Client) MirrorClone(ctx context.Context, url, user, token, dest string) error {
	var auth transport.AuthMethod
	if user != "" {
		auth = &http.BasicAuth{Username: user, Password: token}
	}

	_, err := git.PlainCloneContext(ctx, dest, true, &git.CloneOptions{
		URL:  url,
		Auth: auth,
	})
	if err != nil {
		return fmt.Errorf("mirror clone: %w", err)
	}
	return nil
}

func (c *Client) MirrorPush(ctx context.Context, repoDir, url, user, token string) error {
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	remote, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "mirror-target",
		URLs: []string{url},
	})
	if err != nil {
		existing, err2 := repo.Remote("mirror-target")
		if err2 != nil {
			return fmt.Errorf("create remote: %w", err)
		}
		remote = existing
	}

	var auth transport.AuthMethod
	if user != "" {
		auth = &http.BasicAuth{Username: user, Password: token}
	}

	err = remote.PushContext(ctx, &git.PushOptions{
		Auth:     auth,
		RefSpecs: []config.RefSpec{"+refs/*:refs/*"},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("mirror push: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/mirror/... -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mirror/mirror.go internal/mirror/mirror_test.go
git commit -m "feat: add mirror package with go-git operations"
```

---

### Task 3: Create internal/github — go-github wrapper

**Files:**
- Create: `internal/github/client.go`
- Create: `internal/github/client_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/github/client_test.go`:

```go
package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gh "github.com/google/go-github/v72/github"
	"github.com/martmull/github-to-gitlab-mirror/internal/github"
)

func TestListRepos_ReturnsAllPages(t *testing.T) {
	ctx := context.Background()
	page := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("affiliation") != "owner,organization_member" {
			t.Errorf("expected affiliation=owner,organization_member, got %s", r.URL.Query().Get("affiliation"))
		}

		page++
		repos := []*gh.Repository{}
		if page == 1 {
			repos = append(repos, &gh.Repository{
				FullName:      gh.Ptr("user/repo1"),
				Private:       gh.Ptr(false),
				DefaultBranch: gh.Ptr("main"),
			})
			repos = append(repos, &gh.Repository{
				FullName:      gh.Ptr("org/repo2"),
				Private:       gh.Ptr(true),
				DefaultBranch: gh.Ptr("master"),
			})

			w.Header().Set("Link", `<`+srv.URL+`/user/repos?page=2>; rel="next"`)
		} else {
			repos = append(repos, &gh.Repository{
				FullName:      gh.Ptr("user/repo3"),
				Private:       gh.Ptr(false),
				DefaultBranch: gh.Ptr("main"),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(repos)
	}))
	defer srv.Close()

	client := github.NewClientWithURL("test-token", srv.URL)
	repos, err := client.ListRepos(ctx)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}

	if len(repos) != 3 {
		t.Fatalf("expected 3 repos, got %d", len(repos))
	}
	if repos[0].FullName != "user/repo1" {
		t.Errorf("expected user/repo1, got %s", repos[0].FullName)
	}
	if repos[1].Private != true {
		t.Errorf("expected repo2 to be private")
	}
	if repos[2].FullName != "user/repo3" {
		t.Errorf("expected user/repo3, got %s", repos[2].FullName)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/github/...
```
Expected: compilation error (package not yet implemented)

- [ ] **Step 3: Write the implementation**

Create `internal/github/client.go`:

```go
package github

import (
	"context"
	"fmt"
	"net/url"

	gh "github.com/google/go-github/v72/github"
	"golang.org/x/oauth2"
)

type Repo struct {
	FullName      string
	Private       bool
	DefaultBranch string
}

type Client struct {
	client *gh.Client
}

func NewClient(token string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	return &Client{client: gh.NewClient(tc)}
}

func NewClientWithURL(token, baseURL string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	client := gh.NewClient(tc)
	client.BaseURL, _ = url.Parse(baseURL + "/")
	return &Client{client: client}
}

func (c *Client) ListRepos(ctx context.Context) ([]Repo, error) {
	var all []Repo
	opts := &gh.RepositoryListByAuthenticatedUserOptions{
		Affiliation: "owner,organization_member",
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := c.client.Repositories.ListByAuthenticatedUser(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("list repos: %w", err)
		}

		for _, r := range repos {
			branch := "main"
			if r.GetDefaultBranch() != "" {
				branch = r.GetDefaultBranch()
			}
			all = append(all, Repo{
				FullName:      r.GetFullName(),
				Private:       r.GetPrivate(),
				DefaultBranch: branch,
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return all, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/github/... -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/github/client.go internal/github/client_test.go
git commit -m "feat: add github package with ListRepos"
```

---

### Task 4: Create internal/gitlab — go-gitlab wrapper

**Files:**
- Create: `internal/gitlab/client.go`
- Create: `internal/gitlab/client_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/gitlab/client_test.go`:

```go
package gitlab_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/martmull/github-to-gitlab-mirror/internal/gitlab"
)

func TestResolveGroup(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/groups/my-group%2Fsubgroup" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":        42,
				"full_path": "my-group/subgroup",
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client := gitlab.NewClientWithURL("test-token", srv.URL)
	info, err := client.ResolveGroup(ctx, "my-group/subgroup")
	if err != nil {
		t.Fatalf("ResolveGroup: %v", err)
	}
	if info.ID != 42 {
		t.Errorf("expected ID 42, got %d", info.ID)
	}
	if info.FullPath != "my-group/subgroup" {
		t.Errorf("expected FullPath my-group/subgroup, got %s", info.FullPath)
	}
}

func TestEnsureProject_CreatesWhenNotFound(t *testing.T) {
	ctx := context.Background()
	created := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/v4/projects/my-group%2Fmy-repo" {
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/v4/projects" {
			created = true
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]interface{}{"id": 1, "name": "my-repo"})
			return
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()

	client := gitlab.NewClientWithURL("test-token", srv.URL)
	group := gitlab.GroupInfo{ID: 42, FullPath: "my-group"}
	err := client.EnsureProject(ctx, group, "my-repo", true)
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if !created {
		t.Error("expected project to be created")
	}
}

func TestEnsureProject_SkipsWhenExists(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/v4/projects/my-group%2Fmy-repo" {
			json.NewEncoder(w).Encode(map[string]interface{}{"id": 1, "name": "my-repo"})
			return
		}
		if r.Method == "POST" {
			t.Error("should not have called POST when project exists")
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()

	client := gitlab.NewClientWithURL("test-token", srv.URL)
	group := gitlab.GroupInfo{ID: 42, FullPath: "my-group"}
	err := client.EnsureProject(ctx, group, "my-repo", false)
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
}

func TestSetDefaultBranch(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && r.URL.Path == "/api/v4/projects/my-group%2Fmy-repo" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":             1,
				"default_branch": "develop",
			})
			return
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()

	client := gitlab.NewClientWithURL("test-token", srv.URL)
	err := client.SetDefaultBranch(ctx, "my-group/my-repo", "develop")
	if err != nil {
		t.Fatalf("SetDefaultBranch: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/gitlab/...
```
Expected: compilation error

- [ ] **Step 3: Write the implementation**

Create `internal/gitlab/client.go`:

```go
package gitlab

import (
	"context"
	"fmt"

	gl "github.com/xanzy/go-gitlab"
)

type GroupInfo struct {
	ID       int
	FullPath string
}

type Client struct {
	client *gl.Client
}

func NewClient(host, token string) (*Client, error) {
	baseURL := "https://" + host
	if host == "gitlab.com" {
		baseURL = "https://gitlab.com"
	}
	client, err := gl.NewClient(token, gl.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("create gitlab client: %w", err)
	}
	return &Client{client: client}, nil
}

func NewClientWithURL(token, baseURL string) *Client {
	client, _ := gl.NewClient(token, gl.WithBaseURL(baseURL))
	return &Client{client: client}
}

func (c *Client) ResolveGroup(ctx context.Context, groupPath string) (GroupInfo, error) {
	g, _, err := c.client.Groups.GetGroup(groupPath, nil, gl.WithContext(ctx))
	if err != nil {
		return GroupInfo{}, fmt.Errorf("resolve group %q: %w", groupPath, err)
	}
	return GroupInfo{ID: g.ID, FullPath: g.FullPath}, nil
}

func (c *Client) EnsureProject(ctx context.Context, group GroupInfo, name string, private bool) error {
	fullPath := group.FullPath + "/" + name

	_, resp, err := c.client.Projects.GetProject(fullPath, nil, gl.WithContext(ctx))
	if err == nil {
		return nil
	}
	if resp != nil && resp.StatusCode != 404 {
		return fmt.Errorf("get project %q: %w", fullPath, err)
	}

	visibility := gl.PublicVisibility
	if private {
		visibility = gl.PrivateVisibility
	}

	_, _, err = c.client.Projects.CreateProject(&gl.CreateProjectOptions{
		Name:        gl.Ptr(name),
		NamespaceID: gl.Ptr(group.ID),
		Visibility:  gl.Ptr(visibility),
	}, gl.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("create project %q: %w", fullPath, err)
	}
	return nil
}

func (c *Client) SetDefaultBranch(ctx context.Context, projectPath, branch string) error {
	_, _, err := c.client.Projects.EditProject(projectPath, &gl.EditProjectOptions{
		DefaultBranch: gl.Ptr(branch),
	}, gl.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("set default branch for %q: %w", projectPath, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gitlab/... -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gitlab/client.go internal/gitlab/client_test.go
git commit -m "feat: add gitlab package with ResolveGroup, EnsureProject, SetDefaultBranch"
```

---

### Task 5: Create internal/sync — business logic with worker pool

**Files:**
- Create: `internal/sync/sync.go`
- Create: `internal/sync/sync_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/sync/sync_test.go`:

```go
package sync_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/martmull/github-to-gitlab-mirror/internal/github"
	"github.com/martmull/github-to-gitlab-mirror/internal/gitlab"
	"github.com/martmull/github-to-gitlab-mirror/internal/sync"
)

type fakeGitHub struct {
	repos []github.Repo
	err   error
}

func (f *fakeGitHub) ListRepos(ctx context.Context) ([]github.Repo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.repos, nil
}

type fakeGitLab struct {
	group          gitlab.GroupInfo
	groupErr       error
	createdProjects map[string]bool
	getErr         map[string]error
}

func (f *fakeGitLab) ResolveGroup(ctx context.Context, path string) (gitlab.GroupInfo, error) {
	if f.groupErr != nil {
		return gitlab.GroupInfo{}, f.groupErr
	}
	return f.group, nil
}

func (f *fakeGitLab) EnsureProject(ctx context.Context, group gitlab.GroupInfo, name string, private bool) error {
	key := group.FullPath + "/" + name
	if err, ok := f.getErr[key]; ok {
		return err
	}
	f.createdProjects[key] = true
	return nil
}

func (f *fakeGitLab) SetDefaultBranch(ctx context.Context, projectPath, branch string) error {
	return nil
}

type fakeMirror struct {
	refs map[string]map[string]string // url -> refs
}

func (f *fakeMirror) GetRefs(ctx context.Context, url, user, token string) (map[string]string, error) {
	if refs, ok := f.refs[url]; ok {
		return refs, nil
	}
	return map[string]string{}, nil
}

func (f *fakeMirror) MirrorClone(ctx context.Context, url, user, token, dest string) error {
	return nil
}

func (f *fakeMirror) MirrorPush(ctx context.Context, repoDir, url, user, token string) error {
	return nil
}

func TestSync_SkipsWhenRefsIdentical(t *testing.T) {
	ctx := context.Background()

	gh := &fakeGitHub{
		repos: []github.Repo{
			{FullName: "user/repo1", Private: false, DefaultBranch: "main"},
		},
	}

	gl := &fakeGitLab{
		group:           gitlab.GroupInfo{ID: 42, FullPath: "my-group"},
		createdProjects: make(map[string]bool),
	}

	mirror := &fakeMirror{
		refs: map[string]map[string]string{
			"https://github.com/user/repo1.git":       {"refs/heads/main": "abc123"},
			"https://gitlab.com/my-group/repo1.git":    {"refs/heads/main": "abc123"},
		},
	}

	syncer := sync.New(gh, gl, mirror, 2)
	results := syncer.Sync(ctx, "my-group", "x-access-token", "gl-token", "gitlab.com")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != sync.StatusSkipped {
		t.Errorf("expected StatusSkipped, got %v", results[0].Status)
	}
	if len(gl.createdProjects) > 0 {
		t.Error("no projects should have been created")
	}
}

func TestSync_SyncsWhenRefsDiffer(t *testing.T) {
	ctx := context.Background()

	gh := &fakeGitHub{
		repos: []github.Repo{
			{FullName: "user/repo1", Private: true, DefaultBranch: "develop"},
		},
	}

	gl := &fakeGitLab{
		group:           gitlab.GroupInfo{ID: 42, FullPath: "my-group"},
		createdProjects: make(map[string]bool),
	}

	mirror := &fakeMirror{
		refs: map[string]map[string]string{
			"https://github.com/user/repo1.git":       {"refs/heads/main": "abc123", "refs/heads/develop": "def456"},
			"https://gitlab.com/my-group/repo1.git":    {"refs/heads/main": "abc123"},
		},
	}

	syncer := sync.New(gh, gl, mirror, 2)
	results := syncer.Sync(ctx, "my-group", "gh-token", "gl-token", "gitlab.com")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != sync.StatusSynced {
		t.Errorf("expected StatusSynced, got %v", results[0].Status)
	}
	if !gl.createdProjects["my-group/repo1"] {
		t.Error("expected repo1 to be created")
	}
}

func TestSync_CollectsFailures(t *testing.T) {
	ctx := context.Background()

	gh := &fakeGitHub{
		repos: []github.Repo{
			{FullName: "user/repo1", Private: false, DefaultBranch: "main"},
			{FullName: "user/repo2", Private: false, DefaultBranch: "main"},
		},
	}

	gl := &fakeGitLab{
		group:           gitlab.GroupInfo{ID: 42, FullPath: "my-group"},
		createdProjects: make(map[string]bool),
		getErr: map[string]error{
			"my-group/repo2": errors.New("permission denied"),
		},
	}

	mirror := &fakeMirror{
		refs: map[string]map[string]string{
			"https://github.com/user/repo1.git":    {"refs/heads/main": "abc123"},
			"https://gitlab.com/my-group/repo1.git": {},
		},
	}

	syncer := sync.New(gh, gl, mirror, 2)
	results := syncer.Sync(ctx, "my-group", "gh-token", "gl-token", "gitlab.com")

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	var synced, failed int
	for _, r := range results {
		if r.Status == sync.StatusSynced {
			synced++
		}
		if r.Status == sync.StatusFailed {
			failed++
		}
	}
	if synced != 1 {
		t.Errorf("expected 1 synced, got %d", synced)
	}
	if failed != 1 {
		t.Errorf("expected 1 failed, got %d", failed)
	}
}

func TestSync_GitHubError(t *testing.T) {
	ctx := context.Background()

	gh := &fakeGitHub{err: errors.New("network error")}
	gl := &fakeGitLab{group: gitlab.GroupInfo{ID: 42, FullPath: "my-group"}}
	mirror := &fakeMirror{}

	syncer := sync.New(gh, gl, mirror, 2)
	results := syncer.Sync(ctx, "my-group", "gh", "gl", "gitlab.com")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != sync.StatusFailed {
		t.Errorf("expected StatusFailed, got %v", results[0].Status)
	}
	if !strings.Contains(results[0].Err.Error(), "network error") {
		t.Errorf("expected error to contain 'network error', got %v", results[0].Err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/sync/...
```
Expected: compilation error

- [ ] **Step 3: Write the implementation**

Create `internal/sync/sync.go`:

```go
package sync

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/martmull/github-to-gitlab-mirror/internal/github"
	"github.com/martmull/github-to-gitlab-mirror/internal/gitlab"
)

type SyncStatus int

const (
	StatusSkipped SyncStatus = iota
	StatusSynced
	StatusFailed
)

type SyncResult struct {
	Repo   string
	Status SyncStatus
	Err    error
}

type GitHubClient interface {
	ListRepos(ctx context.Context) ([]github.Repo, error)
}

type GitLabClient interface {
	ResolveGroup(ctx context.Context, groupPath string) (gitlab.GroupInfo, error)
	EnsureProject(ctx context.Context, group gitlab.GroupInfo, name string, private bool) error
	SetDefaultBranch(ctx context.Context, projectPath, branch string) error
}

type MirrorClient interface {
	GetRefs(ctx context.Context, url, user, token string) (map[string]string, error)
	MirrorClone(ctx context.Context, url, user, token, dest string) error
	MirrorPush(ctx context.Context, repoDir, url, user, token string) error
}

type Syncer struct {
	github  GitHubClient
	gitlab  GitLabClient
	mirror  MirrorClient
	workers int
}

func New(gh GitHubClient, gl GitLabClient, m MirrorClient, workers int) *Syncer {
	if workers < 1 {
		workers = 5
	}
	return &Syncer{github: gh, gitlab: gl, mirror: m, workers: workers}
}

func (s *Syncer) Sync(ctx context.Context, groupPath, ghToken, glToken, glHost string) []SyncResult {
	repos, err := s.github.ListRepos(ctx)
	if err != nil {
		return []SyncResult{{Repo: "", Status: StatusFailed, Err: fmt.Errorf("list repos: %w", err)}}
	}

	group, err := s.gitlab.ResolveGroup(ctx, groupPath)
	if err != nil {
		return []SyncResult{{Repo: "", Status: StatusFailed, Err: fmt.Errorf("resolve group: %w", err)}}
	}

	repoCh := make(chan github.Repo, len(repos))
	resultCh := make(chan SyncResult, len(repos))

	var wg sync.WaitGroup
	for i := 0; i < s.workers; i++ {
		wg.Add(1)
		go s.worker(ctx, &wg, group, ghToken, glToken, glHost, repoCh, resultCh)
	}

	for _, r := range repos {
		repoCh <- r
	}
	close(repoCh)

	wg.Wait()
	close(resultCh)

	var results []SyncResult
	for r := range resultCh {
		results = append(results, r)
	}
	return results
}

func (s *Syncer) worker(ctx context.Context, wg *sync.WaitGroup, group gitlab.GroupInfo, ghToken, glToken, glHost string, repos <-chan github.Repo, results chan<- SyncResult) {
	defer wg.Done()

	for repo := range repos {
		result := s.syncRepo(ctx, group, repo, ghToken, glToken, glHost)
		results <- result
	}
}

func (s *Syncer) syncRepo(ctx context.Context, group gitlab.GroupInfo, repo github.Repo, ghToken, glToken, glHost string) SyncResult {
	ghURL := fmt.Sprintf("https://github.com/%s.git", repo.FullName)
	projectPath := group.FullPath + "/" + repoName(repo.FullName)
	glURL := fmt.Sprintf("https://%s/%s.git", glHost, projectPath)

	ghRefs, err := s.mirror.GetRefs(ctx, ghURL, "x-access-token", ghToken)
	if err != nil {
		return SyncResult{Repo: repo.FullName, Status: StatusFailed, Err: fmt.Errorf("get gh refs: %w", err)}
	}

	glRefs, err := s.mirror.GetRefs(ctx, glURL, "oauth2", glToken)
	if err != nil {
		glRefs = map[string]string{}
	}

	if refsEqual(ghRefs, glRefs) {
		return SyncResult{Repo: repo.FullName, Status: StatusSkipped}
	}

	name := repoName(repo.FullName)
	if err := s.gitlab.EnsureProject(ctx, group, name, repo.Private); err != nil {
		return SyncResult{Repo: repo.FullName, Status: StatusFailed, Err: fmt.Errorf("ensure project: %w", err)}
	}

	tmpDir, err := os.MkdirTemp("", "mirror-*")
	if err != nil {
		return SyncResult{Repo: repo.FullName, Status: StatusFailed, Err: fmt.Errorf("temp dir: %w", err)}
	}
	defer os.RemoveAll(tmpDir)

	cloneDir := tmpDir + "/repo.git"
	if err := s.mirror.MirrorClone(ctx, ghURL, "x-access-token", ghToken, cloneDir); err != nil {
		return SyncResult{Repo: repo.FullName, Status: StatusFailed, Err: fmt.Errorf("mirror clone: %w", err)}
	}

	if err := s.mirror.MirrorPush(ctx, cloneDir, glURL, "oauth2", glToken); err != nil {
		return SyncResult{Repo: repo.FullName, Status: StatusFailed, Err: fmt.Errorf("mirror push: %w", err)}
	}

	if err := s.gitlab.SetDefaultBranch(ctx, projectPath, repo.DefaultBranch); err != nil {
		return SyncResult{Repo: repo.FullName, Status: StatusFailed, Err: fmt.Errorf("set default branch: %w", err)}
	}

	return SyncResult{Repo: repo.FullName, Status: StatusSynced}
}

func repoName(fullName string) string {
	for i := len(fullName) - 1; i >= 0; i-- {
		if fullName[i] == '/' {
			return fullName[i+1:]
		}
	}
	return fullName
}

func refsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || va != vb {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/sync/... -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sync/sync.go internal/sync/sync_test.go
git commit -m "feat: add sync package with worker pool and ref comparison"
```

---

### Task 6: Create main.go — entry point

**Files:**
- Create: `main.go`

- [ ] **Step 1: Write main.go**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/martmull/github-to-gitlab-mirror/internal/github"
	"github.com/martmull/github-to-gitlab-mirror/internal/gitlab"
	"github.com/martmull/github-to-gitlab-mirror/internal/mirror"
	"github.com/martmull/github-to-gitlab-mirror/internal/sync"
)

func main() {
	ghPAT := requireEnv("GH_PAT")
	glToken := requireEnv("GITLAB_TOKEN")
	glGroup := requireEnv("GITLAB_GROUP")
	glHost := os.Getenv("GITLAB_HOST")
	if glHost == "" {
		glHost = "gitlab.com"
	}

	workers := 5
	if w := os.Getenv("WORKERS"); w != "" {
		var err error
		workers, err = strconv.Atoi(w)
		if err != nil || workers < 1 {
			log.Fatalf("WORKERS must be a positive integer, got %q", w)
		}
	}

	ghClient := github.NewClient(ghPAT)

	glClient, err := gitlab.NewClient(glHost, glToken)
	if err != nil {
		log.Fatalf("create gitlab client: %v", err)
	}

	mirrorClient := mirror.New()
	syncer := sync.New(ghClient, glClient, mirrorClient, workers)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	results := syncer.Sync(ctx, glGroup, ghPAT, glToken, glHost)

	var synced, skipped, failed int
	for _, r := range results {
		switch r.Status {
		case sync.StatusSynced:
			synced++
			fmt.Printf("  %s — synced\n", r.Repo)
		case sync.StatusSkipped:
			skipped++
			fmt.Printf("  %s — skipped (no changes)\n", r.Repo)
		case sync.StatusFailed:
			failed++
			fmt.Printf("  %s — FAILED: %v\n", r.Repo, r.Err)
		}
	}

	fmt.Println("────────────────────────────────────")
	fmt.Printf("Synced: %d | Skipped: %d | Failed: %d\n", synced, skipped, failed)

	if failed > 0 {
		os.Exit(1)
	}
}

func requireEnv(name string) string {
	val := os.Getenv(name)
	if val == "" {
		log.Fatalf("%s is required", name)
	}
	return val
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat: add main.go entry point with env parsing and summary"
```

---

### Task 7: Update CI workflow

**Files:**
- Modify: `.github/workflows/mirror-to-gitlab.yml`

- [ ] **Step 1: Rewrite the workflow**

Replace `.github/workflows/mirror-to-gitlab.yml` content:

```yaml
name: Mirror GitHub to GitLab

on:
  schedule:
    - cron: '0 3 * * *'
  workflow_dispatch:

permissions:
  contents: read

concurrency:
  group: mirror-sync
  cancel-in-progress: false

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.24"
      - run: go fmt ./...
      - uses: golangci/golangci-lint-action@v6

  mirror:
    needs: lint
    runs-on: ubuntu-latest
    timeout-minutes: 180
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.24"
      - run: go run .
        env:
          GH_PAT: ${{ secrets.GH_PAT }}
          GITLAB_TOKEN: ${{ secrets.GITLAB_TOKEN }}
          GITLAB_GROUP: ${{ vars.GITLAB_GROUP }}
          GITLAB_HOST: ${{ vars.GITLAB_HOST }}
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/mirror-to-gitlab.yml
git commit -m "feat: update CI workflow for Go with lint and mirror jobs"
```

---

### Task 8: Add golangci-lint config

**Files:**
- Create: `.golangci.yml`

- [ ] **Step 1: Create .golangci.yml**

```yaml
linters:
  enable:
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
    - gofmt
    - goimports
linters-settings:
  gofmt:
    simplify: true
  goimports:
    local-prefixes: github.com/martmull/github-to-gitlab-mirror
run:
  timeout: 3m
```

- [ ] **Step 2: Run lint to verify**

```bash
golangci-lint run ./...
```
Expected: no issues

- [ ] **Step 3: Commit**

```bash
git add .golangci.yml
git commit -m "chore: add golangci-lint configuration"
```

---

### Task 9: Cleanup — delete shell scripts, update README

**Files:**
- Delete: `mirror-repos.sh`
- Modify: `README.md`

- [ ] **Step 1: Delete the shell script**

```bash
git rm mirror-repos.sh
```

- [ ] **Step 2: Update README.md**

Replace the "Fonctionnement" section and code reference. In README.md, change:

```
`.github/workflows/mirror-to-gitlab.yml` déclenche `scripts/mirror-repos.sh`
```

To:

```
`.github/workflows/mirror-to-gitlab.yml` exécute le binaire Go (`go run .`)
```

Also update any references to `bash` or shell scripting to mention Go.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "chore: delete shell script, update README for Go rewrite"
```

---

### Task 10: Final verification

- [ ] **Step 1: Run all tests**

```bash
go test ./... -v
```
Expected: all tests PASS

- [ ] **Step 2: Run go vet**

```bash
go vet ./...
```
Expected: no issues

- [ ] **Step 3: Verify build**

```bash
go build -o /dev/null ./...
```
Expected: no errors

- [ ] **Step 4: Commit any remaining changes**

```bash
git add -A
git diff --cached --stat
git commit -m "chore: final verification — all tests pass, go vet clean"
```
