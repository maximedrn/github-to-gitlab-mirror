package gitlab

import (
	"context"
	"fmt"

	gl "github.com/xanzy/go-gitlab"
)

// GroupInfo holds the resolved group ID and full path.
type GroupInfo struct {
	ID       int
	FullPath string
}

// Client wraps go-gitlab for GitLab API operations.
type Client struct {
	client *gl.Client
}

// NewClient creates a new Client using host and token.
// For gitlab.com, pass "gitlab.com" as host. For self-managed instances,
// pass the full host (e.g. "gitlab.example.com").
func NewClient(host, token string) (*Client, error) {
	client, err := gl.NewClient(token, gl.WithBaseURL("https://"+host))
	if err != nil {
		return nil, fmt.Errorf("create gitlab client: %w", err)
	}
	return &Client{client: client}, nil
}

// NewClientWithURL creates a new Client with a custom base URL (used in tests).
func NewClientWithURL(token, baseURL string) (*Client, error) {
	client, err := gl.NewClient(token, gl.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("create gitlab client: %w", err)
	}
	return &Client{client: client}, nil
}

// ResolveGroup fetches group details by full path (e.g. "my-group/subgroup").
func (c *Client) ResolveGroup(
	ctx context.Context, groupPath string,
) (GroupInfo, error) {
	g, _, err := c.client.Groups.GetGroup(groupPath, nil, gl.WithContext(ctx))
	if err != nil {
		return GroupInfo{}, fmt.Errorf("resolve group %q: %w", groupPath, err)
	}
	return GroupInfo{ID: g.ID, FullPath: g.FullPath}, nil
}

// EnsureProject checks if a project exists in the given group
// and creates it if not found.
func (c *Client) EnsureProject(
	ctx context.Context, group GroupInfo, name string, private bool,
) error {
	fullPath := group.FullPath + "/" + name

	_, resp, err := c.client.Projects.GetProject(
		fullPath, nil, gl.WithContext(ctx),
	)
	if err == nil {
		return nil
	}
	if resp == nil {
		return fmt.Errorf("get project %q: %w", fullPath, err)
	}
	if resp.StatusCode != 404 {
		return fmt.Errorf("get project %q: %w", fullPath, err)
	}

	visibility := gl.PublicVisibility
	if private {
		visibility = gl.PrivateVisibility
	}

	_, _, err = c.client.Projects.CreateProject(&gl.CreateProjectOptions{
		Name:        gl.Ptr(name),
		NamespaceID: gl.Ptr(group.ID),
		Visibility:  gl.Ptr(visibility),
	}, gl.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("create project %q: %w", fullPath, err)
	}
	return nil
}

// SetDefaultBranch updates the default branch for a project.
func (c *Client) SetDefaultBranch(
	ctx context.Context, projectPath, branch string,
) error {
	_, _, err := c.client.Projects.EditProject(projectPath, &gl.EditProjectOptions{
		DefaultBranch: gl.Ptr(branch),
	}, gl.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("set default branch for %q: %w", projectPath, err)
	}
	return nil
}
