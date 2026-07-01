package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	gh "github.com/google/go-github/v72/github"
	"golang.org/x/oauth2"
)

// Repository describes the subset of GitHub repository metadata that the
// mirroring pipeline needs to synchronize a project to GitLab.
type Repository struct {
	FullName      string
	Private       bool
	DefaultBranch string
}

// Client wraps a go-github client and exposes the repository listing
// operations required by the mirroring pipeline.
type Client struct {
	client *gh.Client
}

// NewClient returns a Client authenticated against the public GitHub API
// with the provided OAuth token.
func NewClient(token string) *Client {
	var tokenSource oauth2.TokenSource = oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	var httpClient *http.Client = oauth2.NewClient(
		context.Background(),
		tokenSource,
	)
	return &Client{client: gh.NewClient(httpClient)}
}

// NewClientWithURL returns a Client authenticated with the provided token
// and configured to talk to the GitHub API served at baseURL. It is
// primarily used in tests to point the client at an httptest server.
func NewClientWithURL(token, baseURL string) *Client {
	var tokenSource oauth2.TokenSource = oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	var httpClient *http.Client = oauth2.NewClient(
		context.Background(),
		tokenSource,
	)
	var githubClient *gh.Client = gh.NewClient(httpClient)
	githubClient.BaseURL, _ = url.Parse(baseURL + "/")
	return &Client{client: githubClient}
}

// ListRepositories returns every repository the authenticated user owns or is a
// member of via an organization, following pagination until the last page
// has been fetched.
func (client *Client) ListRepositories(
	requestContext context.Context,
) ([]Repository, error) {
	var repositories []Repository
	var options *gh.RepositoryListByAuthenticatedUserOptions
	options = &gh.RepositoryListByAuthenticatedUserOptions{
		Affiliation: "owner,organization_member",
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	var service *gh.RepositoriesService = client.client.Repositories
	for {
		var page []*gh.Repository
		var response *gh.Response
		var listError error
		page, response, listError = service.ListByAuthenticatedUser(
			requestContext,
			options,
		)
		if listError != nil {
			return nil, fmt.Errorf("List repositories: %w", listError)
		}

		var repository *gh.Repository
		for _, repository = range page {
			var branch string = "master"
			if repository.GetDefaultBranch() != "" {
				branch = repository.GetDefaultBranch()
			}
			repositories = append(repositories, Repository{
				FullName:      repository.GetFullName(),
				Private:       repository.GetPrivate(),
				DefaultBranch: branch,
			})
		}

		if response.NextPage == 0 {
			break
		}
		options.Page = response.NextPage
	}

	return repositories, nil
}
