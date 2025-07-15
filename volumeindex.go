package sori

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

// TODO annotation 은 config blob 으로 빠짐. 사용자에게 json 을 만들도록 해주었음. 따로 필드를 만들어주지 않음. 한번 생각해보자. 이방법이 더 나을듯한데.

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
const OCIStore = "../repo"

// loadCollection: rootDir/volume-collection.json 이 있으면 언마샬, 없으면 빈 컬렉션 반환
func loadCollection(rootDir string) (VolumeCollection, error) {
	var coll VolumeCollection
	path := filepath.Join(rootDir, CollectionFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return coll, nil
		}
		return coll, err
	}
	if err := json.Unmarshal(data, &coll); err != nil {
		return coll, err
	}
	return coll, nil
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

// HasVolume VolumeCollection 에 추가할 인덱스가 이미 들어있는지 검사합니다.
func (c *VolumeCollection) HasVolume(vi VolumeIndex) bool {
	for _, existing := range c.Volumes {
		// DisplayName 또는 VolumeRef 중 하나라도 같으면 이미 존재한 것으로 간주
		if existing.DisplayName == vi.DisplayName || existing.VolumeRef == vi.VolumeRef {
			return true
		}
	}
	return false
}

// Merge 새 컬렉션을 기존 컬렉션에 병합합니다.
//   - 중복되지 않은 VolumeIndex만 추가
//   - 하나라도 추가되면 Version을 +1 하고 true를 리턴
//   - 아무것도 추가되지 않으면 false를 리턴
func (c *VolumeCollection) Merge(newColl VolumeCollection) bool {
	added := false

	for _, vi := range newColl.Volumes {
		if !c.HasVolume(vi) {
			c.Volumes = append(c.Volumes, vi)
			added = true
		}
	}

	if added {
		c.Version++
	}
	return added
}

// TODO volume-index.json 이거 받도록 해주고, 이거 제외해줘야 한다. 다른 폴더에 저장하던가.
// TODO json 분리하는 방안도 생각해보자.

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
	if err := os.WriteFile(outFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", outFile, err)
	}
	return nil
}

// PublishVolumeAsOCI reads a volume-index.json at indexPath under rootPath,
// creates an OCI layout in `repo`, pushes a config and one layer per partition,
// then packs and tags a manifest with the given tag.
func PublishVolumeAsOCI(ctx context.Context, rootPath, indexPath, repo, tag string, configBlob []byte) (*VolumeIndex, error) {
	// 1) Load volume index
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("read index file %q: %w", indexPath, err)
	}
	var vi VolumeIndex
	if err := json.Unmarshal(data, &vi); err != nil {
		return nil, fmt.Errorf("unmarshal volume index: %w", err)
	}

	// 2) Init OCI store
	store, err := oci.New(repo)
	if err != nil {
		return nil, fmt.Errorf("init OCI store: %w", err)
	}

	// helper: descriptor 가 없으면 push
	pushIfNeeded := func(desc ocispec.Descriptor, r io.Reader) error {
		exists, err := store.Exists(ctx, desc)
		if err != nil {
			return fmt.Errorf("check exists (%s): %w", desc.Digest, err)
		}
		if exists {
			return nil
		}
		if err := store.Push(ctx, desc, r); err != nil {
			return fmt.Errorf("push blob (%s): %w", desc.Digest, err)
		}
		return nil
	}

	// 3) Push config blob
	configDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.FromBytes(configBlob),
		Size:      int64(len(configBlob)),
	}
	if err := pushIfNeeded(configDesc, bytes.NewReader(configBlob)); err != nil {
		return nil, err
	}

	// 4) Pack and push each layer
	rootBase := filepath.Base(rootPath)
	layers := make([]ocispec.Descriptor, 0, len(vi.Partitions))
	for i := range vi.Partitions {
		part := &vi.Partitions[i]
		fsPath := filepath.Join(rootPath, strings.TrimPrefix(part.Path, rootBase+"/"))

		layerData, err := tarGzDirDeterministic(fsPath, part.Path)
		if err != nil {
			return nil, fmt.Errorf("tar.gz %q: %w", fsPath, err)
		}

		desc := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageLayerGzip,
			Digest:    digest.FromBytes(layerData),
			Size:      int64(len(layerData)),
		}
		if err := pushIfNeeded(desc, bytes.NewReader(layerData)); err != nil {
			return nil, fmt.Errorf("push layer %s: %w", part.Name, err)
		}

		part.ManifestRef = desc.Digest.String()
		layers = append(layers, desc)
	}

	// 5) Create & tag manifest
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

	vi.VolumeRef = manifestDesc.Digest.String()
	if err := store.Tag(ctx, manifestDesc, tag); err != nil {
		return nil, fmt.Errorf("tag manifest %q: %w", tag, err)
	}

	// 6) Persist updated index
	out, err := json.MarshalIndent(vi, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal updated index: %w", err)
	}
	if err := os.WriteFile(indexPath, out, 0o644); err != nil {
		return nil, fmt.Errorf("write updated index: %w", err)
	}

	fmt.Printf("✅ Volume artifact %s:%s saved\n", repo, tag)
	return &vi, nil
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

// loadMetadataJSON 임의의 json 파일을 읽어와서 []byte 로 변환
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

// PublishVolume 는 이미 파싱된 VolumeIndex 를 받아 OCI 스토어에 올리고, 업데이트된 VolumeIndex 를 리턴한다.
func (vi *VolumeIndex) PublishVolume(ctx context.Context, volPath, volName string, configBlob []byte) (*VolumeIndex, error) {
	// 1) Init OCI store
	store, err := oci.New(OCIStore)
	if err != nil {
		return nil, fmt.Errorf("init OCI store: %w", err)
	}

	// helper: descriptor 가 없으면 push
	pushIfNeeded := func(desc ocispec.Descriptor, r io.Reader) error {
		exists, err := store.Exists(ctx, desc)
		if err != nil {
			return fmt.Errorf("check exists (%s): %w", desc.Digest, err)
		}
		if exists {
			return nil
		}
		if err := store.Push(ctx, desc, r); err != nil {
			return fmt.Errorf("push blob (%s): %w", desc.Digest, err)
		}
		return nil
	}

	// 2) Push config blob
	configDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.FromBytes(configBlob),
		Size:      int64(len(configBlob)),
	}
	if err := pushIfNeeded(configDesc, bytes.NewReader(configBlob)); err != nil {
		return nil, err
	}

	// 3) Pack and push each layer
	rootBase := filepath.Base(volPath)
	layers := make([]ocispec.Descriptor, 0, len(vi.Partitions))
	for i := range vi.Partitions {
		part := &vi.Partitions[i]
		fsPath := filepath.Join(volPath, strings.TrimPrefix(part.Path, rootBase+"/"))

		layerData, err := tarGzDirDeterministic(fsPath, part.Path)
		if err != nil {
			return nil, fmt.Errorf("tar.gz %q: %w", fsPath, err)
		}

		desc := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageLayerGzip,
			Digest:    digest.FromBytes(layerData),
			Size:      int64(len(layerData)),
		}
		if err := pushIfNeeded(desc, bytes.NewReader(layerData)); err != nil {
			return nil, fmt.Errorf("push layer %s: %w", part.Name, err)
		}

		part.ManifestRef = desc.Digest.String()
		layers = append(layers, desc)
	}

	// 4) Create & tag manifest
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

	vi.VolumeRef = manifestDesc.Digest.String()
	if err := store.Tag(ctx, manifestDesc, volName); err != nil {
		return nil, fmt.Errorf("tag manifest %q: %w", volName, err)
	}

	// 5) 결과 반환 (파일 쓰기는 호출자에게)
	Log.Infof("Volume artifact %s saved\n", volName)
	return vi, nil
}

// TODO 메서드 바뀔 수 있음. 일단 이렇게 해둠. 서비스 방식에 따라 달라질 수 있음.

// NewVolumeCollection 생성 시 초기 Version을 1 로 설정
func NewVolumeCollection(initialVolumes ...VolumeIndex) *VolumeCollection {
	coll := &VolumeCollection{
		Version: 1,
		Volumes: make([]VolumeIndex, len(initialVolumes)),
	}
	copy(coll.Volumes, initialVolumes)
	return coll
}

// AddVolume Volumes에 새 Volume을 append하고 Version  +1
func (c *VolumeCollection) AddVolume(v VolumeIndex) {
	c.Volumes = append(c.Volumes, v)
	c.Version++
}

// RemoveVolume 인덱스 위치의 Volume을 삭제하고 Version +1
func (c *VolumeCollection) RemoveVolume(idx int) error {
	if idx < 0 || idx >= len(c.Volumes) {
		return fmt.Errorf("index %d out of range", idx)
	}
	c.Volumes = append(c.Volumes[:idx], c.Volumes[idx+1:]...)
	c.Version++
	return nil
}

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

	// 4) Perform copy: local(tag) -> remote(tag)
	pushedDesc, err := oras.Copy(ctx, srcStore, tag, repo, tag, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("failed to push to remote registry: %w", err)
	}

	fmt.Println("✅ Pushed to remote:", pushedDesc.Digest)
	return nil
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

// TODO 버그 있고 병렬적으로 가져오도록 해야 한다. 이건 테스트 진행해야 한다.

// FetchVolumeFromOCI 은 repo:tag 로 푸시된 볼륨 아티팩트를 destRoot 아래에 풀고,
// 재구성된 VolumeIndex 를 반환합니다.
func FetchVolumeFromOCI(ctx context.Context, destRoot string, repo, tag string) (*VolumeIndex, error) {
	// 1) OCI 스토어 열기
	store, err := oci.New(repo)
	if err != nil {
		return nil, fmt.Errorf("OCI 스토어 열기 실패 %s: %w", repo, err)
	}

	// 2) 매니페스트 Descriptor 조회
	ref := fmt.Sprintf("%s:%s", repo, tag)
	manifestDesc, err := store.Resolve(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("해당 참조 조회 실패 %s: %w", ref, err)
	}

	rc, err := store.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("매니페스트 Fetch 실패: %w", err)
	}
	defer rc.Close()

	var manifest ocispec.Manifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("매니페스트 스트림 디코딩 실패: %w", err)
	}

	// 4) VolumeIndex 초기화
	vi := &VolumeIndex{
		VolumeRef:  manifestDesc.Digest.String(),
		Partitions: make([]Partition, len(manifest.Layers)),
	}

	// 5) 각 레이어(파티션) 풀기
	for i, layerDesc := range manifest.Layers {
		// 레이어 Reader 얻기 (tar.gz)
		rc, err := store.Fetch(ctx, layerDesc)
		if err != nil {
			return nil, fmt.Errorf("레이어 리더 생성 실패 %s: %w", layerDesc.Digest, err)
		}
		defer rc.Close()

		// 원래 파티션 경로는 레이어 어노테이션에 저장돼 있다고 가정
		partPath := layerDesc.Annotations["org.example.partitionPath"]
		targetDir := filepath.Join(destRoot, partPath)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return nil, fmt.Errorf("디렉터리 생성 실패 %s: %w", targetDir, err)
		}

		// 압축 해제
		if err := extractTarGz(rc, targetDir); err != nil {
			return nil, fmt.Errorf("레이어 압축 해제 실패 %s: %w", layerDesc.Digest, err)
		}

		// VolumeIndex 에 기록
		vi.Partitions[i] = Partition{
			Name:        partPath,
			Path:        partPath,
			ManifestRef: layerDesc.Digest.String(),
		}
	}

	// 6) volume-index.json 다시 쓰기 (선택)
	indexBytes, err := json.MarshalIndent(vi, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("VolumeIndex 마샬링 실패: %w", err)
	}
	if err := os.WriteFile(
		filepath.Join(destRoot, "volume-index.json"),
		indexBytes,
		0644,
	); err != nil {
		return nil, fmt.Errorf("volume-index.json 쓰기 실패: %w", err)
	}

	return vi, nil
}

// extractTarGz 는 gzip 스트림을 해제하여 dest 디렉터리에 tar 파일 내용을 풀어 줍니다.
func extractTarGz(gzipStream io.Reader, dest string) error {
	gz, err := gzip.NewReader(gzipStream)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // 아카이브 끝
		} else if err != nil {
			return err
		}

		target := filepath.Join(dest, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			f, err := os.OpenFile(
				target,
				os.O_CREATE|os.O_WRONLY,
				os.FileMode(hdr.Mode),
			)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
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
