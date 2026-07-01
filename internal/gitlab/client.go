package gitlab

import (
	"context"
	"fmt"
	"strings"

	gl "gitlab.com/gitlab-org/api/client-go"
)

// GroupInfo holds the numeric identifier and the full slash-separated
// path of a GitLab group that has been resolved from its path.
type GroupInfo struct {
	ID       int64
	FullPath string
}

// Client wraps a go-gitlab client and exposes the group and project
// operations required by the mirroring pipeline.
type Client struct {
	client *gl.Client
}

// NewClient returns a Client configured to talk to the GitLab instance
// reachable at https://<host> using the provided personal access token.
// For gitlab.com, pass "gitlab.com" as host. For self-managed instances,
// pass the full host (for example "gitlab.example.com").
func NewClient(host, token string) (*Client, error) {
	var underlyingClient *gl.Client
	var creationError error
	underlyingClient, creationError = gl.NewClient(
		token,
		gl.WithBaseURL("https://"+host),
	)
	if creationError != nil {
		return nil, fmt.Errorf("Create GitLab client: %w", creationError)
	}
	return &Client{client: underlyingClient}, nil
}

// NewClientWithURL returns a Client authenticated with the provided token
// and configured to talk to the GitLab API served at baseURL. It is
// primarily used in tests to point the client at an httptest server.
func NewClientWithURL(token, baseURL string) (*Client, error) {
	var underlyingClient *gl.Client
	var creationError error
	underlyingClient, creationError = gl.NewClient(
		token,
		gl.WithBaseURL(baseURL),
	)
	if creationError != nil {
		return nil, fmt.Errorf("Create GitLab client: %w", creationError)
	}
	return &Client{client: underlyingClient}, nil
}

// ResolveGroup fetches the group referenced by groupPath (for example
// "my-group/subgroup") and returns its numeric identifier along with its
// canonical full path.
func (client *Client) ResolveGroup(
	requestContext context.Context, groupPath string,
) (GroupInfo, error) {
	var group *gl.Group
	var resolveError error
	group, _, resolveError = client.client.Groups.GetGroup(
		groupPath,
		nil,
		gl.WithContext(requestContext),
	)
	if resolveError != nil {
		return GroupInfo{}, fmt.Errorf(
			"Resolve group %q: %w",
			groupPath,
			resolveError,
		)
	}
	return GroupInfo{ID: group.ID, FullPath: group.FullPath}, nil
}

// EnsureProject creates the project named name inside the group described
// by group when it does not exist. Existing projects are left untouched.
// The private flag controls the visibility used when the project is
// created (private when true, public otherwise).
func (client *Client) EnsureProject(
	requestContext context.Context,
	group GroupInfo,
	name string,
	private bool,
) error {
	var fullPath string = group.FullPath + "/" + name

	var response *gl.Response
	var getError error
	_, response, getError = client.client.Projects.GetProject(
		fullPath,
		nil,
		gl.WithContext(requestContext),
	)
	if getError == nil {
		return nil
	}
	if response == nil {
		return fmt.Errorf("Get project %q: %w", fullPath, getError)
	}
	if response.StatusCode != 404 {
		return fmt.Errorf("Get project %q: %w", fullPath, getError)
	}

	var visibility gl.VisibilityValue = gl.PublicVisibility
	if private {
		visibility = gl.PrivateVisibility
	}

	var createError error
	_, _, createError = client.client.Projects.CreateProject(
		&gl.CreateProjectOptions{
			Name:        gl.Ptr(name),
			NamespaceID: gl.Ptr(group.ID),
			Visibility:  gl.Ptr(visibility),
		},
		gl.WithContext(requestContext),
	)
	if createError != nil {
		if strings.Contains(createError.Error(), "has already been taken") {
			return nil
		}
		return fmt.Errorf("Create project %q: %w", fullPath, createError)
	}
	return nil
}

// SetDefaultBranch updates the default branch of the project identified
// by projectPath to branch.
func (client *Client) SetDefaultBranch(
	requestContext context.Context, projectPath, branch string,
) error {
	var editError error
	_, _, editError = client.client.Projects.EditProject(
		projectPath,
		&gl.EditProjectOptions{
			DefaultBranch: gl.Ptr(branch),
		},
		gl.WithContext(requestContext),
	)
	if editError != nil {
		return fmt.Errorf(
			"Set default branch for %q: %w",
			projectPath,
			editError,
		)
	}
	return nil
}
