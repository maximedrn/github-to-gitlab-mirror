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
	group            gitlab.GroupInfo
	groupErr         error
	createdProjects  map[string]bool
	getErr           map[string]error
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
	if f.createdProjects != nil {
		f.createdProjects[key] = true
	}
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
			"https://github.com/user/repo1.git":      {"refs/heads/main": "abc123"},
			"https://gitlab.com/my-group/repo1.git":   {"refs/heads/main": "abc123"},
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
			"https://github.com/user/repo1.git":      {"refs/heads/main": "abc123", "refs/heads/develop": "def456"},
			"https://gitlab.com/my-group/repo1.git":   {"refs/heads/main": "abc123"},
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
			"https://github.com/user/repo2.git":    {"refs/heads/main": "abc123"},
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
