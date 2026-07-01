package sync_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maximedrn/github-to-gitlab-mirror/internal/github"
	"github.com/maximedrn/github-to-gitlab-mirror/internal/gitlab"
	"github.com/maximedrn/github-to-gitlab-mirror/internal/sync"
)

// fakeGitHub is a stub implementation of sync.GitHubClient that returns
// a fixed list of repositories or a fixed error.
type fakeGitHub struct {
	repositories []github.Repository
	listError    error
}

// ListRepositories returns either the stubbed error or the stubbed
// repositories.
func (fake *fakeGitHub) ListRepositories(
	requestContext context.Context,
) ([]github.Repository, error) {
	if fake.listError != nil {
		return nil, fake.listError
	}
	return fake.repositories, nil
}

// fakeGitLab is a stub implementation of sync.GitLabClient that records
// created projects and exposes hooks to force errors.
type fakeGitLab struct {
	group              gitlab.GroupInfo
	resolveGroupError  error
	createdProjects    map[string]bool
	ensureProjectError map[string]error
}

// ResolveGroup returns either the stubbed error or the stubbed group.
func (fake *fakeGitLab) ResolveGroup(
	requestContext context.Context, path string,
) (gitlab.GroupInfo, error) {
	if fake.resolveGroupError != nil {
		return gitlab.GroupInfo{}, fake.resolveGroupError
	}
	return fake.group, nil
}

// EnsureProject records that the project was created unless a matching
// stubbed error is registered for the "group/name" key.
func (fake *fakeGitLab) EnsureProject(
	requestContext context.Context,
	group gitlab.GroupInfo,
	name string,
	private bool,
) error {
	var key string = group.FullPath + "/" + name
	var registeredError error
	var exists bool
	registeredError, exists = fake.ensureProjectError[key]
	if exists {
		return registeredError
	}
	if fake.createdProjects != nil {
		fake.createdProjects[key] = true
	}
	return nil
}

// SetDefaultBranch is a no-op used only to satisfy the interface.
func (fake *fakeGitLab) SetDefaultBranch(
	requestContext context.Context, projectPath, branch string,
) error {
	return nil
}

// fakeMirror is a stub implementation of sync.MirrorClient that returns
// pre-registered ref maps and no-op clone/push operations.
type fakeMirror struct {
	referencesByURL map[string]map[string]string
}

// GetRefs returns the pre-registered ref map for the given URL, or an
// empty map when no map is registered.
func (fake *fakeMirror) GetRefs(
	requestContext context.Context, remoteURL, user, token string,
) (map[string]string, error) {
	var references map[string]string
	var exists bool
	references, exists = fake.referencesByURL[remoteURL]
	if exists {
		return references, nil
	}
	return map[string]string{}, nil
}

// MirrorClone is a no-op used only to satisfy the interface.
func (fake *fakeMirror) MirrorClone(
	requestContext context.Context,
	remoteURL, user, token, destinationDirectory string,
) error {
	return nil
}

// MirrorPush is a no-op used only to satisfy the interface.
func (fake *fakeMirror) MirrorPush(
	requestContext context.Context,
	repositoryDirectory, remoteURL, user, token string,
) error {
	return nil
}

// TestSync_SkipsWhenRefsIdentical verifies that the syncer reports
// StatusSkipped and does not create the destination project when the
// GitHub and GitLab remotes advertise identical refs.
func TestSync_SkipsWhenRefsIdentical(test *testing.T) {
	var requestContext context.Context = context.Background()

	var githubStub *fakeGitHub = &fakeGitHub{
		repositories: []github.Repository{
			{
				FullName:      "user/repo1",
				Private:       false,
				DefaultBranch: "main",
			},
		},
	}

	var gitlabStub *fakeGitLab = &fakeGitLab{
		group: gitlab.GroupInfo{
			ID:       42,
			FullPath: "my-group",
		},
		createdProjects: make(map[string]bool),
	}

	var mirrorStub *fakeMirror = &fakeMirror{
		referencesByURL: map[string]map[string]string{
			"https://github.com/user/repo1.git": {
				"refs/heads/main": "abc123",
			},
			"https://gitlab.com/my-group/repo1.git": {
				"refs/heads/main": "abc123",
			},
		},
	}

	var syncer *sync.Syncer = sync.New(
		githubStub,
		gitlabStub,
		mirrorStub,
		2,
	)
	var results []sync.SyncResult = syncer.Sync(
		requestContext,
		"my-group",
		"x-access-token",
		"gl-token",
		"gitlab.com",
	)

	if len(results) != 1 {
		test.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Status != sync.StatusSkipped {
		test.Errorf("Expected StatusSkipped, got %v", results[0].Status)
	}
	if len(gitlabStub.createdProjects) > 0 {
		test.Error("Expected no projects to be created")
	}
}

// TestSync_SyncsWhenRefsDiffer verifies that the syncer reports
// StatusSynced and creates the destination project when the GitHub and
// GitLab remotes advertise different refs.
func TestSync_SyncsWhenRefsDiffer(test *testing.T) {
	var requestContext context.Context = context.Background()

	var githubStub *fakeGitHub = &fakeGitHub{
		repositories: []github.Repository{
			{
				FullName:      "user/repository-1",
				Private:       true,
				DefaultBranch: "develop",
			},
		},
	}

	var gitlabStub *fakeGitLab = &fakeGitLab{
		group: gitlab.GroupInfo{
			ID:       42,
			FullPath: "my-group",
		},
		createdProjects: make(map[string]bool),
	}

	var mirrorStub *fakeMirror = &fakeMirror{
		referencesByURL: map[string]map[string]string{
			"https://github.com/user/repository-1.git": {
				"refs/heads/main":    "abc123",
				"refs/heads/develop": "def456",
			},
			"https://gitlab.com/my-group/repository-1.git": {
				"refs/heads/main": "abc123",
			},
		},
	}

	var syncer *sync.Syncer = sync.New(
		githubStub,
		gitlabStub,
		mirrorStub,
		2,
	)
	var results []sync.SyncResult = syncer.Sync(
		requestContext,
		"my-group",
		"gh-token",
		"gl-token",
		"gitlab.com",
	)

	if len(results) != 1 {
		test.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Status != sync.StatusSynced {
		test.Errorf("Expected StatusSynced, got %v", results[0].Status)
	}
	if !gitlabStub.createdProjects["my-group/repository-1"] {
		test.Error("Expected repository-1 to be created")
	}
}

// TestSync_CollectsFailures verifies that a failure for one repository
// does not prevent successful synchronization of the other repositories
// and that every outcome is reported.
func TestSync_CollectsFailures(test *testing.T) {
	var requestContext context.Context = context.Background()

	var githubStub *fakeGitHub = &fakeGitHub{
		repositories: []github.Repository{
			{
				FullName:      "user/repository-1",
				Private:       false,
				DefaultBranch: "main",
			},
			{
				FullName:      "user/repository-2",
				Private:       false,
				DefaultBranch: "main",
			},
		},
	}

	var gitlabStub *fakeGitLab = &fakeGitLab{
		group: gitlab.GroupInfo{
			ID:       42,
			FullPath: "my-group",
		},
		createdProjects: make(map[string]bool),
		ensureProjectError: map[string]error{
			"my-group/repository-2": errors.New("Permission denied"),
		},
	}

	var mirrorStub *fakeMirror = &fakeMirror{
		referencesByURL: map[string]map[string]string{
			"https://github.com/user/repository-1.git": {
				"refs/heads/main": "abc123",
			},
			"https://gitlab.com/my-group/repository-1.git": {},
			"https://github.com/user/repository-2.git": {
				"refs/heads/main": "abc123",
			},
		},
	}

	var syncer *sync.Syncer = sync.New(
		githubStub,
		gitlabStub,
		mirrorStub,
		2,
	)
	var results []sync.SyncResult = syncer.Sync(
		requestContext,
		"my-group",
		"gh-token",
		"gl-token",
		"gitlab.com",
	)

	if len(results) != 2 {
		test.Fatalf("Expected 2 results, got %d", len(results))
	}

	var syncedCount, failedCount int
	var result sync.SyncResult
	for _, result = range results {
		if result.Status == sync.StatusSynced {
			syncedCount++
		}
		if result.Status == sync.StatusFailed {
			failedCount++
		}
	}
	if syncedCount != 1 {
		test.Errorf("Expected 1 synced, got %d", syncedCount)
	}
	if failedCount != 1 {
		test.Errorf("Expected 1 failed, got %d", failedCount)
	}
}

// TestSync_GitHubError verifies that an error returned by ListRepositories is
// surfaced as a single StatusFailed SyncResult.
func TestSync_GitHubError(test *testing.T) {
	var requestContext context.Context = context.Background()

	var githubStub *fakeGitHub = &fakeGitHub{
		listError: errors.New("Network error"),
	}
	var gitlabStub *fakeGitLab = &fakeGitLab{
		group: gitlab.GroupInfo{
			ID:       42,
			FullPath: "my-group",
		},
	}
	var mirrorStub *fakeMirror = &fakeMirror{}

	var syncer *sync.Syncer = sync.New(
		githubStub,
		gitlabStub,
		mirrorStub,
		2,
	)
	var results []sync.SyncResult = syncer.Sync(
		requestContext,
		"my-group",
		"gh",
		"gl",
		"gitlab.com",
	)

	if len(results) != 1 {
		test.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Status != sync.StatusFailed {
		test.Errorf("Expected StatusFailed, got %v", results[0].Status)
	}
	if !strings.Contains(results[0].Err.Error(), "Network error") {
		test.Errorf(
			"Expected error to contain 'Network error', got %v",
			results[0].Err,
		)
	}
}
