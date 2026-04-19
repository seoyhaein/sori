package sori

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestBuildRegisteredDataDefinition(t *testing.T) {
	req := DataRegisterRequest{
		DataName:    "hg38-reference",
		Version:     "2024-01",
		Description: "reference genome",
		Format:      "FASTA",
		SourceURI:   "s3://bucket/hg38.fa.gz",
		Display: DisplaySpec{
			Category: "Reference",
			Tags:     []string{"human", "genome"},
		},
	}
	pkg := &PackageResult{
		LocalTag:       "hg38.v1",
		ManifestDigest: "sha256:local-manifest",
	}
	push := &PushResult{
		Reference:      "harbor.example/data/hg38:2024-01",
		ManifestDigest: "sha256:remote-manifest",
	}

	def, err := BuildRegisteredDataDefinition(req, pkg, push)
	if err != nil {
		t.Fatalf("BuildRegisteredDataDefinition: %v", err)
	}

	if def.StableRef != "hg38-reference@2024-01" {
		t.Fatalf("stable ref mismatch: got %q", def.StableRef)
	}
	if def.Checksum != push.ManifestDigest {
		t.Fatalf("checksum mismatch: got %q want %q", def.Checksum, push.ManifestDigest)
	}
	if def.StorageURI != push.Reference {
		t.Fatalf("storage uri mismatch: got %q want %q", def.StorageURI, push.Reference)
	}
	if def.Display.Label != req.DataName {
		t.Fatalf("display label mismatch: got %q", def.Display.Label)
	}
	if def.CASHash == "" {
		t.Fatal("expected cas hash to be populated")
	}
}

func TestBuildRegisteredDataDefinition_ValidationError(t *testing.T) {
	_, err := BuildRegisteredDataDefinition(DataRegisterRequest{}, nil, nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestDataCatalogRegisterListGet(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	storePath := filepath.Join(rootDir, "oci")

	pkg, err := PackageVolumeToStore(ctx, storePath, PackageRequest{
		SourceDir:   "./test-vol",
		DisplayName: "HumanRef",
		Tag:         "hg38.v1",
		Dataset:     "hg38-reference",
		Version:     "2024-01",
	})
	if err != nil {
		t.Fatalf("PackageVolumeToStore: %v", err)
	}

	resp, err := RegisterPackagedData(ctx, rootDir, DataRegisterRequest{
		DataName:    "hg38-reference",
		Version:     "2024-01",
		Description: "reference genome",
		Format:      "FASTA",
		SourceURI:   "s3://bucket/hg38.fa.gz",
	}, pkg, nil)
	if err != nil {
		t.Fatalf("RegisterPackagedData: %v", err)
	}
	if resp.CASHash == "" || resp.Data == nil {
		t.Fatalf("unexpected register response: %+v", resp)
	}

	cat := NewDataCatalog(rootDir)
	got, err := cat.Get(resp.CASHash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DataName != "hg38-reference" {
		t.Fatalf("data name mismatch: got %q", got.DataName)
	}

	_, err = cat.Get("sha256:missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	all, err := cat.List("")
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 item, got %d", len(all))
	}

	filtered, err := cat.List("hg38-reference@2024-01")
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered item, got %d", len(filtered))
	}
}
