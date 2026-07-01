package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gh "github.com/google/go-github/v72/github"

	"github.com/maximedrn/github-to-gitlab-mirror/internal/github"
)

// TestListRepositories_ReturnsAllPages verifies that ListRepositories follows
// GitHub pagination and returns every repository across the successive pages of
// the /user/repos endpoint.
func TestListRepositories_ReturnsAllPages(test *testing.T) {
	var requestContext context.Context = context.Background()
	var pageIndex int = 0

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/user/repos" {
				writer.WriteHeader(404)
				return
			}
			if request.URL.Query().
				Get("affiliation") !=
				"owner,organization_member" {
				test.Errorf(
					"Expected affiliation=owner,organization_member, got %s",
					request.URL.Query().Get("affiliation"),
				)
			}

			pageIndex++
			var repositories []*gh.Repository = []*gh.Repository{}
			if pageIndex == 1 {
				repositories = append(repositories, &gh.Repository{
					FullName:      gh.Ptr("user/repository-1"),
					Private:       gh.Ptr(false),
					DefaultBranch: gh.Ptr("main"),
				})
				repositories = append(repositories, &gh.Repository{
					FullName:      gh.Ptr("org/repository-2"),
					Private:       gh.Ptr(true),
					DefaultBranch: gh.Ptr("master"),
				})

				writer.Header().Set(
					"Link",
					`<`+server.URL+`/user/repos?page=2>; rel="next"`,
				)
			} else {
				repositories = append(repositories, &gh.Repository{
					FullName:      gh.Ptr("user/repository-3"),
					Private:       gh.Ptr(false),
					DefaultBranch: gh.Ptr("main"),
				})
			}

			writer.Header().Set("Content-Type", "application/json")
			var encodeError error = json.NewEncoder(writer).Encode(
				repositories,
			)
			if encodeError != nil {
				test.Fatalf("Encode response: %v", encodeError)
			}
		}))
	defer server.Close()

	var client *github.Client = github.NewClientWithURL(
		"test-token",
		server.URL,
	)
	var repositories []github.Repository
	var listError error
	repositories, listError = client.ListRepositories(requestContext)
	if listError != nil {
		test.Fatalf("ListRepositories: %v", listError)
	}

	if len(repositories) != 3 {
		test.Fatalf("Expected 3 repositories, got %d", len(repositories))
	}
	if repositories[0].FullName != "user/repository-1" {
		test.Errorf(
			"Expected user/repository-1, got %s",
			repositories[0].FullName,
		)
	}
	if repositories[1].Private != true {
		test.Errorf("Expected repository-2 to be private")
	}
	if repositories[2].FullName != "user/repository-3" {
		test.Errorf(
			"Expected user/repository-3, got %s",
			repositories[2].FullName,
		)
	}
}
