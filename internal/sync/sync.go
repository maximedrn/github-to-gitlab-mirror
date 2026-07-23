package sync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/maximedrn/github-to-gitlab-mirror/internal/github"
	"github.com/maximedrn/github-to-gitlab-mirror/internal/gitlab"
)

// SyncStatus describes the outcome of synchronizing a single repository.
type SyncStatus int

const (
	// StatusSkipped indicates that the source and destination refs were
	// identical, so no mirror clone or push was performed.
	StatusSkipped SyncStatus = iota
	// StatusSynced indicates that the repository was successfully cloned
	// from GitHub and pushed to GitLab.
	StatusSynced
	// StatusFailed indicates that the repository could not be synchronized
	// and that the error is available in SyncResult.Err.
	StatusFailed
)

// SyncResult reports the outcome of synchronizing a single repository.
// Err is non-nil only when Status is StatusFailed.
type SyncResult struct {
	Repo   string
	Status SyncStatus
	Err    error
}

// GitHubClient is the subset of the GitHub client behavior consumed by
// the synchronizer. It exists so that tests can substitute a fake.
type GitHubClient interface {
	ListRepositories(
		requestContext context.Context,
	) ([]github.Repository, error)
}

// GitLabClient is the subset of the GitLab client behavior consumed by
// the synchronizer. It exists so that tests can substitute a fake.
type GitLabClient interface {
	ResolveGroup(
		requestContext context.Context,
		groupPath string,
	) (gitlab.GroupInfo, error)
	EnsureProject(
		requestContext context.Context,
		group gitlab.GroupInfo,
		name string,
		private bool,
	) error
	SetDefaultBranch(
		requestContext context.Context,
		projectPath, branch string,
	) error
}

// MirrorClient is the subset of the mirror client behavior consumed by
// the synchronizer. It exists so that tests can substitute a fake.
type MirrorClient interface {
	GetRefs(
		requestContext context.Context,
		remoteURL, user, token string,
	) (map[string]string, error)
	MirrorClone(
		requestContext context.Context,
		remoteURL, user, token, destinationDirectory string,
	) error
	MirrorLFS(
		requestContext context.Context,
		repositoryDirectory,
		sourceURL, sourceUser, sourceToken,
		targetURL, targetUser, targetToken string,
	) error
	MirrorPush(
		requestContext context.Context,
		repositoryDirectory, remoteURL, user, token string,
	) error
}

// Syncer orchestrates the mirroring of every GitHub repository owned or
// accessible by the authenticated user to the target GitLab group using
// a fixed worker pool.
type Syncer struct {
	githubClient GitHubClient
	gitlabClient GitLabClient
	mirrorClient MirrorClient
	workers      int
}

// New returns a Syncer configured with the provided clients and a worker
// pool of workers goroutines. When workers is less than 1 the pool falls
// back to five goroutines.
func New(
	githubClient GitHubClient,
	gitlabClient GitLabClient,
	mirrorClient MirrorClient,
	workers int,
) *Syncer {
	if workers < 1 {
		workers = 5
	}
	return &Syncer{
		githubClient: githubClient,
		gitlabClient: gitlabClient,
		mirrorClient: mirrorClient,
		workers:      workers,
	}
}

// Sync lists every GitHub repository, resolves the target GitLab group,
// then dispatches every repository to a worker pool that mirrors each
// one. The returned slice contains exactly one SyncResult per repository
// that was processed. When listing the repositories or resolving the
// group fails a single SyncResult carrying StatusFailed and an empty
// Repo is returned.
func (syncer *Syncer) Sync(
	requestContext context.Context,
	groupPath, githubToken, gitlabToken, gitlabHost string,
) []SyncResult {
	var repositories []github.Repository
	var listError error
	repositories, listError = syncer.githubClient.ListRepositories(
		requestContext,
	)
	if listError != nil {
		return []SyncResult{{
			Repo:   "",
			Status: StatusFailed,
			Err:    fmt.Errorf("list repos: %w", listError),
		}}
	}

	var group gitlab.GroupInfo
	var resolveError error
	group, resolveError = syncer.gitlabClient.ResolveGroup(
		requestContext,
		groupPath,
	)
	if resolveError != nil {
		return []SyncResult{{
			Repo:   "",
			Status: StatusFailed,
			Err:    fmt.Errorf("resolve group: %w", resolveError),
		}}
	}

	var repositoryChannel chan github.Repository = make(
		chan github.Repository,
		len(repositories),
	)
	var resultChannel chan SyncResult = make(
		chan SyncResult,
		len(repositories),
	)

	var waitGroup sync.WaitGroup
	var workerIndex int
	for workerIndex = 0; workerIndex < syncer.workers; workerIndex++ {
		waitGroup.Add(1)
		go syncer.worker(
			requestContext,
			&waitGroup,
			group,
			githubToken,
			gitlabToken,
			gitlabHost,
			repositoryChannel,
			resultChannel,
		)
	}

	var repository github.Repository
	for _, repository = range repositories {
		repositoryChannel <- repository
	}
	close(repositoryChannel)

	waitGroup.Wait()
	close(resultChannel)

	var results []SyncResult
	var result SyncResult
	for result = range resultChannel {
		results = append(results, result)
	}
	return results
}

// worker consumes repositories from repositories and pushes the outcome
// of syncing each one on results until repositories is closed.
func (syncer *Syncer) worker(
	requestContext context.Context,
	waitGroup *sync.WaitGroup,
	group gitlab.GroupInfo,
	githubToken, gitlabToken, gitlabHost string,
	repositories <-chan github.Repository,
	results chan<- SyncResult,
) {
	defer waitGroup.Done()

	var repository github.Repository
	for repository = range repositories {
		var result SyncResult = syncer.syncRepository(
			requestContext,
			group,
			repository,
			githubToken,
			gitlabToken,
			gitlabHost,
		)
		results <- result
	}
}

// syncRepository synchronizes the given GitHub repository to the given
// GitLab group. It compares the remote refs first and returns
// StatusSkipped when the GitHub and GitLab sides advertise the same
// hashes. Otherwise it ensures the destination project exists, performs
// a mirror clone from GitHub, synchronizes Git LFS objects from GitHub
// to GitLab, performs a mirror push to GitLab, then updates the default
// branch on the GitLab side.
func (syncer *Syncer) syncRepository(
	requestContext context.Context,
	group gitlab.GroupInfo,
	repository github.Repository,
	githubToken, gitlabToken, gitlabHost string,
) SyncResult {
	var githubURL string = fmt.Sprintf(
		"https://github.com/%s.git",
		repository.FullName,
	)
	var projectPath string = group.FullPath + "/" + repositoryName(
		repository.FullName,
	)
	var gitlabURL string = fmt.Sprintf(
		"https://%s/%s.git",
		gitlabHost,
		projectPath,
	)

	var githubReferences map[string]string
	var githubReferencesError error
	githubReferences, githubReferencesError = syncer.mirrorClient.GetRefs(
		requestContext,
		githubURL,
		"x-access-token",
		githubToken,
	)
	if githubReferencesError != nil {
		if errors.Is(githubReferencesError, transport.ErrEmptyRemoteRepository) {
			return SyncResult{
				Repo:   repository.FullName,
				Status: StatusSkipped,
			}
		}
		return SyncResult{
			Repo:   repository.FullName,
			Status: StatusFailed,
			Err: fmt.Errorf(
				"get gh refs: %w",
				githubReferencesError,
			),
		}
	}

	var gitlabReferences map[string]string
	var gitlabReferencesError error
	gitlabReferences, gitlabReferencesError = syncer.mirrorClient.GetRefs(
		requestContext,
		gitlabURL,
		"oauth2",
		gitlabToken,
	)
	if gitlabReferencesError != nil {
		gitlabReferences = map[string]string{}
	}

	if referencesEqual(githubReferences, gitlabReferences) {
		return SyncResult{
			Repo:   repository.FullName,
			Status: StatusSkipped,
		}
	}

	var name string = repositoryName(repository.FullName)
	var ensureError error = syncer.gitlabClient.EnsureProject(
		requestContext,
		group,
		name,
		repository.Private,
	)
	if ensureError != nil {
		return SyncResult{
			Repo:   repository.FullName,
			Status: StatusFailed,
			Err:    fmt.Errorf("ensure project: %w", ensureError),
		}
	}

	var temporaryDirectory string
	var temporaryDirectoryError error
	temporaryDirectory, temporaryDirectoryError = os.MkdirTemp(
		"",
		"mirror-*",
	)
	if temporaryDirectoryError != nil {
		return SyncResult{
			Repo:   repository.FullName,
			Status: StatusFailed,
			Err: fmt.Errorf(
				"temp dir: %w",
				temporaryDirectoryError,
			),
		}
	}
	defer func() { _ = os.RemoveAll(temporaryDirectory) }()

	var cloneDirectory string = temporaryDirectory + "/repo.git"
	log.Printf("start clone for %q", repository.FullName)
	var cloneError error = syncer.mirrorClient.MirrorClone(
		requestContext,
		githubURL,
		"x-access-token",
		githubToken,
		cloneDirectory,
	)
	if cloneError != nil {
		return SyncResult{
			Repo:   repository.FullName,
			Status: StatusFailed,
			Err:    fmt.Errorf("mirror clone: %w", cloneError),
		}
	}
	log.Printf("done clone for %q", repository.FullName)

	log.Printf("start lfs for %q", repository.FullName)
	var lfsError error = syncer.mirrorClient.MirrorLFS(
		requestContext,
		cloneDirectory,
		githubURL,
		"x-access-token",
		githubToken,
		gitlabURL,
		"oauth2",
		gitlabToken,
	)
	if lfsError != nil {
		return SyncResult{
			Repo:   repository.FullName,
			Status: StatusFailed,
			Err:    fmt.Errorf("mirror lfs: %w", lfsError),
		}
	}
	log.Printf("done lfs for %q", repository.FullName)

	log.Printf("start push for %q", repository.FullName)
	var pushError error = syncer.mirrorClient.MirrorPush(
		requestContext,
		cloneDirectory,
		gitlabURL,
		"oauth2",
		gitlabToken,
	)
	if pushError != nil {
		return SyncResult{
			Repo:   repository.FullName,
			Status: StatusFailed,
			Err:    fmt.Errorf("mirror push: %w", pushError),
		}
	}
	log.Printf("done push for %q", repository.FullName)

	var defaultBranchError error = syncer.gitlabClient.SetDefaultBranch(
		requestContext,
		projectPath,
		repository.DefaultBranch,
	)
	if defaultBranchError != nil {
		return SyncResult{
			Repo:   repository.FullName,
			Status: StatusFailed,
			Err: fmt.Errorf(
				"set default branch: %w",
				defaultBranchError,
			),
		}
	}

	return SyncResult{Repo: repository.FullName, Status: StatusSynced}
}

// repositoryName returns the substring after the last "/" in fullName,
// falling back to fullName itself when it does not contain a slash. It
// extracts the repository name from a "owner/name" GitHub full name.
func repositoryName(fullName string) string {
	var index int
	for index = len(fullName) - 1; index >= 0; index-- {
		if fullName[index] == '/' {
			return fullName[index+1:]
		}
	}
	return fullName
}

// referencesEqual reports whether the two ref maps contain exactly the
// same set of keys mapped to exactly the same commit hashes.
func referencesEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	var key string
	var leftValue string
	for key, leftValue = range left {
		var rightValue string
		var exists bool
		rightValue, exists = right[key]
		if !exists || leftValue != rightValue {
			return false
		}
	}
	return true
}
