package mirror_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/martmull/github-to-gitlab-mirror/internal/mirror"
)

func TestGetRefs_LocalBareRepo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Create a bare repo with one commit
	bare := filepath.Join(dir, "source.git")
	runGit(t, dir, "init", "--bare", bare)

	// Clone the bare repo, make a commit, push
	tmp := filepath.Join(dir, "tmp")
	runGit(t, dir, "clone", bare, tmp)
	runGit(t, tmp, "config", "user.email", "test@test.com")
	runGit(t, tmp, "config", "user.name", "Test")
	runGit(t, tmp, "checkout", "-b", "main")
	writeFile(t, filepath.Join(tmp, "README.md"), "# hello")
	runGit(t, tmp, "add", "README.md")
	runGit(t, tmp, "commit", "-m", "initial")
	runGit(t, tmp, "push", "origin", "main")
	runGit(t, bare, "symbolic-ref", "HEAD", "refs/heads/main")

	client := mirror.New()
	refs, err := client.GetRefs(ctx, "file://"+bare, "", "")
	if err != nil {
		t.Fatalf("GetRefs: %v", err)
	}

	if _, ok := refs["refs/heads/main"]; !ok {
		t.Errorf("expected refs/heads/main to exist, got %v", refs)
	}
}

func TestMirrorClonePush(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Source bare repo
	src := filepath.Join(dir, "source.git")
	runGit(t, dir, "init", "--bare", src)

	// Clone, commit, push
	tmp := filepath.Join(dir, "tmp")
	runGit(t, dir, "clone", src, tmp)
	runGit(t, tmp, "config", "user.email", "test@test.com")
	runGit(t, tmp, "config", "user.name", "Test")
	runGit(t, tmp, "checkout", "-b", "main")
	writeFile(t, filepath.Join(tmp, "README.md"), "# hello")
	runGit(t, tmp, "add", "README.md")
	runGit(t, tmp, "commit", "-m", "initial")
	runGit(t, tmp, "push", "origin", "main")
	runGit(t, src, "symbolic-ref", "HEAD", "refs/heads/main")

	// Destination bare repo
	dst := filepath.Join(dir, "dest.git")
	runGit(t, dir, "init", "--bare", dst)

	// Mirror clone
	client := mirror.New()
	cloneDir := filepath.Join(dir, "clone.git")
	if err := client.MirrorClone(ctx, "file://"+src, "", "", cloneDir); err != nil {
		t.Fatalf("MirrorClone: %v", err)
	}

	// Mirror push
	if err := client.MirrorPush(ctx, cloneDir, "file://"+dst, "", ""); err != nil {
		t.Fatalf("MirrorPush: %v", err)
	}

	// Verify dst has the ref
	refs, err := client.GetRefs(ctx, "file://"+dst, "", "")
	if err != nil {
		t.Fatalf("GetRefs on dst: %v", err)
	}
	if _, ok := refs["refs/heads/main"]; !ok {
		t.Errorf("expected refs/heads/main in dst, got %v", refs)
	}
}

func runGit(t *testing.T, workDir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}
