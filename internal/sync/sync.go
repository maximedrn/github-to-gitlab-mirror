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
