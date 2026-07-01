// Package main is the entry point of the github-to-gitlab-mirror
// command-line tool.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/maximedrn/github-to-gitlab-mirror/internal/github"
	"github.com/maximedrn/github-to-gitlab-mirror/internal/gitlab"
	"github.com/maximedrn/github-to-gitlab-mirror/internal/mirror"
	"github.com/maximedrn/github-to-gitlab-mirror/internal/sync"
)

// main reads configuration from environment variables, drives the
// synchronization of every GitHub repository to the target GitLab group,
// prints a per-repository status summary, and exits with code 1 when at
// least one repository failed to synchronize.
func main() {
	var githubPersonalAccessToken string = requireEnvironment("GH_PAT")
	var gitlabToken string = requireEnvironment("GITLAB_TOKEN")
	var gitlabGroup string = requireEnvironment("GITLAB_GROUP")
	var gitlabHost string = os.Getenv("GITLAB_HOST")
	if gitlabHost == "" {
		gitlabHost = "gitlab.com"
	}

	var workers int = 5
	var workersEnvironment string = os.Getenv("WORKERS")
	if workersEnvironment != "" {
		var parseError error
		workers, parseError = strconv.Atoi(workersEnvironment)
		if parseError != nil || workers < 1 {
			log.Fatalf(
				"WORKERS must be a positive integer, got %q",
				workersEnvironment,
			)
		}
	}

	var githubClient *github.Client = github.NewClient(
		githubPersonalAccessToken,
	)

	var gitlabClient *gitlab.Client
	var gitlabClientError error
	gitlabClient, gitlabClientError = gitlab.NewClient(
		gitlabHost,
		gitlabToken,
	)
	if gitlabClientError != nil {
		log.Fatalf("Create GitLab client: %v", gitlabClientError)
	}

	var mirrorClient *mirror.Client = mirror.New()
	var syncer *sync.Syncer = sync.New(
		githubClient,
		gitlabClient,
		mirrorClient,
		workers,
	)

	var runContext context.Context
	var cancelRunContext context.CancelFunc
	runContext, cancelRunContext = context.WithTimeout(
		context.Background(),
		30*time.Minute,
	)
	defer cancelRunContext()

	var results []sync.SyncResult = syncer.Sync(
		runContext,
		gitlabGroup,
		githubPersonalAccessToken,
		gitlabToken,
		gitlabHost,
	)

	var syncedCount, skippedCount, failedCount int
	var result sync.SyncResult
	for _, result = range results {
		switch result.Status {
		case sync.StatusSynced:
			syncedCount++
			fmt.Printf("  %s - synced\n", result.Repo)
		case sync.StatusSkipped:
			skippedCount++
			fmt.Printf("  %s - skipped (no changes)\n", result.Repo)
		case sync.StatusFailed:
			failedCount++
			fmt.Printf("  %s - FAILED: %v\n", result.Repo, result.Err)
		}
	}

	fmt.Println("------------------------------------")
	fmt.Printf(
		"Synced: %d | Skipped: %d | Failed: %d\n",
		syncedCount,
		skippedCount,
		failedCount,
	)

	if failedCount > 0 {
		os.Exit(1)
	}
}

// requireEnvironment returns the value of the environment variable named
// by name and terminates the process with a fatal log message when the
// variable is unset or empty.
func requireEnvironment(name string) string {
	var value string = os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}
