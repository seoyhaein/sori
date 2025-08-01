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
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type (
	Partition struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		ManifestRef string `json:"manifest_ref"`
		CreatedAt   string `json:"created_at"`
		Compression string `json:"compression"`
	}
	VolumeIndex struct {
		VolumeRef   string      `json:"volume_ref"`
		DisplayName string      `json:"display_name"`
		CreatedAt   string      `json:"created_at"`
		Partitions  []Partition `json:"partitions"`
	}
	ConfigBlob  map[string]interface{}
	VolumeEntry struct {
		Index      VolumeIndex `json:"index"`
		ConfigBlob ConfigBlob  `json:"configBlob"`
	}
	VolumeCollection struct {
		Version int           `json:"version"` // 변경될 때마다 +1
		Volumes []VolumeEntry `json:"volumes"`
	}
	CollectionManager struct {
		mu    sync.RWMutex
		root  string // Configuration root directory
		coll  *VolumeCollection
		byRef map[string]int
	}
)

const (
	ConfigBlobJson  = "configblob.json"
	CollectionJson  = "volume-collection.json"
	VolumeIndexJson = "volume-index.json"
)

func NewCollectionManager(rootDir string, initial ...VolumeEntry) (*CollectionManager, error) {
	coll, err := LoadOrNewCollection(rootDir, initial...)
	if err != nil {
		return nil, err
	}

	m := &CollectionManager{
		root:  rootDir,
		coll:  coll,
		byRef: make(map[string]int, len(coll.Volumes)),
	}
	// 이제 entry.Index.VolumeRef 를 기준으로 인덱스 맵 구성
	for i, entry := range coll.Volumes {
		ref := entry.Index.VolumeRef
		if ref != "" {
			m.byRef[ref] = i
		}
	}
	return m, nil
}

// LoadOrNewCollection now takes initial VolumeEntry objects, not VolumeIndex.
func LoadOrNewCollection(rootDir string, initialEntries ...VolumeEntry) (*VolumeCollection, error) {
	// 1) 컬렉션 파일 경로 준비
	path := filepath.Join(rootDir, CollectionJson)

	// 2) 기존 파일 읽기 시도
	data, err := os.ReadFile(path)
	if err != nil {
		// 2-1) 파일이 없으면 새 컬렉션 생성 + 저장
		if os.IsNotExist(err) {
			coll := NewVolumeCollection(initialEntries...)
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

// NewVolumeCollection 이제 VolumeEntry 를 받습니다.
func NewVolumeCollection(initialEntries ...VolumeEntry) *VolumeCollection {
	coll := &VolumeCollection{
		Version: 1,
		Volumes: make([]VolumeEntry, len(initialEntries)),
	}
	copy(coll.Volumes, initialEntries)
	return coll
}

func (m *CollectionManager) AddOrUpdate(v VolumeEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ref := v.Index.VolumeRef
	if idx, ok := m.byRef[ref]; ok {
		// 내용이 바뀌었는지 비교 (Index+ConfigBlob 전체)
		if !reflect.DeepEqual(m.coll.Volumes[idx], v) {
			m.coll.Volumes[idx] = v
			m.coll.Version++
		} else {
			// 동일하면 저장 생략
			return nil
		}
	} else {
		// 새로 추가
		m.coll.Volumes = append(m.coll.Volumes, v)
		m.byRef[ref] = len(m.coll.Volumes) - 1
		m.coll.Version++
	}

	return saveCollection(m.root, *m.coll)
}

func (m *CollectionManager) Remove(ref string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx, ok := m.byRef[ref]
	if !ok {
		return false, nil
	}

	last := len(m.coll.Volumes) - 1
	if idx != last {
		// 끝 요소를 현재 위치로 스왑
		m.coll.Volumes[idx] = m.coll.Volumes[last]
		// 옮겨진 엔트리의 VolumeRef 로 인덱스 맵 수정
		movedRef := m.coll.Volumes[idx].Index.VolumeRef
		m.byRef[movedRef] = idx
	}

	// 마지막 요소 잘라내기
	m.coll.Volumes = m.coll.Volumes[:last]
	// 원래 ref 키 삭제
	delete(m.byRef, ref)

	// 버전 ++
	m.coll.Version++

	return true, saveCollection(m.root, *m.coll)
}

func (m *CollectionManager) GetSnapshot() VolumeCollection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Version 복사 + VolumeEntry slice 할당
	out := VolumeCollection{
		Version: m.coll.Version,
		Volumes: make([]VolumeEntry, len(m.coll.Volumes)),
	}

	// 각 VolumeEntry를 깊은 복사
	for i, entry := range m.coll.Volumes {
		// ConfigBlob(map) 깊은 복사
		blobCopy := make(ConfigBlob, len(entry.ConfigBlob))
		for k, v := range entry.ConfigBlob {
			blobCopy[k] = v
		}
		// Index는 값 복사로 충분
		out.Volumes[i] = VolumeEntry{
			Index:      entry.Index,
			ConfigBlob: blobCopy,
		}
	}

	return out
}

func (m *CollectionManager) Get(ref string) (VolumeEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	idx, ok := m.byRef[ref]
	if !ok {
		return VolumeEntry{}, false
	}
	return m.coll.Volumes[idx], true
}

func (m *CollectionManager) Flush() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return saveCollection(m.root, *m.coll)
}

func (m *CollectionManager) PublishVolumeFromDir(ctx context.Context, volDir, displayName, tag string) error {
	// 1) 디렉토리 검증 및 configblob.json 로드/생성
	rawConfig, err := ValidateVolumeDir(volDir)
	if err != nil {
		return fmt.Errorf("ValidateVolumeDir failed: %w", err)
	}

	// 2) raw JSON → ConfigBlob
	var cb ConfigBlob
	if err := json.Unmarshal(rawConfig, &cb); err != nil {
		return fmt.Errorf("failed to parse configblob.json: %w", err)
	}

	// 3) VolumeIndex 생성
	vi, err := GenerateVolumeIndex(volDir, displayName)
	if err != nil {
		return fmt.Errorf("GenerateVolumeIndex failed: %w", err)
	}

	// 4) OCI에 퍼블리시
	newVi, err := vi.PublishVolume(ctx, volDir, tag, rawConfig)
	if err != nil {
		return fmt.Errorf("PublishVolume failed: %w", err)
	}
	if newVi == nil {
		return fmt.Errorf("PublishVolume returned nil VolumeIndex")
	}

	// 5) 컬렉션에 추가
	entry := VolumeEntry{
		Index:      *newVi,
		ConfigBlob: cb,
	}
	if err := m.AddOrUpdate(entry); err != nil {
		return fmt.Errorf("AddOrUpdate failed: %w", err)
	}

	return nil
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
	for _, entry := range vc.Volumes {
		// DisplayName 또는 VolumeRef 중 하나라도 같으면 이미 존재한 것으로 간주
		if entry.Index.DisplayName == vi.DisplayName ||
			entry.Index.VolumeRef == vi.VolumeRef {
			return true
		}
	}
	return false
}

// Merge 새 컬렉션을 기존 컬렉션에 병합, 중복되지 않은 VolumeIndex 만 추가, 하나라도 추가되면 Version 을 +1 하고 true 를 리턴, 아무것도 추가되지 않으면 false 를 리턴
func (vc *VolumeCollection) Merge(newColl VolumeCollection) bool {
	added := false
	for _, entry := range newColl.Volumes {
		// entry.Index는 VolumeIndex 타입
		if !vc.HasVolume(entry.Index) {
			vc.Volumes = append(vc.Volumes, entry)
			added = true
		}
	}
	if added {
		vc.Version++
	}
	return added
}

// AddVolume Volumes 에 새 Volume 을 append 하고 Version  +1
func (vc *VolumeCollection) AddVolume(entry VolumeEntry) {
	vc.Volumes = append(vc.Volumes, entry)
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
	path := filepath.Join(rootDir, CollectionJson)
	data, err := json.MarshalIndent(coll, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

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
	outFile := filepath.Join(rootPath, VolumeIndexJson)
	data, err := json.MarshalIndent(vi, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal volume index: %w", err)
	}
	if err := os.WriteFile(outFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", outFile, err)
	}
	return nil
}

// PublishVolume GenerateVolumeIndex 에서 생성된 VolumeIndex 를 받아 OCI 스토어에 올리고, 업데이트된 VolumeIndex 를 리턴한다. volName 은 tag 까지 포함한다.
func (vi *VolumeIndex) PublishVolume(ctx context.Context, volPath, volName string, configBlob []byte) (*VolumeIndex, error) {
	// 1) Init OCI store
	store, err := oci.New(ociStore)
	if err != nil {
		return nil, fmt.Errorf("init OCI store: %w", err)
	}

	// TODO 이 메서드 따로 떼어내자. 확장성있게 만들자.
	anyPushed := false
	// helper: push blob if not exists, returns a pointer to bool pushed
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

// FetchVolSeq 은 repo:tag 로 푸시된 볼륨 아티팩트를 destRoot 아래에 풀고,
// 재구성된 VolumeIndex 를 반환함. TODO 수정해줘야 함. 다음에는 이것부터 하자. 테스트 하고 정교화 하자.
func FetchVolSeq(ctx context.Context, destRoot, repo, tag string) (*VolumeIndex, error) {
	store, err := oci.New(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to open OCI store at %s: %w", repo, err)
	}

	ref := fmt.Sprintf("%s:%s", repo, tag)
	manifestDesc, err := store.Resolve(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve reference %q: %w", ref, err)
	}

	rc, err := store.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	// manifest reader 하나만 defer
	defer rc.Close()

	var manifest ocispec.Manifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	vi := &VolumeIndex{
		VolumeRef:  manifestDesc.Digest.String(),
		Partitions: make([]Partition, len(manifest.Layers)),
	}

	seen := make(map[string]struct{})

	for i, layerDesc := range manifest.Layers {
		layerRC, err := store.Fetch(ctx, layerDesc)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch layer %s: %w", layerDesc.Digest, err)
		}

		partPath := layerDesc.Annotations["org.example.partitionPath"]
		if partPath == "" {
			layerRC.Close()
			return nil, fmt.Errorf("missing partitionPath annotation for layer %s", layerDesc.Digest)
		}
		if _, dup := seen[partPath]; dup {
			layerRC.Close()
			return nil, fmt.Errorf("duplicate partition path %q", partPath)
		}
		seen[partPath] = struct{}{}

		targetDir := filepath.Join(destRoot, partPath)
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			layerRC.Close()
			return nil, fmt.Errorf("failed to create directory %s: %w", targetDir, err)
		}

		if err := UntarGzDir(layerRC, targetDir); err != nil {
			layerRC.Close()
			return nil, fmt.Errorf("failed to extract layer %s: %w", layerDesc.Digest, err)
		}

		if err := layerRC.Close(); err != nil {
			return nil, fmt.Errorf("failed to close layer reader %s: %w", layerDesc.Digest, err)
		}

		vi.Partitions[i] = Partition{
			Name:        partPath,
			Path:        partPath,
			ManifestRef: layerDesc.Digest.String(),
		}
	}

	indexPath := filepath.Join(destRoot, VolumeIndexJson)
	indexBytes, err := json.MarshalIndent(vi, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal VolumeIndex: %w", err)
	}
	if err := os.WriteFile(indexPath, indexBytes, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", indexPath, err)
	}

	return vi, nil
}

// FetchVolParallel TODO 테스트 해봐야 함.
func FetchVolParallel(ctx context.Context, destRoot, repo, tag string, concurrency int) (*VolumeIndex, error) {
	store, err := oci.New(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to open OCI store at %s: %w", repo, err)
	}

	manifestDesc, err := store.Resolve(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve reference %q: %w", fmt.Sprintf("%s:%s", repo, tag), err)
	}

	rc, err := store.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer rc.Close()

	var manifest ocispec.Manifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	n := len(manifest.Layers)
	vi := &VolumeIndex{
		VolumeRef:  manifestDesc.Digest.String(),
		Partitions: make([]Partition, n),
	}

	// ----- 1단계: 메타 검사 -----
	seen := make(map[string]struct{}, n)
	type layerMeta struct {
		idx  int
		desc ocispec.Descriptor
		path string
	}
	metas := make([]layerMeta, 0, n)

	for i, layer := range manifest.Layers {
		partPath := layer.Annotations["org.example.partitionPath"]
		if partPath == "" {
			return nil, fmt.Errorf("missing partitionPath annotation for layer %s", layer.Digest)
		}
		if _, dup := seen[partPath]; dup {
			return nil, fmt.Errorf("duplicate partition path %q", partPath)
		}
		seen[partPath] = struct{}{}
		metas = append(metas, layerMeta{i, layer, partPath})
	}

	if concurrency <= 0 || concurrency > n {
		// 기본값: CPU 코어 수와 n 중 작은 값
		cpu := runtime.NumCPU()
		if cpu < 1 {
			cpu = 1
		}
		if cpu > n {
			cpu = n
		}
		concurrency = cpu
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// ----- 2단계: 병렬 처리 -----
	type jobResult struct {
		idx int
		p   Partition
		err error
	}

	jobs := make(chan layerMeta)
	results := make(chan jobResult)
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for meta := range jobs {
			select {
			case <-ctx.Done():
				return
			default:
			}

			layerRC, err := store.Fetch(ctx, meta.desc)
			if err != nil {
				results <- jobResult{idx: meta.idx, err: fmt.Errorf("fetch layer %s: %w", meta.desc.Digest, err)}
				cancel()
				continue
			}

			targetDir := filepath.Join(destRoot, meta.path)
			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				layerRC.Close()
				results <- jobResult{idx: meta.idx, err: fmt.Errorf("mkdir %s: %w", targetDir, err)}
				cancel()
				continue
			}

			if err := UntarGzDir(layerRC, targetDir); err != nil {
				layerRC.Close()
				results <- jobResult{idx: meta.idx, err: fmt.Errorf("extract layer %s: %w", meta.desc.Digest, err)}
				cancel()
				continue
			}

			if err := layerRC.Close(); err != nil {
				results <- jobResult{idx: meta.idx, err: fmt.Errorf("close reader %s: %w", meta.desc.Digest, err)}
				cancel()
				continue
			}

			results <- jobResult{
				idx: meta.idx,
				p: Partition{
					Name:        meta.path,
					Path:        meta.path,
					ManifestRef: meta.desc.Digest.String(),
				},
			}
		}
	}

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go worker()
	}

	go func() {
		for _, m := range metas {
			jobs <- m
		}
		close(jobs)
	}()

	var firstErr error
	completed := 0
	for completed < n {
		r := <-results
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		if r.err == nil {
			vi.Partitions[r.idx] = r.p
		}
		completed++
		if firstErr != nil {
			// drain 남은 결과 (cancel 후 워커 종료 대기)
			// 단, 빠르게 빠져나가고 싶으면 break 후 Wait; 여기서는 안전하게 모두 수신
		}
	}
	cancel()
	wg.Wait()
	close(results)

	if firstErr != nil {
		return nil, firstErr
	}

	// ----- 3단계: volume-index.json 기록 -----
	indexPath := filepath.Join(destRoot, VolumeIndexJson)
	indexBytes, err := json.MarshalIndent(vi, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal VolumeIndex: %w", err)
	}
	if err := os.WriteFile(indexPath, indexBytes, 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", indexPath, err)
	}

	return vi, nil
}

// UntarGzDir 는 gzip 스트림을 해제하여 dest 디렉터리에 tar 파일 내용을 풀어 준다.
// TODO filepath.clean 이거 다른 코드에도 적용해야 함. close 에러 처리 해야함. (시간날때 처리하자)
// TODO 해당 메서드 살펴봐야 함.
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

// ValidateVolumeDir 폴더에 대해서 빈폴더이면 에러 리턴, 이때 숨김파일이 있으면, 로그를 남기고 오직 숨김파일만 있으면 에러 리턴,
// 해당 폴더에 configblob.json 이 있는지 검사. 없으면 빈 configblob.json 을 만들어줌.
func ValidateVolumeDir(volDir string) ([]byte, error) {
	// 1) 디렉토리 존재 및 타입 확인
	info, err := os.Stat(volDir)
	if err != nil {
		return nil, fmt.Errorf("volume dir %q does not exist: %w", volDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("volume path %q is not a directory", volDir)
	}

	// 2) 숨김 파일을 제외한 실제 항목이 있는지 검사
	entries, err := os.ReadDir(volDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %q: %w", volDir, err)
	}
	visibleCount := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			Log.Warnf("ValidateVolumeDir: hidden entry %q found in %s, skipping", name, volDir)
			continue
		}
		visibleCount++
	}
	if visibleCount == 0 {
		return nil, fmt.Errorf("volume directory %q is empty (only hidden files present)", volDir)
	}

	// 3) configblob.json 처리 및 raw bytes 반환
	cfgPath := filepath.Join(volDir, ConfigBlobJson)
	raw, err := loadMetadataJSON(cfgPath)
	if err != nil {
		// 파일이 없어서 loadMetadataJSON이 실패한 경우
		if os.IsNotExist(errors.Unwrap(err)) || strings.Contains(err.Error(), "no such file") {
			Log.Infof("ValidateVolumeDir: %q not found; creating a new empty configblob.json", cfgPath)
			raw = []byte("{}")
			if writeErr := os.WriteFile(cfgPath, raw, fs.FileMode(0644)); writeErr != nil {
				return nil, fmt.Errorf("failed to create %q: %w", cfgPath, writeErr)
			}
		} else {
			// JSON 파싱 에러 또는 그 외 I/O 에러
			return nil, err
		}
	}

	// 이제 raw는 유효한 JSON bytes
	return raw, nil
}

// 통합 메서드

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

// TODO 정책을 정해야 하는데 일단, rootDir 안에 볼륨 폴더들이 있는 것이 원칙이다. 하지만 그렇게 하지 않다도 되게 일단 만들어 놓는다.
// -> 이렇게 할경우 어떻게해 할지 생각해봐야 함., 복사해서 넣어줘야 할까??
// TODO 그리고 볼륨 폴더에 는 volume-index.json 이 있어야 한다. 위치는 볼륨 폴더의 루트 위치에 있어야 한다. 그것들을 읽어서 VolumeCollection 을 만들어 주는 방식으로 간다.
// -> 만약 이렇게 안되어 있으면 에러 뱉어내야 함. 그리고 생성할때 저렇게 배치되도록 해줘야 함.
// TODO 이러한 내용은 사용자가 알아도 되지만 몰라도 된다. 이렇게 메서드들이 만들어 주는 방식으로 간다. 사용자 편의성을 위해서 사용자의 입력을 최소한으로 진행한다. <- 이렇게 하면, 메서드도 정리 해야 할 듯한데. 이건 생각해보자.

// 일단 살펴보자.

// TODO 스왑-팝(swap-pop) 패턴 관련 살벼 보자.

// TODO 별도의 MediaType 을 설정할지 고민하자. 지금은 현재 image 를 차용해서 사용하고 있다.
