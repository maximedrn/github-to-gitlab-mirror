package mirror_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/maximedrn/github-to-gitlab-mirror/internal/mirror"
)

// TestGetRefs_LocalBareRepo verifies that GetRefs returns the refs of a
// bare repository accessed through a file:// URL.
func TestGetRefs_LocalBareRepo(test *testing.T) {
	var requestContext context.Context = context.Background()
	var directory string = test.TempDir()

	// Create a bare repo with one commit.
	var bareRepository string = filepath.Join(directory, "source.git")
	runGitCommand(test, directory, "init", "--bare", bareRepository)

	// Clone the bare repo, make a commit, push.
	var temporaryDirectory string = filepath.Join(directory, "tmp")
	runGitCommand(test, directory, "clone", bareRepository, temporaryDirectory)
	runGitCommand(
		test,
		temporaryDirectory,
		"config",
		"user.email",
		"test@test.com",
	)
	runGitCommand(test, temporaryDirectory, "config", "user.name", "Test")
	runGitCommand(test, temporaryDirectory, "checkout", "-b", "main")
	writeFileContent(
		test,
		filepath.Join(temporaryDirectory, "README.md"),
		"# hello",
	)
	runGitCommand(test, temporaryDirectory, "add", "README.md")
	runGitCommand(test, temporaryDirectory, "commit", "-m", "initial")
	runGitCommand(test, temporaryDirectory, "push", "origin", "main")
	runGitCommand(
		test,
		bareRepository,
		"symbolic-ref",
		"HEAD",
		"refs/heads/main",
	)

	var client *mirror.Client = mirror.New()
	var references map[string]string
	var listError error
	references, listError = client.GetRefs(
		requestContext,
		"file://"+bareRepository,
		"",
		"",
	)
	if listError != nil {
		test.Fatalf("GetRefs: %v", listError)
	}

	if _, exists := references["refs/heads/main"]; !exists {
		test.Errorf(
			"Expected refs/heads/main to exist, got %v",
			references,
		)
	}
}

// TestMirrorClonePush verifies that MirrorClone followed by MirrorPush
// copies every ref from a source bare repository to a destination bare
// repository.
func TestMirrorClonePush(test *testing.T) {
	var requestContext context.Context = context.Background()
	var directory string = test.TempDir()

	// Source bare repo.
	var sourceRepository string = filepath.Join(directory, "source.git")
	runGitCommand(test, directory, "init", "--bare", sourceRepository)

	// Clone, commit, push.
	var temporaryDirectory string = filepath.Join(directory, "tmp")
	runGitCommand(
		test,
		directory,
		"clone",
		sourceRepository,
		temporaryDirectory,
	)
	runGitCommand(
		test,
		temporaryDirectory,
		"config",
		"user.email",
		"test@test.com",
	)
	runGitCommand(test, temporaryDirectory, "config", "user.name", "Test")
	runGitCommand(test, temporaryDirectory, "checkout", "-b", "main")
	writeFileContent(
		test,
		filepath.Join(temporaryDirectory, "README.md"),
		"# hello",
	)
	runGitCommand(test, temporaryDirectory, "add", "README.md")
	runGitCommand(test, temporaryDirectory, "commit", "-m", "initial")
	runGitCommand(test, temporaryDirectory, "push", "origin", "main")
	runGitCommand(
		test,
		sourceRepository,
		"symbolic-ref",
		"HEAD",
		"refs/heads/main",
	)

	// Destination bare repository.
	var destinationRepository string = filepath.Join(directory, "dest.git")
	runGitCommand(test, directory, "init", "--bare", destinationRepository)

	// Mirror clone.
	var client *mirror.Client = mirror.New()
	var cloneDirectory string = filepath.Join(directory, "clone.git")
	var cloneError error = client.MirrorClone(
		requestContext,
		"file://"+sourceRepository,
		"",
		"",
		cloneDirectory,
	)
	if cloneError != nil {
		test.Fatalf("MirrorClone: %v", cloneError)
	}

	// Mirror push.
	var pushError error = client.MirrorPush(
		requestContext,
		cloneDirectory,
		"file://"+destinationRepository,
		"",
		"",
	)
	if pushError != nil {
		test.Fatalf("MirrorPush: %v", pushError)
	}

	// Verify destination has the ref.
	var references map[string]string
	var listError error
	references, listError = client.GetRefs(
		requestContext,
		"file://"+destinationRepository,
		"",
		"",
	)
	if listError != nil {
		test.Fatalf("GetRefs on destination: %v", listError)
	}
	if _, exists := references["refs/heads/main"]; !exists {
		test.Errorf(
			"Expected refs/heads/main in destination, got %v",
			references,
		)
	}
}

// runGitCommand runs the git subcommand described by arguments in the
// workingDirectory. When the command fails the current test is aborted
// with the captured combined output.
func runGitCommand(
	test *testing.T,
	workingDirectory string,
	arguments ...string,
) {
	test.Helper()
	var command *exec.Cmd = exec.Command("git", arguments...)
	command.Dir = workingDirectory
	var output []byte
	var commandError error
	output, commandError = command.CombinedOutput()
	if commandError != nil {
		test.Fatalf("git %v: %v\n%s", arguments, commandError, output)
	}
}

// writeFileContent writes content to the file at path, aborting the
// current test when the file cannot be written.
func writeFileContent(test *testing.T, path, content string) {
	test.Helper()
	var writeError error = os.WriteFile(path, []byte(content), 0644)
	if writeError != nil {
		test.Fatalf("writeFile: %v", writeError)
	}
}
