package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const testTag = "v0.1.0-alpha.1"

func root(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func env(overrides ...string) []string {
	m := map[string]string{}
	for _, item := range append(os.Environ(), overrides...) {
		k, v, ok := strings.Cut(item, "=")
		if ok {
			m[k] = v
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

func run(t *testing.T, dir string, e []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env(e...)
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func verify(t *testing.T, e []string, args ...string) (string, error) {
	t.Helper()
	return run(t, root(t), e, "./scripts/verify-release.sh", args...)
}

func retrieve(t *testing.T, e []string, args ...string) (string, error) {
	t.Helper()
	return run(t, root(t), e, "./scripts/retrieve-release-draft.sh", args...)
}

func TestPackageGuardsAndUnpackedSmoke(t *testing.T) {
	for _, bad := range []string{"v1.0.0", "v1.2.3-alpha.1$(printf${IFS}INJECTED)", "v1.2.3-alpha.1x", "v1.2.3-alpha.-1"} {
		out, err := run(t, root(t), nil, "./scripts/package-release.sh", "--tag", bad, "--output", filepath.Join(t.TempDir(), "out"))
		if err == nil || !strings.Contains(out, "experimental tag required") {
			t.Fatalf("bad tag accepted %q: %v\n%s", bad, err, out)
		}
	}
	for _, kind := range []string{"directory", "dangling-symlink"} {
		t.Run(kind, func(t *testing.T) {
			repo, _ := packageRepo(t)
			output := filepath.Join(t.TempDir(), "out")
			if kind == "directory" {
				mustMkdir(t, output)
			} else if err := os.Symlink("missing", output); err != nil {
				t.Fatal(err)
			}
			out, err := run(t, repo, nil, "./scripts/package-release.sh", "--tag", testTag, "--output", output)
			if err == nil || !strings.Contains(out, "refusing existing output") {
				t.Fatalf("existing output accepted: %v\n%s", err, out)
			}
		})
	}
	t.Run("unsupported-platform", func(t *testing.T) {
		repo, _ := packageRepo(t)
		bin := t.TempDir()
		output := filepath.Join(t.TempDir(), "out")
		mustWrite(t, filepath.Join(bin, "uname"), []byte("#!/bin/sh\ncase \"$1\" in -s) echo Plan9;; -m) echo mips;; esac\n"), 0755)
		out, err := run(t, repo, []string{"PATH=" + bin + ":" + os.Getenv("PATH")}, "./scripts/package-release.sh", "--tag", testTag, "--output", output)
		if err == nil || !strings.Contains(out, "unsupported native release platform") || exists(output) {
			t.Fatalf("unsupported target wrote output: %v\n%s", err, out)
		}
	})
	t.Run("ambient-cross-target", func(t *testing.T) {
		repo, _ := packageRepo(t)
		output := filepath.Join(t.TempDir(), "out")
		out, err := run(t, repo, []string{"GOOS=not-native"}, "./scripts/package-release.sh", "--tag", testTag, "--output", output)
		if err == nil || !strings.Contains(out, "ambient Go cross-compilation") || exists(output) {
			t.Fatalf("cross target wrote output: %v\n%s", err, out)
		}
	})
	t.Run("unpacked-bytes", func(t *testing.T) {
		repo, log := packageRepo(t)
		output := filepath.Join(t.TempDir(), "out")
		out, err := run(t, repo, []string{"EXEC_LOG=" + log}, "./scripts/package-release.sh", "--tag", testTag, "--output", output)
		if err != nil {
			t.Fatalf("package failed: %v\n%s", err, out)
		}
		body := mustRead(t, log)
		if !strings.Contains(body, "/unpacked/checkpoint") || !strings.Contains(body, "/unpacked/agent-governance-demo") {
			t.Fatalf("staged rather than unpacked commands ran:\n%s", body)
		}
		entries, _ := os.ReadDir(output)
		if len(entries) != 2 {
			t.Fatalf("got %d package outputs", len(entries))
		}
	})
}

func TestReleaseSourceGuardRejectsUntrustedEvents(t *testing.T) {
	repo := t.TempDir()
	mustWrite(t, filepath.Join(repo, "README"), []byte("x"), 0644)
	mustWrite(t, filepath.Join(repo, "scripts/release-source-guard.sh"), []byte(mustRead(t, filepath.Join(root(t), "scripts/release-source-guard.sh"))), 0755)
	commit := initRepo(t, repo)
	mustRun(t, repo, "git", "tag", "-am", "release", testTag, commit)
	mustRun(t, repo, "git", "update-ref", "refs/remotes/origin/main", commit)
	base := []string{"ACTOR=ianhundere", "RERUN_ACTOR=ianhundere", "REPOSITORY=liatrio/agent-governance-evidence", "TAG=" + testTag, "SHA=" + commit}
	out, err := run(t, repo, base, "./scripts/release-source-guard.sh")
	if err != nil || strings.TrimSpace(out) != commit {
		t.Fatalf("valid event failed: %v\n%s", err, out)
	}
	for _, tc := range []struct{ name, change, want string }{
		{"actor", "ACTOR=mallory", "unauthorized actor"}, {"rerun", "RERUN_ACTOR=mallory", "unauthorized rerun actor"},
		{"repository", "REPOSITORY=mallory/repo", "unexpected repository"}, {"injection", "TAG=v1.2.3-alpha.1$(echo bad)", "experimental tag"},
		{"moved-tag", "SHA=" + strings.Repeat("0", 40), "tag target changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := run(t, repo, append(base, tc.change), "./scripts/release-source-guard.sh")
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("untrusted event accepted: %v\n%s", err, out)
			}
		})
	}
	t.Run("off-main", func(t *testing.T) {
		offRepo := t.TempDir()
		mustWrite(t, filepath.Join(offRepo, "README"), []byte("reviewed\n"), 0644)
		mustWrite(t, filepath.Join(offRepo, "scripts/release-source-guard.sh"), []byte(mustRead(t, filepath.Join(root(t), "scripts/release-source-guard.sh"))), 0755)
		mainCommit := initRepo(t, offRepo)
		mustRun(t, offRepo, "git", "update-ref", "refs/remotes/origin/main", mainCommit)
		mustWrite(t, filepath.Join(offRepo, "README"), []byte("unreviewed release commit\n"), 0644)
		mustRun(t, offRepo, "git", "add", "README")
		mustRun(t, offRepo, "git", "commit", "-qm", "unreviewed")
		out, err := run(t, offRepo, nil, "git", "rev-parse", "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		offCommit := strings.TrimSpace(out)
		mustRun(t, offRepo, "git", "tag", "-am", "release", testTag, offCommit)
		offEnv := []string{"ACTOR=ianhundere", "RERUN_ACTOR=ianhundere", "REPOSITORY=liatrio/agent-governance-evidence", "TAG=" + testTag, "SHA=" + offCommit}
		out, err = run(t, offRepo, offEnv, "./scripts/release-source-guard.sh")
		if err == nil || !strings.Contains(out, "tag commit is not on reviewed main") {
			t.Fatalf("matching tag/SHA outside reviewed main passed: %v\n%s", err, out)
		}
	})
}

func TestAggregateAndDraftWorkflowGuards(t *testing.T) {
	t.Run("aggregate-valid", func(t *testing.T) {
		incoming := incoming(t)
		output := filepath.Join(t.TempDir(), "out")
		out, err := run(t, root(t), nil, "./scripts/aggregate-release-assets.py", "--tag", testTag, "--incoming", incoming, "--output", output)
		if err != nil {
			t.Fatalf("aggregate failed: %v\n%s", err, out)
		}
		entries, _ := os.ReadDir(output)
		if len(entries) != 4 {
			t.Fatalf("got %d aggregate files", len(entries))
		}
	})
	t.Run("aggregate-valid-recompressed-source", func(t *testing.T) {
		incoming := incoming(t)
		output := filepath.Join(t.TempDir(), "out")
		body := gunzipBytes(t, filepath.Join(incoming, "linux-amd64", sourceName()))
		writeGzipNamed(t, filepath.Join(incoming, "darwin-arm64", sourceName()), body, "darwin")
		out, err := run(t, root(t), nil, "./scripts/aggregate-release-assets.py", "--tag", testTag, "--incoming", incoming, "--output", output)
		if err != nil {
			t.Fatalf("aggregate rejected matching source payloads: %v\n%s", err, out)
		}
	})
	for _, tc := range []struct {
		name, want string
		mutate     func(*testing.T, string, string)
	}{
		{"extra-dir", "unexpected same-run artifact directories", func(t *testing.T, d, o string) { mustMkdir(t, filepath.Join(d, "extra")) }},
		{"extra-file", "unexpected same-run artifact set", func(t *testing.T, d, o string) {
			mustWrite(t, filepath.Join(d, "linux-amd64", "extra"), []byte("x"), 0644)
		}},
		{"file-symlink", "must be regular files", func(t *testing.T, d, o string) {
			p := filepath.Join(d, "linux-amd64", native("linux-amd64"))
			mustRemove(t, p)
			if err := os.Symlink(sourceName(), p); err != nil {
				t.Fatal(err)
			}
		}},
		{"source-drift", "different source archives", func(t *testing.T, d, o string) {
			writeGzip(t, filepath.Join(d, "darwin-arm64", sourceName()), []byte("bad"))
		}},
		{"existing-output", "refusing existing aggregate output", func(t *testing.T, d, o string) { mustMkdir(t, o) }},
		{"dangling-output", "refusing existing aggregate output", func(t *testing.T, d, o string) {
			if err := os.Symlink("missing", o); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := incoming(t)
			o := filepath.Join(t.TempDir(), "out")
			tc.mutate(t, d, o)
			out, err := run(t, root(t), nil, "./scripts/aggregate-release-assets.py", "--tag", testTag, "--incoming", d, "--output", o)
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("aggregate accepted bad input: %v\n%s", err, out)
			}
		})
	}

	for _, tc := range []struct {
		name, mode, want string
		mutate           func(*testing.T, string)
		create           bool
	}{
		{"create", "missing", "", nil, true}, {"existing", "existing", "release already exists", nil, false},
		{"lookup-error", "error", "without a confirmed 404", nil, false},
		{"extra", "missing", "asset set is not exact", func(t *testing.T, d string) { mustWrite(t, filepath.Join(d, "extra"), []byte("x"), 0644) }, false},
		{"symlink", "missing", "must be regular files", func(t *testing.T, d string) {
			p := filepath.Join(d, "SHA256SUMS")
			mustRemove(t, p)
			if err := os.Symlink("build-provenance.sigstore.json", p); err != nil {
				t.Fatal(err)
			}
		}, false},
	} {
		t.Run("draft-"+tc.name, func(t *testing.T) {
			assets := draftAssets(t)
			if tc.mutate != nil {
				tc.mutate(t, assets)
			}
			mock, log := draftMock(t)
			out, err := run(t, root(t), []string{"PATH=" + mock + ":" + os.Getenv("PATH"), "GH_MODE=" + tc.mode, "GH_LOG=" + log, "ASSETS_DIR=" + assets}, "./scripts/create-release-draft.sh", "--tag", testTag, "--assets-dir", assets, "--repository", "liatrio/agent-governance-evidence")
			if tc.create {
				if err != nil {
					t.Fatalf("create failed: %v\n%s", err, out)
				}
			} else if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("draft guard failed: %v\n%s", err, out)
			}
			created := strings.Contains(mustRead(t, log), "release create")
			if created != tc.create {
				t.Fatalf("create=%v want %v", created, tc.create)
			}
			if tc.create {
				expected := "api --include repos/liatrio/agent-governance-evidence/releases/tags/" + testTag + "\n" +
					"release create " + testTag + " " + filepath.Join(assets, native("linux-amd64")) + " " +
					filepath.Join(assets, native("darwin-arm64")) + " " + filepath.Join(assets, sourceName()) + " " +
					filepath.Join(assets, "SHA256SUMS") + " " + filepath.Join(assets, "build-provenance.sigstore.json") +
					" --repo liatrio/agent-governance-evidence --draft --prerelease --verify-tag --title " + testTag + "\n"
				if got := mustRead(t, log); got != expected {
					t.Fatalf("draft creation argv mismatch:\n%s", got)
				}
			}
		})
	}
}

func TestRetrieveDraftReleaseOnlyDownloadsAndWritesMetadata(t *testing.T) {
	downloads := draftAssets(t)
	outDir := filepath.Join(t.TempDir(), "out")
	metadata := filepath.Join(t.TempDir(), "release-metadata.json")
	mock, ghlog, execlog := retrieveMock(t)
	env := []string{
		"PATH=" + mock + ":" + os.Getenv("PATH"),
		"GH_LOG=" + ghlog,
		"EXEC_LOG=" + execlog,
		"DOWNLOADS_DIR=" + downloads,
		"RELEASE_JSON=" + releaseJSON(t, downloads, true, true, false, testTag, nil),
	}
	out, err := retrieve(t, env, "--tag", testTag, "--assets-dir", outDir, "--metadata-file", metadata, "--repository", "liatrio/agent-governance-evidence")
	if err != nil {
		t.Fatalf("retrieve failed: %v\n%s", err, out)
	}
	if got := mustRead(t, execlog); got != "" {
		t.Fatalf("retrieve executed packaged tools:\n%s", got)
	}
	body := mustRead(t, ghlog)
	if strings.Count(body, "release view "+testTag+" -R liatrio/agent-governance-evidence --json databaseId,tagName,isDraft,isPrerelease,assets") != 1 {
		t.Fatalf("release view missing:\n%s", body)
	}
	if strings.Count(body, "release download "+testTag+" --repo liatrio/agent-governance-evidence --dir "+outDir+" --pattern *") != 1 {
		t.Fatalf("release download missing:\n%s", body)
	}
	if strings.Contains(body, "release edit ") || strings.Contains(body, "release create ") {
		t.Fatalf("retrieve attempted publication:\n%s", body)
	}
	for _, name := range names() {
		if mustRead(t, filepath.Join(outDir, name)) != name {
			t.Fatalf("download mismatch for %s", name)
		}
	}
	manifest := mustRead(t, metadata)
	for _, want := range []string{`"draft": true`, `"prerelease": true`, `"tag": "` + testTag + `"`, `"release_id": 123`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("metadata missing %q:\n%s", want, manifest)
		}
	}
}

func TestAcceptanceEnvironmentAndWorkflowLayout(t *testing.T) {
	for _, tc := range []struct{ script, kernel, arch, msg string }{{"scripts/ci.sh", "Linux", "x86_64", "ci.sh requires Python 3.13"}, {"scripts/regenerate-producers.sh", "Darwin", "arm64", "producer regeneration requires Python 3.13"}} {
		t.Run(filepath.Base(tc.script), func(t *testing.T) {
			bin := t.TempDir()
			mustWrite(t, filepath.Join(bin, "uname"), []byte(fmt.Sprintf("#!/bin/sh\ncase \"$1\" in -s) echo %s;; -m) echo %s;; esac\n", tc.kernel, tc.arch)), 0755)
			mustWrite(t, filepath.Join(bin, "python3"), []byte("#!/bin/sh\necho '"+tc.msg+"' >&2\nexit 1\n"), 0755)
			out, err := run(t, root(t), []string{"PATH=" + bin + ":" + os.Getenv("PATH")}, "./"+tc.script)
			if err == nil || !strings.Contains(out, tc.msg) {
				t.Fatalf("wrong python accepted: %v\n%s", err, out)
			}
		})
	}
	for _, tc := range []struct {
		name, body, path, want string
		ok                     bool
	}{{"clean", "PASS\n", "", "", true}, {"skip", "--- SKIP: TestX\n", "", "skipped Go tests", false}, {"reader", "", filepath.Join(t.TempDir(), "missing"), "unable to inspect", false}} {
		t.Run("test-log-"+tc.name, func(t *testing.T) {
			p := tc.path
			if p == "" {
				p = filepath.Join(t.TempDir(), "log")
				mustWrite(t, p, []byte(tc.body), 0644)
			}
			out, err := run(t, root(t), nil, "./scripts/check-go-test-log.py", p)
			if tc.ok {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("log guard failed: %v\n%s", err, out)
			}
		})
	}
	body := mustRead(t, filepath.Join(root(t), ".github/workflows/verify-release.yml"))
	for _, s := range []string{
		"retrieve-draft-release:",
		"contents: write",
		"name: draft-release-retrieval",
		"inputs.phase == 'draft'",
		"inputs.phase == 'published'",
		"--metadata-file",
		"--source-dir \"$GITHUB_WORKSPACE\"",
		"gh release download \"$TAG\" --repo \"$GITHUB_REPOSITORY\" --dir \"${{ steps.prep.outputs.assets }}\" --pattern '*'",
	} {
		if !strings.Contains(body, s) {
			t.Fatalf("workflow lacks clean asset arrangement %q", s)
		}
	}
	if strings.Contains(body, "mkdir assets") {
		t.Fatal("workflow dirties checkout")
	}
}

func TestVerifyRejectsChecksumAndLocalAssetFailuresBeforeGH(t *testing.T) {
	source, commit := gitSource(t)
	for _, tc := range []struct {
		name, want string
		mutate     func(*testing.T, string)
	}{
		{"missing", "asset set is not exact", func(t *testing.T, d string) { mustRemove(t, filepath.Join(d, "SHA256SUMS")) }},
		{"extra", "asset set is not exact", func(t *testing.T, d string) { mustWrite(t, filepath.Join(d, "extra"), []byte("x"), 0644) }},
		{"directory", "must be regular files", func(t *testing.T, d string) { p := filepath.Join(d, "SHA256SUMS"); mustRemove(t, p); mustMkdir(t, p) }},
		{"symlink", "must be regular files", func(t *testing.T, d string) {
			p := filepath.Join(d, "SHA256SUMS")
			mustRemove(t, p)
			if err := os.Symlink("build-provenance.sigstore.json", p); err != nil {
				t.Fatal(err)
			}
		}},
		{"malformed-hash", "malformed checksum", func(t *testing.T, d string) {
			mutateSums(t, d, func(lines []string) []string { lines[0] = "0" + lines[0][64:]; return lines })
		}},
		{"wrong-hash", "checksum mismatch", func(t *testing.T, d string) {
			mutateSums(t, d, func(lines []string) []string { lines[0] = strings.Repeat("0", 64) + lines[0][64:]; return lines })
		}},
		{"duplicate-name", "names are not exact", func(t *testing.T, d string) {
			mutateSums(t, d, func(lines []string) []string { lines[2] = lines[2][:64] + lines[0][64:]; return lines })
		}},
		{"missing-name", "names are not exact", func(t *testing.T, d string) { mutateSums(t, d, func(lines []string) []string { return lines[:2] }) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := assets(t, source, commit)
			tc.mutate(t, d)
			mock, log := rejectGH(t)
			out, err := verify(t, []string{"PATH=" + mock + ":" + os.Getenv("PATH"), "GH_LOG=" + log}, verifyArgs(d, source, commit, "draft")...)
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("bad asset passed wrong guard: %v\n%s", err, out)
			}
			assertEmpty(t, log)
		})
	}
}

func TestVerifyRejectsArchiveMutationsCausally(t *testing.T) {
	source, commit := gitSource(t)
	for _, tc := range []struct {
		name, want string
		source     bool
		mutate     func(*testing.T, []entry) []entry
	}{
		{"space-prefix", "unsafe source", true, func(t *testing.T, e []entry) []entry {
			return append(e, entry{"elsewhere agent-governance-evidence_" + testTag + "/injected", []byte("x"), 0644, tar.TypeReg, ""})
		}},
		{"traversal", "unsafe source", true, func(t *testing.T, e []entry) []entry {
			return append(e, entry{"agent-governance-evidence_" + testTag + "/nested/../../outside", []byte("x"), 0644, tar.TypeReg, ""})
		}},
		{"absolute", "unsafe source", true, func(t *testing.T, e []entry) []entry {
			return append(e, entry{"/outside", []byte("x"), 0644, tar.TypeReg, ""})
		}},
		{"source-duplicate", "duplicate source", true, func(t *testing.T, e []entry) []entry { return append(e, find(t, e, "/README.md")) }},
		{"source-symlink", "unsafe source", true, func(t *testing.T, e []entry) []entry {
			return change(t, e, "/README.md", func(x entry) entry { x.kind = tar.TypeSymlink; x.link = "LICENSE"; x.body = nil; return x })
		}},
		{"source-hardlink", "unsafe source", true, func(t *testing.T, e []entry) []entry {
			return change(t, e, "/README.md", func(x entry) entry { x.kind = tar.TypeLink; x.link = "LICENSE"; x.body = nil; return x })
		}},
		{"source-special", "unsafe source", true, func(t *testing.T, e []entry) []entry {
			return change(t, e, "/README.md", func(x entry) entry { x.kind = tar.TypeFifo; x.body = nil; return x })
		}},
		{"source-missing", "does not exactly match", true, func(t *testing.T, e []entry) []entry { return remove(t, e, "/README.md") }},
		{"source-extra", "does not exactly match", true, func(t *testing.T, e []entry) []entry {
			return append(e, entry{"agent-governance-evidence_" + testTag + "/extra", []byte("x"), 0644, tar.TypeReg, ""})
		}},
		{"source-altered", "does not exactly match", true, func(t *testing.T, e []entry) []entry {
			return change(t, e, "/README.md", func(x entry) entry { x.body = []byte("bad\n"); return x })
		}},
		{"source-mode", "does not exactly match", true, func(t *testing.T, e []entry) []entry {
			return change(t, e, "/README.md", func(x entry) entry { x.mode = 0755; return x })
		}},
		{"binary-extra", "bad archive membership", false, func(t *testing.T, e []entry) []entry {
			return append(e, entry{"has space", []byte("x"), 0755, tar.TypeReg, ""})
		}},
		{"binary-duplicate", "bad archive membership", false, func(t *testing.T, e []entry) []entry { return append(e, e[0]) }},
		{"binary-symlink", "unsafe binary archive", false, func(t *testing.T, e []entry) []entry {
			e[0].kind = tar.TypeSymlink
			e[0].link = "checkpoint"
			e[0].body = nil
			return e
		}},
		{"binary-mode", "bad archive modes", false, func(t *testing.T, e []entry) []entry { e[0].mode = 0644; return e }},
		{"license", "platform LICENSE differs", false, func(t *testing.T, e []entry) []entry { e[3].body = []byte("bad\n"); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := assets(t, source, commit)
			var e []entry
			var path string
			if tc.source {
				e = sourceEntries(t, source, commit)
				path = filepath.Join(d, sourceName())
			} else {
				e = platformEntries()
				path = filepath.Join(d, native("linux-amd64"))
			}
			writeTar(t, path, tc.mutate(t, e))
			writeSums(t, d)
			mock, log := rejectGH(t)
			out, err := verify(t, []string{"PATH=" + mock + ":" + os.Getenv("PATH"), "GH_LOG=" + log}, verifyArgs(d, source, commit, "draft")...)
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("archive mutation reached wrong guard: %v\n%s", err, out)
			}
			assertEmpty(t, log)
		})
	}
}

func TestVerifyRejectsDirtyAndMismatchedSource(t *testing.T) {
	source, commit := gitSource(t)
	d := assets(t, source, commit)
	mustWrite(t, filepath.Join(source, "dirty"), []byte("x"), 0644)
	mock, log := rejectGH(t)
	out, err := verify(t, []string{"PATH=" + mock + ":" + os.Getenv("PATH"), "GH_LOG=" + log}, verifyArgs(d, source, commit, "draft")...)
	if err == nil || !strings.Contains(out, "checkout is dirty") {
		t.Fatalf("dirty source passed: %v\n%s", err, out)
	}
	assertEmpty(t, log)
	source, commit = gitSource(t)
	d = assets(t, source, commit)
	mock, log = rejectGH(t)
	out, err = verify(t, []string{"PATH=" + mock + ":" + os.Getenv("PATH"), "GH_LOG=" + log}, verifyArgs(d, source, strings.Repeat("0", 40), "draft")...)
	if err == nil || !strings.Contains(out, "commit mismatch") {
		t.Fatalf("mismatch passed: %v\n%s", err, out)
	}
	assertEmpty(t, log)
}

func TestVerifyExactAttestationsDraftAndPublishedProofs(t *testing.T) {
	source, commit := gitSource(t)
	d := assets(t, source, commit)
	metadata := filepath.Join(t.TempDir(), "release-metadata.json")
	mustWrite(t, metadata, []byte(draftMetadataJSON(t, d, true, true, testTag, nil)), 0644)
	mock, ghlog, execlog := ghMock(t)
	release := releaseJSON(t, d, true, true, false, testTag, nil)
	e := verificationEnv(mock, ghlog, execlog, d, commit, release)
	out, err := verify(t, e, append(verifyArgs(d, source, commit, "draft"), "--metadata-file", metadata)...)
	if err != nil {
		t.Fatalf("valid draft failed: %v\n%s", err, out)
	}
	body := mustRead(t, ghlog)
	assertExactAttestations(t, body, d)
	for _, forbidden := range []string{"release view ", "release verify ", "api repos/liatrio/agent-governance-evidence/git/"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("draft called forbidden metadata path %q:\n%s", forbidden, body)
		}
	}
	t.Run("attestation-failure", func(t *testing.T) {
		d := assets(t, source, commit)
		metadata := filepath.Join(t.TempDir(), "release-metadata.json")
		mustWrite(t, metadata, []byte(draftMetadataJSON(t, d, true, true, testTag, nil)), 0644)
		m, g, x := ghMock(t)
		e := append(verificationEnv(m, g, x, d, commit, releaseJSON(t, d, true, true, false, testTag, nil)), "GH_FAIL=attestation")
		out, err := verify(t, e, append(verifyArgs(d, source, commit, "draft"), "--metadata-file", metadata)...)
		if err == nil || !strings.Contains(out, "attestation failure") {
			t.Fatalf("attestation failure passed: %v\n%s", err, out)
		}
		assertEmpty(t, x)
	})

	truncate(t, ghlog)
	truncate(t, execlog)
	published := releaseJSON(t, d, false, true, true, testTag, nil)
	e = verificationEnv(mock, ghlog, execlog, d, commit, published)
	out, err = verify(t, e, verifyArgs(d, source, commit, "published")...)
	if err != nil {
		t.Fatalf("published failed: %v\n%s", err, out)
	}
	body = mustRead(t, ghlog)
	assertExactAttestations(t, body, d)
	if strings.Count(body, "release verify "+testTag+" -R liatrio/agent-governance-evidence") != 1 {
		t.Fatalf("release proof wrong:\n%s", body)
	}
	for _, name := range names() {
		line := "release verify-asset " + testTag + " " + filepath.Join(d, name) + " -R liatrio/agent-governance-evidence"
		if strings.Count(body, line) != 1 {
			t.Fatalf("missing distinct proof %s:\n%s", name, body)
		}
	}
	t.Run("published-attestation-failure", func(t *testing.T) {
		d := assets(t, source, commit)
		m, g, x := ghMock(t)
		e := append(verificationEnv(m, g, x, d, commit, releaseJSON(t, d, false, true, true, testTag, nil)), "GH_FAIL=attestation")
		out, err := verify(t, e, verifyArgs(d, source, commit, "published")...)
		if err == nil || !strings.Contains(out, "attestation failure") {
			t.Fatalf("published attestation failure passed: %v\n%s", err, out)
		}
		assertEmpty(t, x)
	})

	for _, failure := range append([]string{"release"}, names()...) {
		t.Run("failure-"+failure, func(t *testing.T) {
			d := assets(t, source, commit)
			m, g, x := ghMock(t)
			e := append(verificationEnv(m, g, x, d, commit, releaseJSON(t, d, false, true, true, testTag, nil)), "GH_FAIL="+failure)
			out, err := verify(t, e, verifyArgs(d, source, commit, "published")...)
			if err == nil || !strings.Contains(out, "immutable verification failure") {
				t.Fatalf("proof failure passed: %v\n%s", err, out)
			}
			assertEmpty(t, x)
		})
	}
}

func TestAttestationOracleRejectsEveryWrongBinding(t *testing.T) {
	assetsDir := t.TempDir()
	commit := strings.Repeat("a", 40)
	base := []string{
		"attestation", "verify", filepath.Join(assetsDir, native("linux-amd64")),
		"-R", "liatrio/agent-governance-evidence",
		"--bundle", filepath.Join(assetsDir, "build-provenance.sigstore.json"),
		"--source-ref", "refs/tags/" + testTag,
		"--source-digest", commit,
		"--signer-workflow", "liatrio/agent-governance-evidence/.github/workflows/release.yml",
		"--deny-self-hosted-runners",
	}
	for _, tc := range []struct {
		name  string
		index int
		value string
	}{
		{"subject", 2, filepath.Join(assetsDir, "nonexistent")},
		{"bundle", 6, filepath.Join(assetsDir, "SHA256SUMS")},
		{"repository", 4, "mallory/repo"},
		{"source-ref", 8, "refs/tags/v9.9.9-alpha.9"},
		{"source-digest", 10, strings.Repeat("b", 40)},
		{"workflow", 12, "mallory/repo/.github/workflows/release.yml"},
		{"runner-guard", 13, "--no-public-good"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock, log, _ := ghMock(t)
			args := append([]string(nil), base...)
			args[tc.index] = tc.value
			out, err := run(t, root(t), []string{"PATH=" + mock + ":" + os.Getenv("PATH"), "GH_LOG=" + log, "ASSETS_DIR=" + assetsDir, "COMMIT=" + commit}, "gh", args...)
			if err == nil {
				t.Fatalf("wrong %s binding passed oracle:\n%s", tc.name, out)
			}
		})
	}
}

func TestVerifyDraftMetadataFailuresBlockExecution(t *testing.T) {
	source, commit := gitSource(t)
	for _, tc := range []struct {
		name, want string
		body       func(*testing.T, string) string
	}{
		{"duplicate", "missing or duplicate", func(t *testing.T, d string) string {
			return draftMetadataJSON(t, d, true, true, testTag, []map[string]string{{"name": "SHA256SUMS", "digest": "sha256:" + strings.Repeat("0", 64)}, {"name": "SHA256SUMS", "digest": "sha256:" + strings.Repeat("1", 64)}, {"name": "a", "digest": "sha256:" + strings.Repeat("2", 64)}, {"name": "b", "digest": "sha256:" + strings.Repeat("3", 64)}, {"name": "c", "digest": "sha256:" + strings.Repeat("4", 64)}})
		}},
		{"missing", "missing or duplicate", func(t *testing.T, d string) string {
			x := remote(t, d)
			return draftMetadataJSON(t, d, true, true, testTag, x[:4])
		}},
		{"extra", "names or digests mismatch", func(t *testing.T, d string) string {
			x := remote(t, d)
			x[4]["name"] = "extra"
			return draftMetadataJSON(t, d, true, true, testTag, x)
		}},
		{"bad-digest", "names or digests mismatch", func(t *testing.T, d string) string {
			x := remote(t, d)
			x[0]["digest"] = "sha256:" + strings.Repeat("0", 64)
			return draftMetadataJSON(t, d, true, true, testTag, x)
		}},
		{"wrong-tag", "unexpected release state", func(t *testing.T, d string) string {
			return draftMetadataJSON(t, d, true, true, "v9.9.9-alpha.9", nil)
		}},
		{"not-draft", "unexpected release state", func(t *testing.T, d string) string {
			return draftMetadataJSON(t, d, false, true, testTag, nil)
		}},
		{"not-prerelease", "unexpected release state", func(t *testing.T, d string) string {
			return draftMetadataJSON(t, d, true, false, testTag, nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := assets(t, source, commit)
			metadata := filepath.Join(t.TempDir(), "release-metadata.json")
			mustWrite(t, metadata, []byte(tc.body(t, d)), 0644)
			m, g, x := ghMock(t)
			out, err := verify(t, verificationEnv(m, g, x, d, commit, ""), append(verifyArgs(d, source, commit, "draft"), "--metadata-file", metadata)...)
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("metadata failure passed: %v\n%s", err, out)
			}
			if strings.Contains(mustRead(t, g), "release view ") || strings.Contains(mustRead(t, g), "api repos/liatrio/agent-governance-evidence/git/") {
				t.Fatalf("draft metadata failure queried github metadata:\n%s", mustRead(t, g))
			}
			assertEmpty(t, x)
		})
	}
}

func TestVerifyPublishedReleaseMetadataFailuresBlockExecution(t *testing.T) {
	source, commit := gitSource(t)
	for _, tc := range []struct {
		name, want, mockCommit string
		makeJSON               func(*testing.T, string) string
	}{
		{"duplicate", "missing or duplicate", commit, func(t *testing.T, d string) string {
			return releaseJSON(t, d, false, true, true, testTag, []map[string]string{{"name": "SHA256SUMS"}, {"name": "SHA256SUMS"}, {"name": "a"}, {"name": "b"}, {"name": "c"}, {"name": "d"}})
		}},
		{"missing", "missing or duplicate", commit, func(t *testing.T, d string) string {
			x := remote(t, d)
			return releaseJSON(t, d, false, true, true, testTag, x[:4])
		}},
		{"extra", "names or digests mismatch", commit, func(t *testing.T, d string) string {
			x := remote(t, d)
			x[4]["name"] = "extra"
			return releaseJSON(t, d, false, true, true, testTag, x)
		}},
		{"bad-digest", "names or digests mismatch", commit, func(t *testing.T, d string) string {
			x := remote(t, d)
			x[0]["digest"] = "sha256:" + strings.Repeat("0", 64)
			return releaseJSON(t, d, false, true, true, testTag, x)
		}},
		{"wrong-tag", "release identity", commit, func(t *testing.T, d string) string {
			return releaseJSON(t, d, false, true, true, "v9.9.9-alpha.9", nil)
		}},
		{"moved-tag", "release identity", strings.Repeat("0", 40), func(t *testing.T, d string) string { return releaseJSON(t, d, false, true, true, testTag, nil) }},
		{"not-prerelease", "unexpected release state", commit, func(t *testing.T, d string) string { return releaseJSON(t, d, false, false, true, testTag, nil) }},
		{"not-immutable", "not immutable", commit, func(t *testing.T, d string) string { return releaseJSON(t, d, false, true, false, testTag, nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := assets(t, source, commit)
			m, g, x := ghMock(t)
			e := verificationEnv(m, g, x, d, commit, tc.makeJSON(t, d))
			if tc.name == "moved-tag" {
				e = append(e, "PEELED_COMMIT="+tc.mockCommit)
			}
			out, err := verify(t, e, verifyArgs(d, source, commit, "published")...)
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("metadata failure passed: %v\n%s", err, out)
			}
			assertEmpty(t, x)
		})
	}
}

func TestChangedSourceCannotFallThroughToExecution(t *testing.T) {
	source, commit := gitSource(t)
	d := assets(t, source, commit)
	e := sourceEntries(t, source, commit)
	e = change(t, e, "/README.md", func(x entry) entry { x.body = []byte("changed\n"); return x })
	writeTar(t, filepath.Join(d, sourceName()), e)
	writeSums(t, d)
	m, g, x := ghMock(t)
	out, err := verify(t, verificationEnv(m, g, x, d, commit, releaseJSON(t, d, true, true, false, testTag, nil)), verifyArgs(d, source, commit, "draft")...)
	if err == nil || !strings.Contains(out, "does not exactly match") {
		t.Fatalf("source equality guard failed: %v\n%s", err, out)
	}
	assertEmpty(t, g)
	assertEmpty(t, x)
}

type entry struct {
	name string
	body []byte
	mode int64
	kind byte
	link string
}

func native(platform string) string {
	return "agent-governance-evidence_" + testTag + "_" + platform + ".tar.gz"
}
func sourceName() string { return "agent-governance-evidence_" + testTag + "_source.tar.gz" }
func names() []string {
	return []string{native("linux-amd64"), native("darwin-arm64"), sourceName(), "SHA256SUMS", "build-provenance.sigstore.json"}
}
func verifyArgs(d, s, c, p string) []string {
	return []string{"--tag", testTag, "--commit", c, "--phase", p, "--assets-dir", d, "--source-dir", s}
}

func gitSource(t *testing.T) (string, string) {
	t.Helper()
	d := t.TempDir()
	mustWrite(t, filepath.Join(d, "README.md"), []byte("trusted\n"), 0644)
	mustWrite(t, filepath.Join(d, "LICENSE"), []byte("license\n"), 0644)
	mustWrite(t, filepath.Join(d, "scripts/setup-autogov.sh"), []byte("#!/bin/sh\nmkdir -p .autogov/bin\nprintf '#!/bin/sh\\nexit 0\\n' > .autogov/bin/autogov-v1.4.0\nchmod 755 .autogov/bin/autogov-v1.4.0\n"), 0755)
	return d, initRepo(t, d)
}
func initRepo(t *testing.T, d string) string {
	t.Helper()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-qm", "fixture"}} {
		mustRun(t, d, "git", a...)
	}
	out, err := run(t, d, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}
func mustRun(t *testing.T, d, n string, a ...string) {
	t.Helper()
	if out, err := run(t, d, nil, n, a...); err != nil {
		t.Fatalf("%s %v: %v\n%s", n, a, err, out)
	}
}

func assets(t *testing.T, source, commit string) string {
	t.Helper()
	d := t.TempDir()
	for _, n := range []string{native("linux-amd64"), native("darwin-arm64")} {
		writeTar(t, filepath.Join(d, n), platformEntries())
	}
	raw, err := exec.Command("git", "-C", source, "-c", "tar.umask=022", "archive", "--format=tar", "--prefix=agent-governance-evidence_"+testTag+"/", commit).Output()
	if err != nil {
		t.Fatal(err)
	}
	writeGzip(t, filepath.Join(d, sourceName()), raw)
	writeSums(t, d)
	mustWrite(t, filepath.Join(d, "build-provenance.sigstore.json"), []byte("{}"), 0644)
	return d
}
func platformEntries() []entry {
	script := func(n string) []byte {
		return []byte("#!/bin/sh\nprintf '%s\\n' '" + n + ":$0' >> \"${EXEC_LOG:?}\"\n")
	}
	return []entry{{"agent-governance-evidence", script("evidence"), 0755, tar.TypeReg, ""}, {"agent-governance-demo", script("demo"), 0755, tar.TypeReg, ""}, {"checkpoint", script("checkpoint"), 0755, tar.TypeReg, ""}, {"LICENSE", []byte("license\n"), 0644, tar.TypeReg, ""}}
}
func writeTar(t *testing.T, p string, e []entry) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(f)
	w := tar.NewWriter(z)
	for _, x := range e {
		h := &tar.Header{Name: x.name, Mode: x.mode, Size: int64(len(x.body)), Typeflag: x.kind, Linkname: x.link}
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := w.Write(x.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
func writeGzip(t *testing.T, p string, b []byte) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(f)
	if _, err := z.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
func writeGzipNamed(t *testing.T, p string, b []byte, name string) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(f)
	z.Name = name
	if _, err := z.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
func gunzipBytes(t *testing.T, p string) []byte {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	z, err := gzip.NewReader(f)
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		t.Fatal(err)
	}
	b, err := io.ReadAll(z)
	closeErr := z.Close()
	fileErr := f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if fileErr != nil {
		t.Fatal(fileErr)
	}
	return b
}
func sourceEntries(t *testing.T, s, c string) []entry {
	t.Helper()
	raw, err := exec.Command("git", "-C", s, "-c", "tar.umask=022", "archive", "--format=tar", "--prefix=agent-governance-evidence_"+testTag+"/", c).Output()
	if err != nil {
		t.Fatal(err)
	}
	r := tar.NewReader(bytes.NewReader(raw))
	var out []entry
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, entry{h.Name, b, h.Mode, h.Typeflag, h.Linkname})
	}
	return out
}
func find(t *testing.T, e []entry, suffix string) entry {
	t.Helper()
	for _, x := range e {
		if strings.HasSuffix(x.name, suffix) {
			x.body = append([]byte(nil), x.body...)
			return x
		}
	}
	t.Fatal("entry not found")
	return entry{}
}
func change(t *testing.T, e []entry, suffix string, f func(entry) entry) []entry {
	t.Helper()
	out := append([]entry(nil), e...)
	found := false
	for i := range out {
		out[i].body = append([]byte(nil), out[i].body...)
		if strings.HasSuffix(out[i].name, suffix) {
			out[i] = f(out[i])
			found = true
		}
	}
	if !found {
		t.Fatal("entry not found")
	}
	return out
}
func remove(t *testing.T, e []entry, suffix string) []entry {
	t.Helper()
	out := make([]entry, 0, len(e))
	for _, x := range e {
		if !strings.HasSuffix(x.name, suffix) {
			out = append(out, x)
		}
	}
	if len(out) == len(e) {
		t.Fatal("entry not found")
	}
	return out
}

func writeSums(t *testing.T, d string) {
	t.Helper()
	var b strings.Builder
	for _, n := range names()[:3] {
		sum := sha256.Sum256([]byte(mustRead(t, filepath.Join(d, n))))
		fmt.Fprintf(&b, "%x  %s\n", sum, n)
	}
	mustWrite(t, filepath.Join(d, "SHA256SUMS"), []byte(b.String()), 0644)
}
func mutateSums(t *testing.T, d string, f func([]string) []string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(mustRead(t, filepath.Join(d, "SHA256SUMS")), "\n"), "\n")
	mustWrite(t, filepath.Join(d, "SHA256SUMS"), []byte(strings.Join(f(lines), "\n")+"\n"), 0644)
}
func remote(t *testing.T, d string) []map[string]string {
	t.Helper()
	var out []map[string]string
	for _, n := range names() {
		sum := sha256.Sum256([]byte(mustRead(t, filepath.Join(d, n))))
		out = append(out, map[string]string{"name": n, "digest": fmt.Sprintf("sha256:%x", sum)})
	}
	return out
}
func releaseJSON(t *testing.T, d string, draft, pre, immutable bool, releaseTag string, override []map[string]string) string {
	t.Helper()
	if override == nil {
		override = remote(t, d)
	}
	b, err := json.Marshal(map[string]any{"databaseId": 123, "tagName": releaseTag, "isDraft": draft, "isPrerelease": pre, "isImmutable": immutable, "assets": override})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func draftMetadataJSON(t *testing.T, d string, draft, pre bool, releaseTag string, override []map[string]string) string {
	t.Helper()
	if override == nil {
		override = remote(t, d)
	}
	b, err := json.Marshal(map[string]any{"release_id": 123, "tag": releaseTag, "draft": draft, "prerelease": pre, "assets": override})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func ghMock(t *testing.T) (string, string, string) {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "gh.log")
	execLog := filepath.Join(t.TempDir(), "exec.log")
	truncate(t, log)
	truncate(t, execLog)
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$GH_LOG"
case "$1 $2" in
"attestation verify")
 test "$#" -eq 14
 case "$3" in "$ASSETS_DIR/agent-governance-evidence_v0.1.0-alpha.1_linux-amd64.tar.gz"|"$ASSETS_DIR/agent-governance-evidence_v0.1.0-alpha.1_darwin-arm64.tar.gz"|"$ASSETS_DIR/agent-governance-evidence_v0.1.0-alpha.1_source.tar.gz"|"$ASSETS_DIR/SHA256SUMS");; *) exit 71;; esac
 test "$4" = -R; test "$5" = liatrio/agent-governance-evidence; test "$6" = --bundle; test "$7" = "$ASSETS_DIR/build-provenance.sigstore.json"
 test "$8" = --source-ref; test "$9" = refs/tags/v0.1.0-alpha.1; test "${10}" = --source-digest; test "${11}" = "$COMMIT"; test "${12}" = --signer-workflow; test "${13}" = liatrio/agent-governance-evidence/.github/workflows/release.yml; test "${14}" = --deny-self-hosted-runners
 test "${GH_FAIL-}" != attestation || { echo attestation failure >&2; exit 47; };;
"release view") printf '%s' "$RELEASE_JSON";;
"release verify") test "${GH_FAIL-}" != release || { echo immutable verification failure >&2; exit 48; };;
"release verify-asset") test "$4" = "$ASSETS_DIR/$(basename "$4")"; test "${GH_FAIL-}" != "$(basename "$4")" || { echo immutable verification failure >&2; exit 49; };;
"api "*) case "$2" in *"/git/ref/tags/"*) echo object;; *) echo "${PEELED_COMMIT:-$COMMIT}";; esac;;
*) exit 99;; esac
`
	mustWrite(t, filepath.Join(bin, "gh"), []byte(script), 0755)
	return bin, log, execLog
}
func verificationEnv(m, g, x, d, c, r string) []string {
	return []string{"PATH=" + m + ":" + os.Getenv("PATH"), "GH_LOG=" + g, "EXEC_LOG=" + x, "ASSETS_DIR=" + d, "COMMIT=" + c, "RELEASE_JSON=" + r}
}

func assertExactAttestations(t *testing.T, body, assetsDir string) {
	t.Helper()
	if strings.Count(body, "attestation verify ") != 4 {
		t.Fatalf("wrong attestation count:\n%s", body)
	}
	for _, name := range names()[:4] {
		prefix := "attestation verify " + filepath.Join(assetsDir, name) + " -R liatrio/agent-governance-evidence --bundle " + filepath.Join(assetsDir, "build-provenance.sigstore.json")
		if strings.Count(body, prefix) != 1 {
			t.Fatalf("missing exact subject/bundle %s:\n%s", name, body)
		}
	}
}
func rejectGH(t *testing.T) (string, string) {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "gh.log")
	truncate(t, log)
	mustWrite(t, filepath.Join(bin, "gh"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$GH_LOG\"\nexit 99\n"), 0755)
	return bin, log
}

func incoming(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	for _, p := range []string{"linux-amd64", "darwin-arm64"} {
		mustMkdir(t, filepath.Join(d, p))
		mustWrite(t, filepath.Join(d, p, native(p)), []byte(p), 0644)
		writeGzip(t, filepath.Join(d, p, sourceName()), []byte("source"))
	}
	return d
}
func draftAssets(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	for _, n := range names() {
		mustWrite(t, filepath.Join(d, n), []byte(n), 0644)
	}
	return d
}
func draftMock(t *testing.T) (string, string) {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "gh.log")
	truncate(t, log)
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$GH_LOG"
case "$1 $2" in
"api --include") case "$GH_MODE" in missing) echo 'HTTP/2.0 404 Not Found';exit 1;;existing)echo 'HTTP/2.0 200 OK';exit 0;;error)echo 'HTTP/2.0 500 Error';exit 1;;esac;;
"release create")
 test "$#" -eq 15
 test "$3" = v0.1.0-alpha.1
 test "$4" = "$ASSETS_DIR/agent-governance-evidence_v0.1.0-alpha.1_linux-amd64.tar.gz"
 test "$5" = "$ASSETS_DIR/agent-governance-evidence_v0.1.0-alpha.1_darwin-arm64.tar.gz"
 test "$6" = "$ASSETS_DIR/agent-governance-evidence_v0.1.0-alpha.1_source.tar.gz"
 test "$7" = "$ASSETS_DIR/SHA256SUMS"
 test "$8" = "$ASSETS_DIR/build-provenance.sigstore.json"
 test "$9" = --repo; test "${10}" = liatrio/agent-governance-evidence
 test "${11}" = --draft; test "${12}" = --prerelease; test "${13}" = --verify-tag
 test "${14}" = --title; test "${15}" = v0.1.0-alpha.1;;
*) exit 99;;
esac
`
	mustWrite(t, filepath.Join(bin, "gh"), []byte(script), 0755)
	return bin, log
}

func retrieveMock(t *testing.T) (string, string, string) {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "gh.log")
	execLog := filepath.Join(t.TempDir(), "exec.log")
	truncate(t, log)
	truncate(t, execLog)
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$GH_LOG"
case "$1 $2" in
"release view") printf '%s' "$RELEASE_JSON";;
"release download")
 dir=
 while [ "$#" -gt 0 ]; do
  case "$1" in
   --dir) dir=$2; shift 2;;
   *) shift;;
  esac
 done
 test -n "$dir"
 mkdir -p "$dir"
 cp "$DOWNLOADS_DIR"/* "$dir"/;;
*) exit 99;;
esac
`
	mustWrite(t, filepath.Join(bin, "gh"), []byte(script), 0755)
	for _, name := range []string{"tar", "checkpoint", "agent-governance-demo", "agent-governance-evidence"} {
		mustWrite(t, filepath.Join(bin, name), []byte("#!/bin/sh\nprintf '%s\\n' \"$0 $*\" >> \"$EXEC_LOG\"\nexit 1\n"), 0755)
	}
	return bin, log, execLog
}

func packageRepo(t *testing.T) (string, string) {
	t.Helper()
	d := t.TempDir()
	mustWrite(t, filepath.Join(d, "go.mod"), []byte("module example.com/fixture\n\ngo 1.26.6\n"), 0644)
	mustWrite(t, filepath.Join(d, "LICENSE"), []byte("license\n"), 0644)
	program := `package main
import "os"
func main(){f,e:=os.OpenFile(os.Getenv("EXEC_LOG"),os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600);if e!=nil{panic(e)};defer f.Close();p,_:=os.Executable();f.WriteString(p+"\n")}
`
	for _, p := range []string{"cmd/agent-governance-evidence/main.go", "cmd/demo/main.go", "cmd/checkpoint/main.go"} {
		mustWrite(t, filepath.Join(d, p), []byte(program), 0644)
	}
	mustWrite(t, filepath.Join(d, "scripts/setup-autogov.sh"), []byte("#!/bin/sh\nmkdir -p .autogov/bin\nprintf '#!/bin/sh\\nexit 0\\n' > .autogov/bin/autogov-v1.4.0\nchmod 755 .autogov/bin/autogov-v1.4.0\n"), 0755)
	mustWrite(t, filepath.Join(d, "scripts/package-release.sh"), []byte(mustRead(t, filepath.Join(root(t), "scripts/package-release.sh"))), 0755)
	log := filepath.Join(t.TempDir(), "exec.log")
	c := initRepo(t, d)
	mustRun(t, d, "git", "tag", "-am", "release", testTag, c)
	return d, log
}

func mustWrite(t *testing.T, p string, b []byte, m os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, m); err != nil {
		t.Fatal(err)
	}
}
func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatal(err)
	}
}
func mustRemove(t *testing.T, p string) {
	t.Helper()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
}
func truncate(t *testing.T, p string) { t.Helper(); mustWrite(t, p, nil, 0600) }
func assertEmpty(t *testing.T, p string) {
	t.Helper()
	if b := mustRead(t, p); b != "" {
		t.Fatalf("unexpected downstream execution:\n%s", b)
	}
}
func exists(p string) bool { _, err := os.Lstat(p); return err == nil }
