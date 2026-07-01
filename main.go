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
