package workdir

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveAndExtract_RoundTrip(t *testing.T) {
	t.Run("given a directory with files, when archived and extracted, then contents match", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := filepath.Join(t.TempDir(), "extracted")

		if err := os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello world"), 0o644); err != nil {
			t.Fatal(err)
		}
		subDir := filepath.Join(srcDir, "subdir")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested content"), 0o644); err != nil {
			t.Fatal(err)
		}

		archivePath, err := Archive(srcDir)
		if err != nil {
			t.Fatalf("Archive: %v", err)
		}
		defer os.Remove(archivePath)

		f, err := os.Open(archivePath)
		if err != nil {
			t.Fatalf("open archive: %v", err)
		}
		defer f.Close()

		if err := Extract(f, dstDir); err != nil {
			t.Fatalf("Extract: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(dstDir, "hello.txt"))
		if err != nil {
			t.Fatalf("read hello.txt: %v", err)
		}
		if string(content) != "hello world" {
			t.Errorf("expected 'hello world', got %q", string(content))
		}

		nested, err := os.ReadFile(filepath.Join(dstDir, "subdir", "nested.txt"))
		if err != nil {
			t.Fatalf("read nested.txt: %v", err)
		}
		if string(nested) != "nested content" {
			t.Errorf("expected 'nested content', got %q", string(nested))
		}
	})
}

func TestArchive_GitignorePatternsRespected(t *testing.T) {
	t.Run("given a directory with node_modules and .git, when archived, then they are excluded", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := filepath.Join(t.TempDir(), "extracted")

		if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}

		nodeModules := filepath.Join(srcDir, "node_modules")
		if err := os.MkdirAll(nodeModules, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nodeModules, "package.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}

		gitDir := filepath.Join(srcDir, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("git stuff"), 0o644); err != nil {
			t.Fatal(err)
		}

		pycacheDir := filepath.Join(srcDir, "__pycache__")
		if err := os.MkdirAll(pycacheDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pycacheDir, "module.pyc"), []byte("bytecode"), 0o644); err != nil {
			t.Fatal(err)
		}

		archivePath, err := Archive(srcDir)
		if err != nil {
			t.Fatalf("Archive: %v", err)
		}
		defer os.Remove(archivePath)

		f, err := os.Open(archivePath)
		if err != nil {
			t.Fatalf("open archive: %v", err)
		}
		defer f.Close()

		if err := Extract(f, dstDir); err != nil {
			t.Fatalf("Extract: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dstDir, "main.go")); os.IsNotExist(err) {
			t.Error("expected main.go to be in archive")
		}

		if _, err := os.Stat(filepath.Join(dstDir, "node_modules")); !os.IsNotExist(err) {
			t.Error("expected node_modules to be excluded from archive")
		}

		if _, err := os.Stat(filepath.Join(dstDir, ".git")); !os.IsNotExist(err) {
			t.Error("expected .git to be excluded from archive")
		}

		if _, err := os.Stat(filepath.Join(dstDir, "__pycache__")); !os.IsNotExist(err) {
			t.Error("expected __pycache__ to be excluded from archive")
		}
	})
}

func TestArchive_CustomGitignorePatterns(t *testing.T) {
	t.Run("given a .gitignore file, when archived, then custom patterns are respected", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := filepath.Join(t.TempDir(), "extracted")

		if err := os.WriteFile(filepath.Join(srcDir, ".gitignore"), []byte("*.log\nsecrets/\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, "app.go"), []byte("package main"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, "debug.log"), []byte("log data"), 0o644); err != nil {
			t.Fatal(err)
		}
		secretsDir := filepath.Join(srcDir, "secrets")
		if err := os.MkdirAll(secretsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(secretsDir, "key.pem"), []byte("secret"), 0o644); err != nil {
			t.Fatal(err)
		}

		archivePath, err := Archive(srcDir)
		if err != nil {
			t.Fatalf("Archive: %v", err)
		}
		defer os.Remove(archivePath)

		f, err := os.Open(archivePath)
		if err != nil {
			t.Fatalf("open archive: %v", err)
		}
		defer f.Close()

		if err := Extract(f, dstDir); err != nil {
			t.Fatalf("Extract: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dstDir, "app.go")); os.IsNotExist(err) {
			t.Error("expected app.go to be in archive")
		}
		if _, err := os.Stat(filepath.Join(dstDir, "debug.log")); !os.IsNotExist(err) {
			t.Error("expected debug.log to be excluded by .gitignore pattern")
		}
		if _, err := os.Stat(filepath.Join(dstDir, "secrets")); !os.IsNotExist(err) {
			t.Error("expected secrets/ to be excluded by .gitignore pattern")
		}
	})
}

func TestExtract_PathTraversalRejected(t *testing.T) {
	t.Run("given a tar with path traversal headers, when extracting, then an error is returned", func(t *testing.T) {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)

		maliciousHeader := &tar.Header{
			Name:     "../../../etc/passwd",
			Mode:     0o644,
			Size:     int64(len("malicious content")),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(maliciousHeader); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if _, err := tw.Write([]byte("malicious content")); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if err := tw.Close(); err != nil {
			t.Fatalf("tw.Close: %v", err)
		}
		if err := gw.Close(); err != nil {
			t.Fatalf("gw.Close: %v", err)
		}

		dstDir := t.TempDir()
		err := Extract(&buf, dstDir)
		if err == nil {
			t.Fatal("expected error for path traversal, got nil")
		}
		if !strings.Contains(err.Error(), "invalid path") {
			t.Errorf("expected 'invalid path' in error message, got: %v", err)
		}
	})
}

func TestExtract_NestedPathTraversalRejected(t *testing.T) {
	t.Run("given a tar with nested path traversal, when extracting, then an error is returned", func(t *testing.T) {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)

		maliciousHeader := &tar.Header{
			Name:     "subdir/../../outside.txt",
			Mode:     0o644,
			Size:     int64(len("escape")),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(maliciousHeader); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if _, err := tw.Write([]byte("escape")); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if err := tw.Close(); err != nil {
			t.Fatalf("tw.Close: %v", err)
		}
		if err := gw.Close(); err != nil {
			t.Fatalf("gw.Close: %v", err)
		}

		dstDir := t.TempDir()
		err := Extract(&buf, dstDir)
		if err == nil {
			t.Fatal("expected error for nested path traversal, got nil")
		}
		if !strings.Contains(err.Error(), "invalid path") {
			t.Errorf("expected 'invalid path' in error message, got: %v", err)
		}
	})
}
