package mirror

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Client exposes the go-git mirror operations required by the mirroring
// pipeline: listing remote refs, cloning as a mirror, pushing every ref
// to a target remote, and synchronizing Git LFS objects between the
// source and target remotes.
type Client struct {
	lfsProbeOnce sync.Once
	lfsAvailable bool
}

// New returns a ready-to-use Client. The Client is safe to share between
// goroutines; the Git LFS availability probe it performs is run at most
// once.
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

// MirrorLFS synchronizes the Git LFS objects of the bare repository located
// at repositoryDirectory from the source remote reachable at sourceURL to
// the target remote reachable at targetURL. It is a no-op when Git LFS is
// not installed on the host, when the repository does not use Git LFS, or
// when the source or target remotes do not require authentication (user
// empty). Otherwise it fetches every LFS object from the source remote into
// the local bare repository and then pushes every LFS object to the target
// remote so that the subsequent mirror push of the refs is not rejected by
// the target's pre-receive hook.
//
// The source credentials are injected into the origin remote URL of the
// bare repository for the duration of the fetch, and the target
// credentials are injected into the target URL passed to the LFS push.
// Both live only in the temporary clone directory, which is removed by
// the caller once the synchronization completes.
func (client *Client) MirrorLFS(
	requestContext context.Context,
	repositoryDirectory,
	sourceURL, sourceUser, sourceToken,
	targetURL, targetUser, targetToken string,
) error {
	if !client.lfsInstalled() {
		return nil
	}

	var used bool
	var detectError error
	used, detectError = client.lfsUsed(requestContext, repositoryDirectory)
	if detectError != nil {
		return fmt.Errorf("detect lfs: %w", detectError)
	}
	if !used {
		return nil
	}

	var secrets []string = []string{sourceToken, targetToken}

	var credentialsSourceURL string
	var sourceURLError error
	credentialsSourceURL, sourceURLError = embedCredentials(
		sourceURL,
		sourceUser,
		sourceToken,
	)
	if sourceURLError != nil {
		return fmt.Errorf("build source url: %w", sourceURLError)
	}

	var _, setOriginStderr []byte
	var setOriginError error
	_, setOriginStderr, setOriginError = runGit(
		requestContext,
		repositoryDirectory,
		"remote",
		"set-url",
		"origin",
		credentialsSourceURL,
	)
	if setOriginError != nil {
		return fmt.Errorf(
			"set origin url: %s: %w",
			redactSecrets(string(setOriginStderr), secrets...),
			setOriginError,
		)
	}

	var _, fetchStderr []byte
	var fetchError error
	_, fetchStderr, fetchError = runGit(
		requestContext,
		repositoryDirectory,
		"lfs",
		"fetch",
		"--all",
	)
	if fetchError != nil {
		return fmt.Errorf(
			"lfs fetch: %s: %w",
			redactSecrets(string(fetchStderr), secrets...),
			fetchError,
		)
	}

	var credentialsTargetURL string
	var targetURLError error
	credentialsTargetURL, targetURLError = embedCredentials(
		targetURL,
		targetUser,
		targetToken,
	)
	if targetURLError != nil {
		return fmt.Errorf("build target url: %w", targetURLError)
	}

	var _, pushStderr []byte
	var lfsPushError error
	_, pushStderr, lfsPushError = runGit(
		requestContext,
		repositoryDirectory,
		"lfs",
		"push",
		"--all",
		credentialsTargetURL,
	)
	if lfsPushError != nil {
		return fmt.Errorf(
			"lfs push: %s: %w",
			redactSecrets(string(pushStderr), secrets...),
			lfsPushError,
		)
	}

	return nil
}

// lfsInstalled reports whether the git-lfs extension is available on the
// host. The probe runs at most once per Client and its result is cached so
// concurrent workers do not repeat it.
func (client *Client) lfsInstalled() bool {
	client.lfsProbeOnce.Do(func() {
		var probeError error = exec.Command("git", "lfs", "version").Run()
		client.lfsAvailable = probeError == nil
	})
	return client.lfsAvailable
}

// lfsUsed reports whether the bare repository located at
// repositoryDirectory references at least one Git LFS object reachable
// from its refs. It must only be called when lfsInstalled reports true.
func (client *Client) lfsUsed(
	requestContext context.Context,
	repositoryDirectory string,
) (bool, error) {
	var stdout []byte
	var stderr []byte
	var listError error
	stdout, stderr, listError = runGit(
		requestContext,
		repositoryDirectory,
		"lfs",
		"ls-files",
		"--all",
	)
	if listError != nil {
		return false, fmt.Errorf(
			"lfs ls-files: %s: %w",
			string(stderr),
			listError,
		)
	}
	return len(bytes.TrimSpace(stdout)) > 0, nil
}

// runGit executes git with arguments in workingDirectory, honoring
// requestContext for cancellation and timeouts. It returns the captured
// stdout and stderr alongside any execution error.
func runGit(
	requestContext context.Context,
	workingDirectory string,
	arguments ...string,
) ([]byte, []byte, error) {
	var command *exec.Cmd = exec.CommandContext(
		requestContext,
		"git",
		arguments...,
	)
	command.Dir = workingDirectory
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	var runError error = command.Run()
	return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), runError
}

// embedCredentials returns rawURL with the userinfo user:token inserted so
// that downstream git and git-lfs commands authenticate over HTTP basic
// auth. When user is empty rawURL is returned unchanged.
func embedCredentials(rawURL, user, token string) (string, error) {
	if user == "" {
		return rawURL, nil
	}
	var parsedURL *url.URL
	var parseError error
	parsedURL, parseError = url.Parse(rawURL)
	if parseError != nil {
		return "", parseError
	}
	parsedURL.User = url.UserPassword(user, token)
	return parsedURL.String(), nil
}

// redactSecrets returns text with every non-empty secret in secrets
// replaced by "[REDACTED]" so credentials never leak through error
// messages. It is used to scrub git and git-lfs stderr before it is
// wrapped into an error that may be printed.
func redactSecrets(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}
