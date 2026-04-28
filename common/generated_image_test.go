package common

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupOldGeneratedImages(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GENERATED_IMAGE_DIR", dir)

	oldPath := filepath.Join(dir, "old.png")
	newPath := filepath.Join(dir, "new.png")
	if err := os.WriteFile(oldPath, []byte("old"), 0644); err != nil {
		t.Fatalf("failed to write old image: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0644); err != nil {
		t.Fatalf("failed to write new image: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("failed to age old image: %v", err)
	}

	removedCount, removedBytes, err := CleanupOldGeneratedImages(24 * time.Hour)
	if err != nil {
		t.Fatalf("CleanupOldGeneratedImages returned error: %v", err)
	}
	if removedCount != 1 {
		t.Fatalf("expected 1 removed file, got %d", removedCount)
	}
	if removedBytes != int64(len("old")) {
		t.Fatalf("expected removed bytes %d, got %d", len("old"), removedBytes)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old image to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected new image to remain, stat err=%v", err)
	}
}
