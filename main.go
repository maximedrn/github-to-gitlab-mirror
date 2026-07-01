// Package main is the entry point of the github-to-gitlab-mirror
// command-line tool.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/maximedrn/github-to-gitlab-mirror/internal/github"
	"github.com/maximedrn/github-to-gitlab-mirror/internal/gitlab"
	"github.com/maximedrn/github-to-gitlab-mirror/internal/mirror"
	"github.com/maximedrn/github-to-gitlab-mirror/internal/sync"
)

// httpURLPattern matches HTTP(S) URLs embedded in text so their path
// segments can be masked before the URL is written to logs.
var httpURLPattern *regexp.Regexp = regexp.MustCompile(
	`https?://[^\s"'<>]+`,
)

// quotedSlashPathPattern matches double-quoted slash-separated paths
// like "group/repo" or "group/subgroup/repo" as produced by fmt.Errorf
// with %q. It requires at least one "/" so bare quoted words are left
// alone.
var quotedSlashPathPattern *regexp.Regexp = regexp.MustCompile(
	`"[a-zA-Z0-9_.-]+(?:/[a-zA-Z0-9_.-]+)+"`,
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
			fmt.Printf(
				"  %s - synced\n",
				maskRepositoryName(result.Repo),
			)
		case sync.StatusSkipped:
			skippedCount++
			fmt.Printf(
				"  %s - skipped (no changes)\n",
				maskRepositoryName(result.Repo),
			)
		case sync.StatusFailed:
			failedCount++
			fmt.Printf(
				"  %s - FAILED: %s\n",
				maskRepositoryName(result.Repo),
				maskErrorMessage(result.Err),
			)
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

// maskRepositoryName returns fullName with every slash-separated segment
// obfuscated so that only the first and last characters remain visible,
// with three asterisks in between. Segments of two characters or less
// are fully replaced with three asterisks so the original length is not
// disclosed.
func maskRepositoryName(fullName string) string {
	var segments []string = strings.Split(fullName, "/")
	var index int
	var segment string
	for index, segment = range segments {
		segments[index] = maskSegment(segment)
	}
	return strings.Join(segments, "/")
}

// maskSegment returns segment with its middle replaced by three
// asterisks, keeping only the first and last characters visible. When
// segment is two characters or less the whole segment is replaced.
func maskSegment(segment string) string {
	if len(segment) <= 2 {
		return "***"
	}
	return string(segment[0]) + "***" + string(segment[len(segment)-1])
}

// maskErrorMessage returns caughtError's message with every embedded
// HTTP(S) URL rewritten by maskURL and every double-quoted
// slash-separated path rewritten by maskQuotedSlashPath so user
// handles, group names, or repository names never appear in the
// printed error text.
func maskErrorMessage(caughtError error) string {
	if caughtError == nil {
		return ""
	}
	var message string = caughtError.Error()
	message = httpURLPattern.ReplaceAllStringFunc(message, maskURL)
	message = quotedSlashPathPattern.ReplaceAllStringFunc(
		message,
		maskQuotedSlashPath,
	)
	return message
}

// maskQuotedSlashPath strips the surrounding double quotes from
// quotedPath, applies maskRepositoryName to the inner slash-separated
// path, then wraps the masked result back into double quotes.
func maskQuotedSlashPath(quotedPath string) string {
	if len(quotedPath) < 2 {
		return quotedPath
	}
	var innerPath string = quotedPath[1 : len(quotedPath)-1]
	return `"` + maskRepositoryName(innerPath) + `"`
}

// maskURL returns fullURL with every non-empty path segment obfuscated
// via maskSegment while preserving the scheme, the host, and any query
// or fragment. Trailing punctuation frequently added by error
// formatters (":", ",", ".", ";", ")", "]", "}") is kept outside the
// mask so the surrounding message remains readable. When fullURL has no
// scheme separator or no path it is returned unchanged.
func maskURL(fullURL string) string {
	var trimmedURL string = strings.TrimRight(fullURL, ":,;.)]}")
	var trailingPunctuation string = fullURL[len(trimmedURL):]

	var schemeSeparator int = strings.Index(trimmedURL, "://")
	if schemeSeparator < 0 {
		return fullURL
	}
	var hostStart int = schemeSeparator + 3
	var relativePathStart int = strings.Index(trimmedURL[hostStart:], "/")
	if relativePathStart < 0 {
		return fullURL
	}
	var pathStart int = hostStart + relativePathStart
	var schemeAndHost string = trimmedURL[:pathStart]
	var pathAndSuffix string = trimmedURL[pathStart:]

	var suffixStart int = strings.IndexAny(pathAndSuffix, "?#")
	var path string = pathAndSuffix
	var suffix string = ""
	if suffixStart >= 0 {
		path = pathAndSuffix[:suffixStart]
		suffix = pathAndSuffix[suffixStart:]
	}

	var segments []string = strings.Split(path, "/")
	var index int
	var segment string
	for index, segment = range segments {
		if segment == "" {
			continue
		}
		segments[index] = maskSegment(segment)
	}
	return schemeAndHost +
		strings.Join(segments, "/") +
		suffix +
		trailingPunctuation
}
