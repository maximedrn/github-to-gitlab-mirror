package mirror

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
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

	var progress *redactingProgress = &redactingProgress{
		writer:  os.Stderr,
		secrets: []string{token},
	}

	var pushError error = remote.PushContext(
		requestContext,
		&git.PushOptions{
			Auth:       authentication,
			RemoteName: remote.Config().Name,
			RefSpecs:   []config.RefSpec{"+refs/*:refs/*"},
			Progress:   progress,
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
// not installed on the host or when the repository does not use Git LFS
// (git lfs fetch/push are no-ops in that case). Otherwise it fetches every
// LFS object from the source remote into the local bare repository and
// then pushes every LFS object to the target remote so that the subsequent
// mirror push of the refs is not rejected by the target's pre-receive
// hook.
//
// The source credentials are injected into the origin remote URL of the
// bare repository for the duration of the fetch, and the target
// credentials are injected into the target URL passed to the LFS push.
// Both live only in the temporary clone directory, which is removed by
// the caller once the synchronization completes.
//
// The stderr of git lfs fetch/push is forwarded to the parent process's
// stderr (with credentials redacted) so that the Actions log shows how
// many objects were transferred.
func (client *Client) MirrorLFS(
	requestContext context.Context,
	repositoryDirectory,
	sourceURL, sourceUser, sourceToken,
	targetURL, targetUser, targetToken string,
) error {
	if !client.lfsInstalled() {
		log.Printf(
			"lfs: git-lfs not installed on host, skipping LFS sync for %s",
			repositoryDirectory,
		)
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

	var fetchStdout, fetchStderr []byte
	var fetchError error
	fetchStdout, fetchStderr, fetchError = runGit(
		requestContext,
		repositoryDirectory,
		"lfs",
		"fetch",
		"--all",
	)
	logLFSDiagnostics(os.Stderr, "lfs fetch", fetchStdout, fetchStderr, secrets)
	if fetchError != nil {
		return fmt.Errorf(
			"lfs fetch: %s%s: %w",
			redactSecrets(string(fetchStdout), secrets...),
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

	var pushStdout, pushStderr []byte
	var lfsPushError error
	pushStdout, pushStderr, lfsPushError = runGit(
		requestContext,
		repositoryDirectory,
		"lfs",
		"push",
		"--all",
		credentialsTargetURL,
	)
	logLFSDiagnostics(os.Stderr, "lfs push", pushStdout, pushStderr, secrets)
	if lfsPushError != nil {
		return fmt.Errorf(
			"lfs push: %s%s: %w",
			redactSecrets(string(pushStdout), secrets...),
			redactSecrets(string(pushStderr), secrets...),
			lfsPushError,
		)
	}

	return nil
}

// logLFSDiagnostics writes a labeled, indented dump of the captured
// stdout and stderr to the process's os.Stderr (with secrets redacted)
// so that the Actions log reveals what git-lfs transferred for each
// repository. It is a no-op when both outputs are empty.
func logLFSDiagnostics(
	writer *os.File, label string,
	stdout, stderr []byte, secrets []string,
) {
	var combined string = string(stdout) + string(stderr)
	var redacted string = redactSecrets(combined, secrets...)
	var trimmed string = strings.TrimSpace(redacted)
	if trimmed == "" {
		return
	}
	var line string
	for _, line = range strings.Split(trimmed, "\n") {
		_, _ = fmt.Fprintf(writer, "    [%s] %s\n", label, line)
	}
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

// redactingProgress is an io.Writer passed to go-git's PushOptions.Progress
// so every sideband progress message the remote emits (for GitLab the
// "remote: GitLab: <reason>" lines explaining a pre-receive hook
// rejection) is forwarded to the parent process's stderr with
// credentials redacted. go-git normally drops this stream, leaving only
// "pre-receive hook declined" without the actionable explanation.
type redactingProgress struct {
	writer  *os.File
	secrets []string
}

// Write redacts secrets from chunk and forwards it verbatim (preserving
// line breaks and the leading "remote: " prefix go-git adds) to the
// underlying writer.
func (progress *redactingProgress) Write(chunk []byte) (int, error) {
	var redacted string = redactSecrets(string(chunk), progress.secrets...)
	var bytesWritten int
	var writeError error
	bytesWritten, writeError = progress.writer.Write([]byte(redacted))
	if writeError != nil {
		return bytesWritten, writeError
	}
	return len(chunk), nil
}
