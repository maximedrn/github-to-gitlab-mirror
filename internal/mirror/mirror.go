package mirror

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Client exposes the go-git mirror operations required by the mirroring
// pipeline: listing remote refs, cloning as a mirror, and pushing every
// ref to a target remote.
type Client struct{}

// New returns a ready-to-use Client. The Client is stateless so a single
// instance can safely be shared between goroutines.
func New() *Client {
	return &Client{}
}

// GetRefs returns a map of ref name to commit hash for every ref
// advertised by the remote reachable at remoteURL. When user is non-empty
// the request is authenticated with HTTP basic auth using user and token.
func (client *Client) GetRefs(
	requestContext context.Context,
	remoteURL, user, token string,
) (map[string]string, error) {
	var remote *git.Remote = git.NewRemote(
		memory.NewStorage(),
		&config.RemoteConfig{
			Name: "origin",
			URLs: []string{remoteURL},
		},
	)

	var authentication transport.AuthMethod
	if user != "" {
		authentication = &http.BasicAuth{Username: user, Password: token}
	}

	var references []*plumbing.Reference
	var listError error
	references, listError = remote.ListContext(
		requestContext,
		&git.ListOptions{Auth: authentication},
	)
	if listError != nil {
		return nil, fmt.Errorf("List remote: %w", listError)
	}

	var result map[string]string = make(map[string]string, len(references))
	var reference *plumbing.Reference
	for _, reference = range references {
		result[reference.Name().String()] = reference.Hash().String()
	}
	return result, nil
}

// MirrorClone performs a mirror clone of the remote reachable at
// remoteURL into destinationDirectory, using HTTP basic authentication
// when user is non-empty.
func (client *Client) MirrorClone(
	requestContext context.Context,
	remoteURL, user, token, destinationDirectory string,
) error {
	var authentication transport.AuthMethod
	if user != "" {
		authentication = &http.BasicAuth{Username: user, Password: token}
	}

	var cloneError error
	_, cloneError = git.PlainCloneContext(
		requestContext,
		destinationDirectory,
		true,
		&git.CloneOptions{
			URL:    remoteURL,
			Auth:   authentication,
			Mirror: true,
		},
	)
	if cloneError != nil {
		return fmt.Errorf("Mirror clone: %w", cloneError)
	}
	return nil
}

// MirrorPush pushes every ref from the bare repository located at
// repositoryDirectory to the remote reachable at remoteURL, creating the
// "mirror-target" remote when needed. An "already up to date" outcome is
// treated as success.
func (client *Client) MirrorPush(
	requestContext context.Context,
	repositoryDirectory, remoteURL, user, token string,
) error {
	var repository *git.Repository
	var openError error
	repository, openError = git.PlainOpen(repositoryDirectory)
	if openError != nil {
		return fmt.Errorf("Open repository: %w", openError)
	}

	var remote *git.Remote
	var remoteError error
	remote, remoteError = repository.Remote("mirror-target")
	if remoteError != nil {
		remote, remoteError = repository.CreateRemote(&config.RemoteConfig{
			Name: "mirror-target",
			URLs: []string{remoteURL},
		})
		if remoteError != nil {
			return fmt.Errorf(
				"Setup mirror-target remote: %w",
				remoteError,
			)
		}
	}

	var authentication transport.AuthMethod
	if user != "" {
		authentication = &http.BasicAuth{Username: user, Password: token}
	}

	var pushError error = remote.PushContext(
		requestContext,
		&git.PushOptions{
			Auth:       authentication,
			RemoteName: remote.Config().Name,
			RefSpecs:   []config.RefSpec{"+refs/*:refs/*"},
		},
	)
	if pushError != nil && !errors.Is(pushError, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("Mirror push: %w", pushError)
	}
	return nil
}
