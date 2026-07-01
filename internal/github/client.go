package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	gh "github.com/google/go-github/v72/github"
	"golang.org/x/oauth2"
)

type Repository struct {
	FullName      string
	Private       bool
	DefaultBranch string
}

type Client struct {
	client *gh.Client
}

func NewClient(token string) *Client {
	var tokenSource oauth2.TokenSource = oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	var httpClient *http.Client = oauth2.NewClient(
		context.Background(), tokenSource,
	)
	return &Client{client: gh.NewClient(httpClient)}
}

func NewClientWithURL(token, baseURL string) *Client {
	var tokenSource oauth2.TokenSource = oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	var httpClient *http.Client = oauth2.NewClient(
		context.Background(), tokenSource,
	)
	var githubClient *gh.Client = gh.NewClient(httpClient)
	githubClient.BaseURL, _ = url.Parse(baseURL + "/")
	return &Client{client: githubClient}
}

func (client *Client) ListRepos(context context.Context) ([]Repository, error) {
	var repositories []Repository
	options := &gh.RepositoryListByAuthenticatedUserOptions{
		Affiliation: "owner,organization_member",
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	for {
		repos, response, error := client.client.Repositories.ListByAuthenticatedUser(
			context, options,
		)
		if error != nil {
			return nil, fmt.Errorf("List repos: %w", error)
		}

		for _, r := range repos {
			branch := "main"
			if r.GetDefaultBranch() != "" {
				branch = r.GetDefaultBranch()
			}
			repositories = append(repositories, Repository{
				FullName:      r.GetFullName(),
				Private:       r.GetPrivate(),
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
