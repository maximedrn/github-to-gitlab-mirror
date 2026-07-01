package github

import (
	"context"
	"fmt"
	"net/url"

	gh "github.com/google/go-github/v72/github"
	"golang.org/x/oauth2"
)

type Repo struct {
	FullName      string
	Private       bool
	DefaultBranch string
}

type Client struct {
	client *gh.Client
}

func NewClient(token string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	return &Client{client: gh.NewClient(tc)}
}

func NewClientWithURL(token, baseURL string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	client := gh.NewClient(tc)
	client.BaseURL, _ = url.Parse(baseURL + "/")
	return &Client{client: client}
}

func (c *Client) ListRepos(ctx context.Context) ([]Repo, error) {
	var all []Repo
	opts := &gh.RepositoryListByAuthenticatedUserOptions{
		Affiliation: "owner,organization_member",
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := c.client.Repositories.ListByAuthenticatedUser(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("list repos: %w", err)
		}

		for _, r := range repos {
			branch := "main"
			if r.GetDefaultBranch() != "" {
				branch = r.GetDefaultBranch()
			}
			all = append(all, Repo{
				FullName:      r.GetFullName(),
				Private:       r.GetPrivate(),
				DefaultBranch: branch,
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return all, nil
}
