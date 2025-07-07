package volumeindex

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"io"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Partition struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	ManifestRef string `json:"manifest_ref"`
	CreatedAt   string `json:"created_at"`
	// IncludePatterns []string `json:"include_patterns,omitempty"`
	// ExcludePatterns []string `json:"exclude_patterns,omitempty"`
	Compression string `json:"compression"`
	// ChunkSize       int      `json:"chunk_size,omitempty"`
}

type VolumeIndex struct {
	VolumeRef   string      `json:"volume_ref"`
	DisplayName string      `json:"display_name"`
	CreatedAt   string      `json:"created_at"`
	Partitions  []Partition `json:"partitions"`
}

// TODO volume-index.json 이거 받도록 해주고, 이거 제외해줘야 한다. 다른 폴더에 저장하던가.
// TODO json 분리하는 방안도 생각해보자.

// GenerateVolumeIndex scans rootPath recursively to build a VolumeIndex.
// If a directory contains a "no_deep_scan" file, it will be recorded as a single partition
// and its subdirectories will be skipped.
func GenerateVolumeIndex(rootPath, displayName string) (*VolumeIndex, error) {
	// record generation time in UTC (rounded to seconds)
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)

	// base folder name
	rootBase := filepath.Base(rootPath)

	var parts []Partition
	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error accessing %s: %w", path, err)
		}
		// skip the root itself
		if path == rootPath {
			return nil
		}
		// only consider directories
		if !info.IsDir() {
			return nil
		}

		// if no_deep_scan marker exists, record and skip subdirs
		marker := filepath.Join(path, "no_deep_scan")
		if _, err := os.Stat(marker); err == nil {
			rel, err := filepath.Rel(rootPath, path)
			if err != nil {
				return fmt.Errorf("failed to get rel path for %s: %w", path, err)
			}
			slashRel := filepath.ToSlash(rel)
			fullPath := fmt.Sprintf("%s/%s", rootBase, slashRel)
			parts = append(parts, Partition{
				Name:        info.Name(),
				Path:        fullPath,
				ManifestRef: "",
				CreatedAt:   now,
				Compression: "",
			})
			// skip this directory's children
			return filepath.SkipDir
		}

		// normal directory: record it
		rel, err := filepath.Rel(rootPath, path)
		if err != nil {
			return fmt.Errorf("failed to get rel path for %s: %w", path, err)
		}
		slashRel := filepath.ToSlash(rel)
		fullPath := fmt.Sprintf("%s/%s", rootBase, slashRel)
		parts = append(parts, Partition{
			Name:        info.Name(),
			Path:        fullPath,
			ManifestRef: "",
			CreatedAt:   now,
			Compression: "",
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error scanning directories: %w", err)
	}

	return &VolumeIndex{
		VolumeRef:   "",
		DisplayName: displayName,
		CreatedAt:   now,
		Partitions:  parts,
	}, nil
}

// SaveToFile writes the VolumeIndex as JSON to "volume-index.json" under rootPath.
func (vi *VolumeIndex) SaveToFile(rootPath string) error {
	outFile := filepath.Join(rootPath, "volume-index.json")
	data, err := json.MarshalIndent(vi, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal volume index: %w", err)
	}
	if err := os.WriteFile(outFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", outFile, err)
	}
	return nil
}

// PublishVolumeAsOCI reads a volume-index.json at indexPath under rootPath,
// creates an OCI layout in `repo`, pushes a config and one layer per partition,
// then packs and tags a manifest with the given tag.
func PublishVolumeAsOCI(ctx context.Context, rootPath, indexPath, repo, tag string) (*VolumeIndex, error) {
	// 1) Read existing volume index JSON
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file %s: %w", indexPath, err)
	}
	var vi VolumeIndex
	if err := json.Unmarshal(indexData, &vi); err != nil {
		return nil, fmt.Errorf("failed to unmarshal volume index: %w", err)
	}

	// 2) Initialize local OCI store
	store, err := oci.New(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to init OCI store: %w", err)
	}

	// 3) Push a minimal config descriptor if not exists
	config := []byte("{\"architecture\":\"amd64\",\"os\":\"linux\"}")
	configDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.FromBytes(config),
		Size:      int64(len(config)),
	}

	exists, err := store.Exists(ctx, configDesc)
	if err != nil {
		return nil, fmt.Errorf("failed to check existence: %w", err)
	}
	if !exists {
		// not exists, push
		if err := store.Push(ctx, configDesc, bytes.NewReader(config)); err != nil {
			return nil, fmt.Errorf("failed to push config: %w", err)
		}
	}

	// 4) For each partition, create deterministic gzip layer, push if digest new, and record its digest
	rootBase := filepath.Base(rootPath)
	var layers []ocispec.Descriptor
	for i, part := range vi.Partitions {
		relPath := strings.TrimPrefix(part.Path, rootBase+"/")
		fsPath := filepath.Join(rootPath, relPath)

		// Create deterministic tar.gz of that directory
		layerData, err := tarGzDirDeterministic(fsPath, part.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to tar.gz %s: %w", fsPath, err)
		}

		// Create descriptor
		dgst := digest.FromBytes(layerData)
		desc := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageLayerGzip,
			Digest:    dgst,
			Size:      int64(len(layerData)),
		}

		// Push only if not exists
		exists2, err := store.Exists(ctx, desc)
		if err != nil {
			return nil, fmt.Errorf("failed to check existence: %w", err)
		}

		if !exists2 {
			if err := store.Push(ctx, desc, bytes.NewReader(layerData)); err != nil {
				return nil, fmt.Errorf("failed to push layer %s: %w", part.Name, err)
			}
		}

		// Update partition manifest_ref and layers
		vi.Partitions[i].ManifestRef = desc.Digest.String()
		layers = append(layers, desc)
	}

	// 5) Pack manifest (config + layers)
	manifestDesc, err := oras.PackManifest(
		ctx,
		store,
		oras.PackManifestVersion1_1,
		ocispec.MediaTypeImageManifest,
		oras.PackManifestOptions{
			ConfigDescriptor:    &configDesc,
			Layers:              layers,
			ManifestAnnotations: map[string]string{ocispec.AnnotationCreated: time.Now().UTC().Format(time.RFC3339)},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to pack manifest: %w", err)
	}

	// 6) Record and tag the manifest digest
	vi.VolumeRef = manifestDesc.Digest.String()
	if err := store.Tag(ctx, manifestDesc, tag); err != nil {
		return nil, fmt.Errorf("failed to tag manifest: %w", err)
	}

	// 7) Rewrite volume-index.json with updated refs
	updatedData, err := json.MarshalIndent(vi, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated volume index: %w", err)
	}
	if err := os.WriteFile(indexPath, updatedData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write updated index file: %w", err)
	}

	fmt.Println("✅ Volume artifact saved to OCI store. Check", repo, "layout.")
	return &vi, nil
}

// tarGzDir creates a gzip-compressed tarball of fsDir. The tar entries retain the prefix prefixPath in their names.
func tarGzDir(fsDir, prefixPath string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// walk the directory
	err := filepath.Walk(fsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Create header
		rel, err := filepath.Rel(filepath.Dir(fsDir), path)
		if err != nil {
			return err
		}
		// Build tar header name: prefixPath + relative subpath
		tarName := filepath.ToSlash(filepath.Join(prefixPath, rel))
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = tarName

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		// If not a regular file, skip writing body
		if !info.Mode().IsRegular() {
			return nil
		}

		// Write file content
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		return nil
	})
	// close writers
	tw.Close()
	gw.Close()

	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// tarGzDirDeterministic creates a gzip-compressed tarball of fsDir in a deterministic way:
// - gzip header timestamp fixed to Unix epoch
// - entries sorted alphabetically
// - consistent compression level
func tarGzDirDeterministic(fsDir, prefixPath string) ([]byte, error) {
	// Gather entries
	var paths []string
	err := filepath.Walk(fsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	// Fix timestamp
	gw.Header.ModTime = time.Unix(0, 0)
	// Optionally clear OS/Name
	gw.Header.OS = 0
	tw := tar.NewWriter(gw)

	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}

		rel, err := filepath.Rel(filepath.Dir(fsDir), path)
		if err != nil {
			return nil, err
		}
		tarName := filepath.ToSlash(filepath.Join(prefixPath, rel))

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil, err
		}
		hdr.Name = tarName
		// Remove UID/GID for determinism
		hdr.Uid = 0
		hdr.Gid = 0

		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}

		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	tw.Close()
	gw.Close()

	return buf.Bytes(), nil
}
