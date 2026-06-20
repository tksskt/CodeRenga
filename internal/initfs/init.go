package initfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tks/coderenga/internal/storage"
)

func Initialize(executableDir string, templates fs.FS) error {
	target := filepath.Join(executableDir, "coderenga.d")
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("coderenga.d already exists.\nRemove it first if you want to recreate the configuration.")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect target: %w", err)
	}

	staging, err := os.MkdirTemp(executableDir, ".coderenga-init-")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	if err := fs.WalkDir(templates, "coderenga.d", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel("coderenga.d", path)
		if err != nil || relative == "." {
			return err
		}
		destination := filepath.Join(staging, filepath.FromSlash(relative))
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := fs.ReadFile(templates, path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o644)
	}); err != nil {
		return fmt.Errorf("extract embedded templates: %w", err)
	}
	if err := storage.Bootstrap(filepath.Join(staging, "coderenga.db")); err != nil {
		return err
	}
	if err := os.Rename(staging, target); err != nil {
		return fmt.Errorf("install initialized directory: %w", err)
	}
	return nil
}
