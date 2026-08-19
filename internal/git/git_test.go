package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRepoRootFromCommonGitDirHandlesForwardSlashPath(t *testing.T) {
	root, ok := repoRootFromCommonGitDir("C:/Users/runner/AppData/Local/Temp/repo/.git")
	if !ok {
		t.Fatal("expected .git common dir to resolve to a repo root")
	}

	want := filepath.Clean(filepath.FromSlash("C:/Users/runner/AppData/Local/Temp/repo"))
	if root != want {
		t.Fatalf("expected repo root %q, got %q", want, root)
	}
}

func TestGetDefaultBranchFromDetachedLinkedWorktreeUsesMainRepoHead(t *testing.T) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	wtPath := filepath.Join(base, "worktree")

	mustGit(t, "", "init", "--initial-branch=main", repoDir)
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "config", "user.name", "Test")
	mustGit(t, repoDir, "config", "init.defaultBranch", "wrong")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "initial")
	mustGit(t, repoDir, "worktree", "add", "--detach", wtPath, "main")

	branch, err := GetDefaultBranch(wtPath)
	if err != nil {
		t.Fatalf("GetDefaultBranch failed: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected default branch main from main repo HEAD, got %q", branch)
	}
}

func TestFindMainRepoRootFromLinkedWorktree(t *testing.T) {
	base := t.TempDir()
	base, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(base, "repo")
	wtPath := filepath.Join(base, "worktree")

	mustGit(t, "", "init", "--initial-branch=main", repoDir)
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "initial")
	mustGit(t, repoDir, "worktree", "add", "--detach", wtPath, "main")

	mainRoot, err := FindMainRepoRootFrom(wtPath)
	if err != nil {
		t.Fatalf("FindMainRepoRootFrom failed: %v", err)
	}
	if mainRoot != repoDir {
		t.Fatalf("expected main repo root %s, got %s", repoDir, mainRoot)
	}
}

func TestRemoveCleanWorktreeRejectsDirtyWorktree(t *testing.T) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	wtPath := filepath.Join(base, "worktree")

	mustGit(t, "", "init", "--initial-branch=main", repoDir)
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "initial")
	mustGit(t, repoDir, "worktree", "add", "--detach", wtPath, "main")

	dirtyPath := filepath.Join(wtPath, "uncommitted.txt")
	if err := os.WriteFile(dirtyPath, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveCleanWorktree(repoDir, wtPath); err == nil {
		t.Fatal("expected clean worktree removal to reject dirty worktree")
	}
	if _, err := os.Stat(dirtyPath); err != nil {
		t.Fatalf("expected dirty worktree to remain: %v", err)
	}
}

func TestIsHeadMergedIntoRef(t *testing.T) {
	tests := []struct {
		name                   string
		ordinaryMerge          bool
		squashMerge            bool
		laterUnrelated         bool
		targetFeatureContent   string
		emptyFeatureCommit     bool
		revertedFeatureContent bool
		wantMerged             bool
	}{
		{name: "ordinary ancestry merge", ordinaryMerge: true, wantMerged: true},
		{name: "squash merge", squashMerge: true, wantMerged: true},
		{name: "squash merge followed by unrelated target commit", squashMerge: true, laterUnrelated: true, wantMerged: true},
		{name: "squash merge missing final feature content", squashMerge: true, targetFeatureContent: "one\n", wantMerged: false},
		{name: "unique unmerged content", wantMerged: false},
		{name: "empty feature commit", emptyFeatureCommit: true, wantMerged: false},
		{name: "feature content fully reverted", revertedFeatureContent: true, wantMerged: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoDir := t.TempDir()
			mustGit(t, "", "init", "--initial-branch=main", repoDir)
			mustGit(t, repoDir, "config", "user.email", "test@test.com")
			mustGit(t, repoDir, "config", "user.name", "Test")

			if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("base\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			mustGit(t, repoDir, "add", ".")
			mustGit(t, repoDir, "commit", "-m", "initial")
			mustGit(t, repoDir, "checkout", "-b", "feature")

			switch {
			case tt.emptyFeatureCommit:
				mustGit(t, repoDir, "commit", "--allow-empty", "-m", "empty feature commit")
			case tt.revertedFeatureContent:
				if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("feature\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				mustGit(t, repoDir, "commit", "-am", "feature change")
				if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("base\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				mustGit(t, repoDir, "commit", "-am", "revert feature change")
			default:
				if err := os.WriteFile(filepath.Join(repoDir, "feature.txt"), []byte("one\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				mustGit(t, repoDir, "add", "feature.txt")
				mustGit(t, repoDir, "commit", "-m", "feature one")
				if err := os.WriteFile(filepath.Join(repoDir, "feature.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				mustGit(t, repoDir, "commit", "-am", "feature two")
			}

			mustGit(t, repoDir, "checkout", "main")
			switch {
			case tt.ordinaryMerge:
				mustGit(t, repoDir, "merge", "--no-ff", "feature", "-m", "merge feature")
			case tt.squashMerge:
				mustGit(t, repoDir, "merge", "--squash", "feature")
				if tt.targetFeatureContent != "" {
					if err := os.WriteFile(filepath.Join(repoDir, "feature.txt"), []byte(tt.targetFeatureContent), 0o644); err != nil {
						t.Fatal(err)
					}
					mustGit(t, repoDir, "add", "feature.txt")
				}
				mustGit(t, repoDir, "commit", "-m", "squash feature")
			}
			if tt.laterUnrelated {
				if err := os.WriteFile(filepath.Join(repoDir, "unrelated.txt"), []byte("later\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				mustGit(t, repoDir, "add", "unrelated.txt")
				mustGit(t, repoDir, "commit", "-m", "unrelated target change")
			}
			mustGit(t, repoDir, "checkout", "feature")

			merged, err := IsHeadMergedIntoRef(repoDir, "refs/heads/main")
			if err != nil {
				t.Fatalf("IsHeadMergedIntoRef failed: %v", err)
			}
			if merged != tt.wantMerged {
				t.Fatalf("expected merged=%t, got %t", tt.wantMerged, merged)
			}
		})
	}
}

func TestIsHeadMergedIntoRefFailsClosedWhenTargetCannotBeVerified(t *testing.T) {
	repoDir := t.TempDir()
	mustGit(t, "", "init", "--initial-branch=main", repoDir)
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "initial")

	if _, err := IsHeadMergedIntoRef(repoDir, "refs/heads/missing"); err == nil {
		t.Fatal("expected merge verification error for missing target ref")
	}
}

func TestRootedSeedWriteRejectsEscapingSymlinkAfterRootOpen(t *testing.T) {
	worktree := t.TempDir()
	outside := t.TempDir()
	root, err := os.OpenRoot(worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := os.Symlink(outside, filepath.Join(worktree, "config")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rel := filepath.FromSlash("config/settings.env")
	if err := root.MkdirAll(filepath.Dir(rel), 0o755); err == nil {
		t.Fatal("expected rooted mkdir to reject an escaping symlink")
	}
	if err := writeSeedFile(root, rel, []byte("seeded\n"), 0o644); err == nil {
		t.Fatal("expected rooted write to reject an escaping symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "settings.env")); !os.IsNotExist(err) {
		t.Fatalf("outside destination was created: %v", err)
	}
}

func TestRootedSeedWriteRejectsInRootSymlinkToTrackedFile(t *testing.T) {
	worktree := t.TempDir()
	tracked := filepath.Join(worktree, "tracked.env")
	if err := os.WriteFile(tracked, []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	seeded := filepath.Join(worktree, ".env")
	if err := os.Symlink("tracked.env", seeded); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := writeSeedFile(root, ".env", []byte("seeded\n"), 0o644); err == nil {
		t.Fatal("expected rooted write to reject an in-root symlink")
	}
	got, err := os.ReadFile(tracked)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "tracked\n" {
		t.Fatalf("tracked file was modified: %q", got)
	}
}

func TestRootedSeedWriteRejectsInRootParentSymlinkToTrackedFile(t *testing.T) {
	worktree := t.TempDir()
	tracked := filepath.Join(worktree, "settings.env")
	if err := os.WriteFile(tracked, []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := os.Symlink(".", filepath.Join(worktree, "config")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rel := filepath.FromSlash("config/settings.env")
	if err := writeSeedFile(root, rel, []byte("seeded\n"), 0o644); err == nil {
		t.Fatal("expected rooted write to reject an in-root parent symlink")
	}
	got, err := os.ReadFile(tracked)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "tracked\n" {
		t.Fatalf("tracked file was modified: %q", got)
	}
}

func TestOpenRootedParentKeepsOriginalDirectoryAfterPathReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not permit renaming an open directory")
	}
	worktree := t.TempDir()
	original := filepath.Join(worktree, "config")
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	parent, name, closeParents, err := openRootedParent(root, filepath.FromSlash("config/settings.env"))
	if err != nil {
		t.Fatalf("openRootedParent failed: %v", err)
	}
	defer closeParents()

	renamed := filepath.Join(worktree, "config.original")
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}

	dst, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dst.Write([]byte("seeded\n")); err != nil {
		dst.Close()
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(renamed, "settings.env")); err != nil {
		t.Fatalf("original directory did not receive the write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(original, "settings.env")); !os.IsNotExist(err) {
		t.Fatalf("replacement directory received the write: %v", err)
	}
}

func TestExcludedIncludeSubtreeAllowsNestedDirectoryPattern(t *testing.T) {
	if excludedIncludeSubtree("foo/bar/file.txt", "!foo/\nbar/\n") {
		t.Fatal("nested directory pattern should re-include the file")
	}
	if !excludedIncludeSubtree("foo/baz/file.txt", "!foo/\nbar/\n") {
		t.Fatal("unmatched directory should remain excluded")
	}
	if !excludedIncludeSubtree("foo/bar", "!foo/\nbar/\n") {
		t.Fatal("directory pattern should not match a file basename")
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}
