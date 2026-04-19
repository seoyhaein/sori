package archiveutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"testing"
)

func TestSecureJoinArchivePath_PathTraversalTypedError(t *testing.T) {
	_, err := SecureJoinArchivePath(t.TempDir(), "../evil")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestUntarGzDir_InvalidGzipTypedError(t *testing.T) {
	err := UntarGzDir(bytes.NewReader([]byte("not gzip")), t.TempDir())
	if !errors.Is(err, ErrIntegrity) {
		t.Fatalf("expected ErrIntegrity, got %v", err)
	}
}

func TestUntarGzDir_SymlinkEscapeTypedError(t *testing.T) {
	buf := &bytes.Buffer{}
	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name:     "link",
		Typeflag: tar.TypeSymlink,
		Linkname: "../evil",
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	err := UntarGzDir(bytes.NewReader(buf.Bytes()), t.TempDir())
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}
