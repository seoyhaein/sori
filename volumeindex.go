package sori

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"io"
	"io/fs"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
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
	Compression string `json:"compression"`
}

type VolumeIndex struct {
	VolumeRef   string      `json:"volume_ref"`
	DisplayName string      `json:"display_name"`
	CreatedAt   string      `json:"created_at"`
	Partitions  []Partition `json:"partitions"`
}

// VolumeCollection 여러 개의 VolumeIndex 를 담는 구조체
type VolumeCollection struct {
	Version int           `json:"version"` // 변경될 때마다 +1
	Volumes []VolumeIndex `json:"volumes"`
}

// TODO 빠르게 개발하기 위해 서일단 그냥 막씀.

const CollectionFileName = "volume-collection.json"
const OCIStore = "./repo"

// TODO 정책을 정해야 하는데 일단, rootDir 안에 볼륨 폴더들이 있는 것이 원칙이다. 하지만 그렇게 하지 않다도 되게 일단 만들어 놓는다.
// TODO 그리고 볼륨 폴더에 는 volume-index.json 이 있어야 한다. 위치는 볼륨 폴더의 루트 위치에 있어야 한다. 그것들을 읽어서 VolumeCollection 을 만들어 주는 방식으로 간다.
// TODO 이러한 내용은 사용자가 알아도 되지만 몰라도 된다. 이렇게 메서드들이 만들어 주는 방식으로 간다. 사용자 편의성을 위해서 사용자의 입력을 최소한으로 진행한다. <- 이렇게 하면, 메서드도 정리 해야 할 듯한데. 이건 생각해보자.

// LoadOrNewCollection rootDir/volume-collection.json 이 있으면 언마샬, 없으면 초기화 시킴.
// 만약 아무것도 없다면 그냥 LoadOrNewCollection("") 이렇게 사용하면 됨.
func LoadOrNewCollection(rootDir string, initialVolumes ...VolumeIndex) (*VolumeCollection, error) {
	// 1) 컬렉션 파일 경로 준비
	path := filepath.Join(rootDir, CollectionFileName)

	// 2) 기존 파일 읽기 시도
	data, err := os.ReadFile(path)
	if err != nil {
		// 2-1) 파일이 없으면 새 컬렉션 생성 + 저장
		if os.IsNotExist(err) {
			coll := NewVolumeCollection(initialVolumes...)
			if err := saveCollection(rootDir, *coll); err != nil {
				return nil, fmt.Errorf("failed to save new collection: %w", err)
			}
			return coll, nil
		}
		// 2-2) 그 외 I/O 에러
		return nil, fmt.Errorf("read collection file: %w", err)
	}

	// 3) 파일이 존재하면 JSON 언마샬
	var coll VolumeCollection
	if err := json.Unmarshal(data, &coll); err != nil {
		return nil, fmt.Errorf("unmarshal collection JSON: %w", err)
	}
	return &coll, nil
}

// NewVolumeCollection 생성 시 초기 Version 을 1 로 설정
func NewVolumeCollection(initialVolumes ...VolumeIndex) *VolumeCollection {
	coll := &VolumeCollection{
		Version: 1,
		Volumes: make([]VolumeIndex, len(initialVolumes)),
	}
	copy(coll.Volumes, initialVolumes)
	return coll
}

// loadMetadataJSON (위의) 임의의 json 파일을 읽어와서 []byte 로 변환, -> config blob 으로 채워짐.
func loadMetadataJSON(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read JSON file %s: %w", path, err)
	}
	// 선택적으로 JSON 유효성 검사
	var tmp interface{}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	return data, nil
}

// HasVolume VolumeCollection 에 추가할 인덱스가 이미 들어있는지 검사합니다.
func (vc *VolumeCollection) HasVolume(vi VolumeIndex) bool {
	for _, existing := range vc.Volumes {
		// DisplayName 또는 VolumeRef 중 하나라도 같으면 이미 존재한 것으로 간주
		if existing.DisplayName == vi.DisplayName || existing.VolumeRef == vi.VolumeRef {
			return true
		}
	}
	return false
}

// Merge 새 컬렉션을 기존 컬렉션에 병합, 중복되지 않은 VolumeIndex 만 추가, 하나라도 추가되면 Version 을 +1 하고 true 를 리턴, 아무것도 추가되지 않으면 false 를 리턴
func (vc *VolumeCollection) Merge(newColl VolumeCollection) bool {
	added := false
	for _, vi := range newColl.Volumes {
		if !vc.HasVolume(vi) {
			vc.Volumes = append(vc.Volumes, vi)
			added = true
		}
	}
	if added {
		vc.Version++
	}
	return added
}

// AddVolume Volumes 에 새 Volume 을 append 하고 Version  +1
func (vc *VolumeCollection) AddVolume(v VolumeIndex) {
	vc.Volumes = append(vc.Volumes, v)
	vc.Version++
}

// RemoveVolume 인덱스 위치의 Volume 을 삭제하고 Version +1
func (vc *VolumeCollection) RemoveVolume(idx int) error {
	if idx < 0 || idx >= len(vc.Volumes) {
		return fmt.Errorf("index %d out of range", idx)
	}
	vc.Volumes = append(vc.Volumes[:idx], vc.Volumes[idx+1:]...)
	vc.Version++
	return nil
}

// saveCollection: coll 을 rootDir/volume-collection.json 에 저장
func saveCollection(rootDir string, coll VolumeCollection) error {
	path := filepath.Join(rootDir, CollectionFileName)
	data, err := json.MarshalIndent(coll, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// TODO volume-index.json 이거 받도록 해주고, 이거 제외해줘야 한다. 다른 폴더에 저장하던가.
// TODO json 분리하는 방안도 생각해보자.

// GenerateVolumeIndex 디렉토리를 검사하면서 초기 VolumeIndex 를 생성함. displayName 사용자에게 보여줄 volume 이름.
func GenerateVolumeIndex(rootPath, displayName string) (*VolumeIndex, error) {
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	rootBase := filepath.Base(rootPath)
	var parts []Partition
	//  Golang 1.16 에 적용됨. os.Stat 사용안해도 됨.
	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error accessing %s: %w", path, err)
		}
		// 루트 자체는 건너뛴다
		if path == rootPath {
			return nil
		}
		// 디렉토리가 아니면 스킵
		if !d.IsDir() {
			return nil
		}

		// 디렉토리 내부를 한 번에 읽어서 marker 존재 확인
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return fmt.Errorf("failed to read dir %s: %w", path, readErr)
		}
		hasMarker := false
		for _, e := range entries {
			if e.Name() == "no_deep_scan" && !e.IsDir() {
				hasMarker = true
				break
			}
		}

		// 상대경로 계산 및 parts 추가
		rel, relErr := filepath.Rel(rootPath, path)
		if relErr != nil {
			return fmt.Errorf("failed to get rel path for %s: %w", path, relErr)
		}
		slashRel := filepath.ToSlash(rel)
		fullPath := fmt.Sprintf("%s/%s", rootBase, slashRel)
		parts = append(parts, Partition{
			Name:        d.Name(),
			Path:        fullPath,
			ManifestRef: "",
			CreatedAt:   now,
			Compression: "",
		})

		// marker 가 있으면 하위 트리 스킵
		if hasMarker {
			return fs.SkipDir
		}
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
	if err := os.WriteFile(outFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", outFile, err)
	}
	return nil
}

// TODO 파일 이름은 고정해두어야 할 거같은데 정하지 못했다.
// TODO 이렇게 임의의 key, value 로 잡아 두도록 한다.

/*
{
  "referenceName": "GRCh38",
  "organism": "Homo sapiens",
  "sequenceCount": 24,
  "totalBaseCount": 3200456742,
  "format": "FASTA.GZ",
  "compression": "gzip",
  "lineWrap": 60,
  "created": "2025-07-15T19:30:00Z",
  "pipelineVersion": "v2.3.1",
  "checksum": "sha256:abcdef1234567890…",
  "checksumAlgorithm": "sha256",
  "description": "Primary tumor WGS FASTA, trimmed and filtered",
  "notes": "Adapters removed with Trimmomatic v0.39; low-quality bases (<Q20) filtered"
}
*/

// TODO 별도의 MediaType 을 설정할지 고민하자. 지금은 현재 image 를 차용해서 사용하고 있다.

// PublishVolume GenerateVolumeIndex 에서 생성된 VolumeIndex 를 받아 OCI 스토어에 올리고, 업데이트된 VolumeIndex 를 리턴한다. volName 은 tag 까지 포함한다.
// TODO wraper method 를 하나 더 두어서 OCIStore 이렇게 처리 하지 말고, 입력받도록 처리하는 메서드를 하나 더 두어서 다양항하게 접근하자. <- 이건 생각해보자.
func (vi *VolumeIndex) PublishVolume(ctx context.Context, volPath, volName string, configBlob []byte) (*VolumeIndex, error) {
	// 1) Init OCI store
	store, err := oci.New(OCIStore)
	if err != nil {
		return nil, fmt.Errorf("init OCI store: %w", err)
	}

	anyPushed := false
	// helper: push blob if not exists, returns pointer to bool pushed
	pushIfNeeded := func(desc ocispec.Descriptor, r io.Reader) (*bool, error) {
		exists, err := store.Exists(ctx, desc)
		if err != nil {
			return nil, fmt.Errorf("check exists (%s): %w", desc.Digest, err)
		}
		if exists {
			Log.Infof("blob %s already exists, skipping", desc.Digest)
			skipped := false
			return &skipped, nil
		}
		if err := store.Push(ctx, desc, r); err != nil {
			return nil, fmt.Errorf("push blob (%s): %w", desc.Digest, err)
		}
		pushed := true
		return &pushed, nil
	}

	// 2) Push config blob
	configDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.FromBytes(configBlob),
		Size:      int64(len(configBlob)),
	}
	pushedPtr, err := pushIfNeeded(configDesc, bytes.NewReader(configBlob))
	if err != nil {
		return nil, err
	}
	if pushedPtr != nil && *pushedPtr {
		anyPushed = true
	}

	// 3) Pack and push layers
	rootBase := filepath.Base(volPath)
	layers := make([]ocispec.Descriptor, 0, len(vi.Partitions))

	if len(vi.Partitions) == 0 {
		// fallback: whole volPath as one layer
		layerData, err := TarGzDir(volPath, rootBase)
		if err != nil {
			return nil, fmt.Errorf("tar.gz fallback %q: %w", volPath, err)
		}
		desc := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageLayerGzip,
			Digest:    digest.FromBytes(layerData),
			Size:      int64(len(layerData)),
		}
		pushedPtr, err := pushIfNeeded(desc, bytes.NewReader(layerData))
		if err != nil {
			return nil, fmt.Errorf("push fallback layer: %w", err)
		}
		if pushedPtr != nil && *pushedPtr {
			anyPushed = true
		}
		layers = append(layers, desc)
	} else {
		for i := range vi.Partitions {
			part := &vi.Partitions[i]
			fsPath := filepath.Join(volPath, strings.TrimPrefix(part.Path, rootBase+"/"))
			layerData, err := TarGzDir(fsPath, part.Path)
			if err != nil {
				return nil, fmt.Errorf("tar.gz %q: %w", fsPath, err)
			}
			desc := ocispec.Descriptor{
				MediaType: ocispec.MediaTypeImageLayerGzip,
				Digest:    digest.FromBytes(layerData),
				Size:      int64(len(layerData)),
			}
			pushedPtr, err := pushIfNeeded(desc, bytes.NewReader(layerData))
			if err != nil {
				return nil, fmt.Errorf("push layer %s: %w", part.Name, err)
			}
			if pushedPtr != nil && *pushedPtr {
				anyPushed = true
			}
			part.ManifestRef = desc.Digest.String()
			layers = append(layers, desc)
		}
	}

	// If nothing changed, skip manifest update
	if !anyPushed {
		existingDesc, err := store.Resolve(ctx, volName)
		if err == nil {
			Log.Infof("No changes detected (config+layers), skipping manifest update for %q", volName)
			vi.VolumeRef = existingDesc.Digest.String()
			return vi, nil
		}
		// If resolve failed (e.g., not found), continue to create manifest
	}

	// 4) Create & tag new manifest
	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1,
		ocispec.MediaTypeImageManifest,
		oras.PackManifestOptions{
			ConfigDescriptor: &configDesc,
			Layers:           layers,
			ManifestAnnotations: map[string]string{
				ocispec.AnnotationCreated: time.Now().UTC().Format(time.RFC3339),
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("pack manifest: %w", err)
	}
	if err := store.Tag(ctx, manifestDesc, volName); err != nil {
		return nil, fmt.Errorf("tag manifest %q: %w", volName, err)
	}
	vi.VolumeRef = manifestDesc.Digest.String()

	Log.Infof("Volume artifact %s tagged as %s", volName, manifestDesc.Digest)
	return vi, nil
}

// PushLocalToRemote 일단 localRepoPath 를 직접 입력 받도록 했다. plainHTTP = true, 이면 http, false 이면 https 적용된다. 디폴트는 https 이다.
// TODO 푸쉬 실패와 에러는 구분해주자. 즉, 로컬에 없어서 푸쉬 실패할때도 에러로 처리하는데 이건 구분해저야 할듯.
func PushLocalToRemote(ctx context.Context, localRepoPath, tag, remoteRepo, user, pass string, plainHTTP bool) error {
	// 1) Initialize local OCI store
	srcStore, err := oci.New(localRepoPath)
	if err != nil {
		return fmt.Errorf("failed to init local OCI store: %w", err)
	}

	// 2) Connect to remote repository
	repo, err := remote.NewRepository(remoteRepo)
	if err != nil {
		return fmt.Errorf("failed to connect to remote repository: %w", err)
	}
	// Use HTTP instead of HTTPS if requested
	if plainHTTP {
		repo.PlainHTTP = true
	}

	// 3) Set up authentication and retry client
	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(repo.Reference.Registry, auth.Credential{Username: user, Password: pass}),
	}
	// TODO 여기서 local 에 tag 이름으로 있는지 확인해야 할듯하다. 없으면 copy 않하도록 구분 지어주는 방향으로 가자.
	// 4) Perform copy: local(tag) -> remote(tag)
	pushedDesc, err := oras.Copy(ctx, srcStore, tag, repo, tag, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("failed to push to remote registry: %w", err)
	}

	fmt.Println("Pushed to remote:", pushedDesc.Digest)
	return nil
}

// TODO 버그 있고 병렬적으로 가져오도록 해야 한다. 이건 테스트 진행해야 한다.

// FetchVolumeFromOCI 은 repo:tag 로 푸시된 볼륨 아티팩트를 destRoot 아래에 풀고,
// 재구성된 VolumeIndex 를 반환함. TODO 수정해줘야 함. 다음에는 이것부터 하자.
func FetchVolumeFromOCI(ctx context.Context, destRoot, repo, tag string) (*VolumeIndex, error) {
	// 1) Open OCI store
	store, err := oci.New(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to open OCI store at %s: %w", repo, err)
	}

	// 2) Resolve manifest descriptor
	ref := fmt.Sprintf("%s:%s", repo, tag)
	manifestDesc, err := store.Resolve(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve reference %q: %w", ref, err)
	}

	// 3) Fetch and decode manifest
	rc, err := store.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer rc.Close()

	var manifest ocispec.Manifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	// 4) Initialize VolumeIndex
	vi := &VolumeIndex{
		VolumeRef:  manifestDesc.Digest.String(),
		Partitions: make([]Partition, len(manifest.Layers)),
	}

	// 5) For each layer, fetch & extract
	for i, layerDesc := range manifest.Layers {
		layerRC, err := store.Fetch(ctx, layerDesc)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch layer %s: %w", layerDesc.Digest, err)
		}
		defer layerRC.Close()

		partPath := layerDesc.Annotations["org.example.partitionPath"]
		targetDir := filepath.Join(destRoot, partPath)
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", targetDir, err)
		}

		if err := UntarGzDir(layerRC, targetDir); err != nil {
			return nil, fmt.Errorf("failed to extract layer %s: %w", layerDesc.Digest, err)
		}

		vi.Partitions[i] = Partition{
			Name:        partPath,
			Path:        partPath,
			ManifestRef: layerDesc.Digest.String(),
		}
	}

	// 6) Write out volume-index.json
	indexPath := filepath.Join(destRoot, "volume-index.json")
	indexBytes, err := json.MarshalIndent(vi, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal VolumeIndex: %w", err)
	}
	if err := os.WriteFile(indexPath, indexBytes, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", indexPath, err)
	}

	return vi, nil
}

// UntarGzDir 는 gzip 스트림을 해제하여 dest 디렉터리에 tar 파일 내용을 풀어 준다. TODO filepath.clean 이거 다른 코드에도 적용해야 함. close 에러 처리 해야함. (시간날때 처리하자)
func UntarGzDir(gzipStream io.Reader, dest string) error {
	// Initialize gzip reader
	gz, err := gzip.NewReader(gzipStream)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break // end of archive
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		// Clean entry name and build target path
		entry := filepath.Clean(hdr.Name)
		target := filepath.Join(dest, entry)
		mode := hdr.FileInfo().Mode()

		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create directory with permissions
			if err := os.MkdirAll(target, mode.Perm()); err != nil {
				return fmt.Errorf("mkdir %q: %w", target, err)
			}

		case tar.TypeReg, tar.TypeRegA:
			// Ensure parent directory exists
			parentDir := filepath.Dir(target)
			if err := os.MkdirAll(parentDir, 0o755); err != nil {
				return fmt.Errorf("mkdir parent %q: %w", parentDir, err)
			}
			// Create or truncate file with correct permissions
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
			if err != nil {
				return fmt.Errorf("open file %q: %w", target, err)
			}
			// Copy file contents
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("copy file %q: %w", target, err)
			}
			// Close and restore permission
			if err := f.Close(); err != nil {
				return fmt.Errorf("close file %q: %w", target, err)
			}

		case tar.TypeSymlink:
			// Ensure parent directory exists
			parentDir := filepath.Dir(target)
			if err := os.MkdirAll(parentDir, 0o755); err != nil {
				return fmt.Errorf("mkdir parent for symlink %q: %w", parentDir, err)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("symlink %q -> %q: %w", target, hdr.Linkname, err)
			}

		default:
			// Skip other file types (hard links, devices, etc.)
			continue
		}
	}
	return nil
}

// TarGzDir 입력값이 변하지 않는다면 sha 를 고정적으로 만들어주면서 압축된 tarball 만들어줌.
func TarGzDir(fsDir, prefixPath string) ([]byte, error) {
	// 1) Collect all paths under fsDir
	var entries []string
	if err := filepath.WalkDir(fsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries = append(entries, path)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(entries)

	// 2) Prepare gzip + tar writers
	buf := &bytes.Buffer{}
	gw, err := gzip.NewWriterLevel(buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	// Fix gzip header for determinism
	gw.Header.ModTime = time.Unix(0, 0)
	gw.Header.OS = 0

	tw := tar.NewWriter(gw)

	// 3) Stream each entry exactly once
	for _, path := range entries {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(fsDir, path)
		if err != nil {
			return nil, err
		}

		// Compute the name to use inside the tar archive
		var tarName string
		if rel == "." {
			tarName = prefixPath
		} else {
			tarName = filepath.ToSlash(filepath.Join(prefixPath, rel))
		}

		// Create a deterministic header
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil, err
		}
		hdr.Name = tarName
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = ""
		hdr.Gname = ""
		hdr.ModTime = time.Unix(0, 0)

		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}

		// If it's a regular file, copy its contents into the tar
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(tw, f); err != nil {
				cErr := f.Close()
				if cErr != nil {
					return nil, fmt.Errorf("failed to copy file %q: %w and file close error :%w", path, err, cErr)
				}
				return nil, fmt.Errorf("failed to copy file %q: %w", path, err)
			}
			if err := f.Close(); err != nil {
				return nil, fmt.Errorf("failed to close file %q: %w", path, err)
			}
		}
	}

	// 4) Explicitly close writers to flush footers
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("tar.Close failed: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("gzip.Close failed: %w", err)
	}

	// 5) Now buf contains a complete, deterministic .tar.gz
	return buf.Bytes(), nil
}
