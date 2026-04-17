package catalogutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrInit_InvalidJSONTypedError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "catalog.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LoadOrInit(root, "catalog.json", struct{}{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestSave_CreateDirTypedError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file-root")
	if err := os.WriteFile(root, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := Save(root, "catalog.json", map[string]string{"k": "v"})
	if !errors.Is(err, ErrTransport) {
		t.Fatalf("expected ErrTransport, got %v", err)
	}
}
