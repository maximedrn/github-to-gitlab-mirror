package mirror_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// requireLFS skips the current test when the git-lfs extension is not
// installed on the host so the LFS integration test only runs where the
// required tooling is present.
func requireLFS(test *testing.T) {
	test.Helper()
	var commandError error = exec.Command("git", "lfs", "version").Run()
	if commandError != nil {
		test.Skipf("git-lfs not installed: %v", commandError)
	}
}

// createLFSSourceRepository builds a bare repository at sourceRepository
// containing a single commit on the main branch that tracks and stores a
// small file through Git LFS. The returned oid is the LFS object id of the
// committed file.
func createLFSSourceRepository(
	test *testing.T,
	sourceRepository string,
) string {
	test.Helper()
	runGitCommand(test, filepath.Dir(sourceRepository), "init", "--bare", sourceRepository)

	// Clone, configure LFS locally, commit a tracked file, push back.
	var temporaryDirectory string = filepath.Join(
		filepath.Dir(sourceRepository),
		"work",
	)
	runGitCommand(
		test,
		filepath.Dir(sourceRepository),
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
	runGitCommand(test, temporaryDirectory, "lfs", "install", "--local")
	runGitCommand(test, temporaryDirectory, "lfs", "track", "*.bin")
	runGitCommand(test, temporaryDirectory, "add", ".gitattributes")
	runGitCommand(test, temporaryDirectory, "commit", "-m", "track lfs")
	writeFileContent(
		test,
		filepath.Join(temporaryDirectory, "big.bin"),
		strings.Repeat("X", 512),
	)
	runGitCommand(test, temporaryDirectory, "add", "big.bin")
	runGitCommand(test, temporaryDirectory, "commit", "-m", "add lfs file")
	runGitCommand(test, temporaryDirectory, "push", "origin", "HEAD:main")
	runGitCommand(
		test,
		sourceRepository,
		"symbolic-ref",
		"HEAD",
		"refs/heads/main",
	)

	// Resolve the LFS object id from the pointer blob stored in the
	// working clone (the working tree holds the real content, not the
	// pointer, so read the committed blob directly).
	var pointerCommand *exec.Cmd = exec.Command(
		"git",
		"-C",
		temporaryDirectory,
		"cat-file",
		"-p",
		"HEAD:big.bin",
	)
	var pointerOutput []byte
	var pointerError error
	pointerOutput, pointerError = pointerCommand.Output()
	if pointerError != nil {
		test.Fatalf("git cat-file HEAD:big.bin: %v", pointerError)
	}
	var oidPrefix string = "oid sha256:"
	for _, line := range strings.Split(string(pointerOutput), "\n") {
		if strings.HasPrefix(line, oidPrefix) {
			return strings.TrimSpace(line[len(oidPrefix):])
		}
	}
	test.Fatalf("LFS pointer not found in output: %q", pointerOutput)
	return ""
}

// TestMirrorLFS_CloneFetchAndPush verifies that a mirror clone followed by
// MirrorLFS and a mirror push transfers both the git refs and the Git LFS
// objects from a source bare repository to a destination bare repository.
func TestMirrorLFS_CloneFetchAndPush(test *testing.T) {
	requireLFS(test)
	if testing.Short() {
		test.Skip("skipping LFS integration test in short mode")
	}

	var requestContext context.Context = context.Background()
	var directory string = test.TempDir()

	var sourceRepository string = filepath.Join(directory, "source.git")
	var oid string = createLFSSourceRepository(test, sourceRepository)

	var destinationRepository string = filepath.Join(directory, "dest.git")
	runGitCommand(
		test,
		directory,
		"init",
		"--bare",
		destinationRepository,
	)

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

	var lfsError error = client.MirrorLFS(
		requestContext,
		cloneDirectory,
		"file://"+sourceRepository,
		"",
		"",
		"file://"+destinationRepository,
		"",
		"",
	)
	if lfsError != nil {
		test.Fatalf("MirrorLFS: %v", lfsError)
	}

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

	// The LFS object must have been pushed to the destination.
	var expectedObjectPath string = filepath.Join(
		destinationRepository,
		"lfs",
		"objects",
		oid[0:2],
		oid[2:4],
		oid,
	)
	var objectInfo os.FileInfo
	var statError error
	objectInfo, statError = os.Stat(expectedObjectPath)
	if statError != nil {
		test.Errorf(
			"Expected LFS object %q in destination: %v",
			oid,
			statError,
		)
	} else if objectInfo.Size() == 0 {
		test.Errorf("LFS object %q is empty in destination", oid)
	}

	// The main ref must have been pushed to the destination.
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

// TestMirrorLFS_NoopForNonLFSRepository verifies that MirrorLFS is a no-op
// for a repository that does not use Git LFS and produces no error.
func TestMirrorLFS_NoopForNonLFSRepository(test *testing.T) {
	requireLFS(test)

	var requestContext context.Context = context.Background()
	var directory string = test.TempDir()

	var sourceRepository string = filepath.Join(directory, "source.git")
	runGitCommand(test, directory, "init", "--bare", sourceRepository)
	var temporaryDirectory string = filepath.Join(directory, "work")
	runGitCommand(test, directory, "clone", sourceRepository, temporaryDirectory)
	runGitCommand(test, temporaryDirectory, "config", "user.email", "test@test.com")
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
	runGitCommand(test, sourceRepository, "symbolic-ref", "HEAD", "refs/heads/main")

	var destinationRepository string = filepath.Join(directory, "dest.git")
	runGitCommand(test, directory, "init", "--bare", destinationRepository)

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

	var lfsError error = client.MirrorLFS(
		requestContext,
		cloneDirectory,
		"file://"+sourceRepository,
		"",
		"",
		"file://"+destinationRepository,
		"",
		"",
	)
	if lfsError != nil {
		test.Fatalf("MirrorLFS on non-LFS repo: %v", lfsError)
	}

	// No LFS object directory should have been created in the clone.
	var cloneLFS string = filepath.Join(cloneDirectory, "lfs")
	if _, statError := os.Stat(cloneLFS); statError == nil {
		test.Errorf("Unexpected lfs directory in non-LFS clone")
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
