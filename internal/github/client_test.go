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

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("affiliation") != "owner,organization_member" {
			t.Errorf(
				"expected affiliation=owner,organization_member, got %s",
				r.URL.Query().Get("affiliation"),
			)
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
