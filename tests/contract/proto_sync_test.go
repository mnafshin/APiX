// Package contract_test verifies that the VS Code extension's proto reference
// is correctly wired to the Go source of truth, and that generated Go bindings
// are up to date with the proto source.
package contract_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRoot returns the absolute path to the repository root by walking up from
// this file's location. Using runtime.Caller keeps the path correct regardless
// of where `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = …/tests/contract/proto_sync_test.go  →  go up two levels
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

// TestContract_ExtensionProtoLinkedToSource verifies that the VS Code
// extension's proto is correctly linked to (or is content-identical to) the
// Go source of truth at pkg/api/proto/apix.proto.
//
// The extension proto is intentionally kept as a symlink so the two are always
// identical. This test catches three failure modes:
//  1. The symlink is broken (target does not exist).
//  2. The symlink points to the wrong file.
//  3. The extension switched from a symlink to a stale copy that drifted.
func TestContract_ExtensionProtoLinkedToSource(t *testing.T) {
	root := repoRoot(t)

	srcPath := filepath.Join(root, "pkg", "api", "proto", "apix.proto")
	extPath := filepath.Join(root, "apix-vscode", "proto", "apix.proto")

	// Verify the source proto exists.
	if _, err := os.Stat(srcPath); err != nil {
		t.Fatalf("source proto missing at %s: %v", srcPath, err)
	}

	// Check whether the extension entry is a symlink.
	fi, err := os.Lstat(extPath)
	if err != nil {
		t.Fatalf("extension proto missing at %s: %v\n  fix: ln -s ../../pkg/api/proto/apix.proto apix-vscode/proto/apix.proto", extPath, err)
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		// It's a symlink — verify it resolves to the canonical source.
		resolved, err := filepath.EvalSymlinks(extPath)
		if err != nil {
			t.Fatalf("extension proto symlink is broken: %v\n  fix: ln -sf ../../pkg/api/proto/apix.proto apix-vscode/proto/apix.proto", err)
		}
		wantResolved, err := filepath.EvalSymlinks(srcPath)
		if err != nil {
			t.Fatalf("EvalSymlinks on source: %v", err)
		}
		if resolved != wantResolved {
			t.Errorf(
				"extension proto symlink points to wrong target\n"+
					"  got:  %s\n"+
					"  want: %s\n"+
					"  fix:  ln -sf ../../pkg/api/proto/apix.proto apix-vscode/proto/apix.proto",
				resolved, wantResolved,
			)
		}
	} else {
		// It's a regular file — verify content matches (copy-based workflow).
		srcBytes, err := os.ReadFile(srcPath)
		if err != nil {
			t.Fatalf("read source proto: %v", err)
		}
		extBytes, err := os.ReadFile(extPath)
		if err != nil {
			t.Fatalf("read extension proto: %v", err)
		}
		if !bytes.Equal(srcBytes, extBytes) {
			t.Errorf(
				"extension proto is a stale copy of the source\n"+
					"  source:    %s\n"+
					"  extension: %s\n"+
					"  fix:       cp pkg/api/proto/apix.proto apix-vscode/proto/apix.proto",
				srcPath, extPath,
			)
		}
	}
}

// TestContract_GeneratedGoMatchesProto regenerates the Go bindings from the
// proto source in a temp directory and diffs them against the committed files
// in pkg/api/generated/.  The test is skipped automatically when protoc or
// protoc-gen-go are not in PATH so it never breaks CI that lacks those tools.
func TestContract_GeneratedGoMatchesProto(t *testing.T) {
	if _, err := exec.LookPath("protoc"); err != nil {
		t.Skip("protoc not in PATH — skipping generated-code freshness check")
	}
	if _, err := exec.LookPath("protoc-gen-go"); err != nil {
		t.Skip("protoc-gen-go not in PATH — skipping generated-code freshness check")
	}
	if _, err := exec.LookPath("protoc-gen-go-grpc"); err != nil {
		t.Skip("protoc-gen-go-grpc not in PATH — skipping generated-code freshness check")
	}

	root := repoRoot(t)
	tmpDir := t.TempDir()

	protoSrc := filepath.Join(root, "pkg", "api", "proto", "apix.proto")

	cmd := exec.Command(
		"protoc",
		"--go_out="+tmpDir,
		"--go-grpc_out="+tmpDir,
		"--go_opt=paths=source_relative",
		"--go-grpc_opt=paths=source_relative",
		"--proto_path="+filepath.Join(root, "pkg", "api", "proto"),
		protoSrc,
	)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("protoc failed: %v\n%s", err, out)
	}

	// Compare each generated file against its committed counterpart.
	generatedFiles := []string{"apix.pb.go", "apix_grpc.pb.go"}
	for _, name := range generatedFiles {
		want, err := os.ReadFile(filepath.Join(tmpDir, name))
		if err != nil {
			t.Errorf("generated file %s not produced by protoc: %v", name, err)
			continue
		}
		got, err := os.ReadFile(filepath.Join(root, "pkg", "api", "generated", name))
		if err != nil {
			t.Errorf("committed file pkg/api/generated/%s missing: %v", name, err)
			continue
		}
		if !bytes.Equal(want, got) {
			t.Errorf(
				"pkg/api/generated/%s is stale\n"+
					"  fix: protoc --go_out=pkg/api/generated --go-grpc_out=pkg/api/generated \\\n"+
					"         --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative \\\n"+
					"         pkg/api/proto/apix.proto",
				name,
			)
		}
	}
}
