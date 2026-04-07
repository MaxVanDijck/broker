package workdir

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Archive creates a tar.gz of the given directory, respecting .gitignore
// patterns. Returns the path to the temporary archive file.
func Archive(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	ignorePatterns := loadGitignore(dir)
	ignorePatterns = append(ignorePatterns,
		".git",
		"node_modules",
		"__pycache__",
		".venv",
		"venv",
		".mypy_cache",
		".pytest_cache",
		"*.pyc",
		".DS_Store",
	)

	tmp, err := os.CreateTemp("", "broker-workdir-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmp.Close()

	gw := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gw)

	err = filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		if shouldIgnore(rel, info.IsDir(), ignorePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel

		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			header.Linkname = link
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tw, f)
		return err
	})

	if err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("walk directory: %w", err)
	}

	if err := tw.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := gw.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}

	return tmp.Name(), nil
}

// Extract unpacks a tar.gz into the target directory.
func Extract(r io.Reader, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		target := filepath.Join(targetDir, header.Name)

		// Prevent path traversal
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(targetDir)) {
			return fmt.Errorf("invalid path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			// Validate symlink target stays within the extraction directory.
			linkTarget := header.Linkname
			if !filepath.IsAbs(linkTarget) {
				linkTarget = filepath.Join(filepath.Dir(target), linkTarget)
			}
			linkTarget = filepath.Clean(linkTarget)
			cleanDir := filepath.Clean(targetDir)
			if !strings.HasPrefix(linkTarget, cleanDir+string(filepath.Separator)) && linkTarget != cleanDir {
				return fmt.Errorf("symlink %q points outside extraction directory: %q", header.Name, header.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
}

func loadGitignore(dir string) []string {
	f, err := os.Open(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

func shouldIgnore(rel string, isDir bool, patterns []string) bool {
	name := filepath.Base(rel)
	for _, pattern := range patterns {
		// Exact directory match
		if isDir && pattern == name {
			return true
		}
		if isDir && pattern == name+"/" {
			return true
		}
		// Glob match against the base name
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
		// Glob match against the full relative path
		if matched, _ := filepath.Match(pattern, rel); matched {
			return true
		}
		// Directory prefix match (e.g. "vendor" matches "vendor/foo/bar")
		if strings.HasPrefix(rel, pattern+"/") {
			return true
		}
	}
	return false
}
