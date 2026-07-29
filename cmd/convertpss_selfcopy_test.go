package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// The disc and single-file routes both materialize the .PSS into the output directory
// and then hand that path to convertPSSFile, which used to copy it to the same place.
// os.Create truncates the destination first, so the file was emptied before anything
// was read and ffmpeg received nothing. This pins the guard.
func TestConvertPSSDoesNotTruncateWhenSourceIsAlreadyInOutDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.pss")
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	// outDir is the directory the file already lives in, which is the failing shape.
	convertPSSFile(path, dir, false)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("source vanished: %v", err)
	}
	if len(got) != len(payload) {
		t.Fatalf("source was truncated: %d bytes, want %d", len(got), len(payload))
	}
}

func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The same file reached by two different-looking paths must still compare equal,
	// which is why this is an inode check rather than a string compare.
	indirect := filepath.Join(dir, ".", "a")
	if !sameFile(a, indirect) {
		t.Error("same file by two paths reported as different")
	}
	if sameFile(a, b) {
		t.Error("distinct files reported as the same")
	}
	if sameFile(a, filepath.Join(dir, "missing")) {
		t.Error("missing file reported as the same")
	}
}
