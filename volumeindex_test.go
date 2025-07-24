package sori

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndSaveVolumeIndex(t *testing.T) {
	// JSON 생성
	vi, err := GenerateVolumeIndex("./test-vol", "TestVol")
	if err != nil {
		t.Fatalf("GenerateVolumeIndex failed: %v", err)
	}

	// 파일로 저장
	if err := vi.SaveToFile("./test-vol"); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}
}

func TestPublishVolumeOther(t *testing.T) {

	volDir := "./test2"
	vi, err := GenerateVolumeIndex(volDir, "테스트 하는 볼륨2")
	if err != nil {
		t.Fatalf("GenerateVolumeIndex failed: %v", err)
	}
	configblob, err := loadMetadataJSON("configblob1.json")
	if err != nil {
		t.Fatalf(" failed to load configblob juson:%v", err)
	}

	_, err = vi.PublishVolume(context.Background(), volDir, "test2.v1.0.0", configblob)
	assert.NoError(t, err)
}

func TestPublishVolumeOther1(t *testing.T) {
	// 동일한 것을 넣으면 어떻게 될까?
	volDir := "./test2"
	vi, err := GenerateVolumeIndex(volDir, "테스트 하는 볼륨2")
	if err != nil {
		t.Fatalf("GenerateVolumeIndex failed: %v", err)
	}
	configblob, err := loadMetadataJSON("configblob1.json")
	if err != nil {
		t.Fatalf(" failed to load configblob juson:%v", err)
	}

	_, err = vi.PublishVolume(context.Background(), volDir, "test2.v1.0.0", configblob)
	assert.NoError(t, err)
}

func TestTarGzDirDeterministic(t *testing.T) {
	tPath := "./test-vol"
	// 두번째 메서드는 풀리는 폴더 이름이라고 보면 됨.
	data1, err := TarGzDir(tPath, "test-vol1")
	if err != nil {
		t.Fatalf("tarGzDirDeterministic failed: %v", err)
	}

	outFile := "test-vol.tar.gz"
	if err := os.WriteFile(outFile, data1, 0o777); err != nil {
		t.Fatalf("failed to write tarball: %v", err)
	}
	t.Logf("wrote deterministic tarball: %s (%d bytes)", outFile, len(data1))
	// tar -xzf test-vol.tar.gz
}

//TODO 몇가지 버그가 있다. 수정해야 한다.

// TestFetchVolumeFromOCI pushes a small volume to a local OCI store, fetches it back, and verifies
// both file contents and VolumeIndex metadata.
func TestFetchVolumeFromOCI(t *testing.T) {
	ctx := context.Background()

	repo := "./repo"
	dest := "./test-vol-restored"
	_, err := FetchVolSeq(ctx, dest, repo, "v1.0.0")
	if err != nil {
		t.Fatalf("FetchVolSeq failed: %v", err)
	}

}

// TestPushLocalToRemote_Harbor 실제 Harbor 레지스트리에 푸시
func TestPushLocalToRemote_Harbor(t *testing.T) {
	ctx := context.Background()

	localTag := "test.v1.0.0"
	remoteRepo := "harbor.local/demo-project/testrepo"
	user := "admin"
	pass := "Harbor12345"
	repo := "./repo"

	// 5) 실제 푸시 호출
	if err := PushLocalToRemote(ctx, repo, localTag, remoteRepo, user, pass, true); err != nil {
		t.Fatalf("Harbor 레지스트리 푸시 실패: %v", err)
	}

	t.Logf("✅ Harbor 에 성공적으로 푸시됨: %s:%s", remoteRepo, localTag)

}

// 부가적인 테스트 코드
// TestMerge_AddNewVolumes checks that Merge adds only non-duplicate volumes
func TestMerge_AddNewVolumes(t *testing.T) {
	existing := VolumeCollection{
		Version: 1,
		Volumes: []VolumeEntry{
			{
				Index:      VolumeIndex{DisplayName: "v1", VolumeRef: "ref1"},
				ConfigBlob: ConfigBlob{},
			},
		},
	}
	newColl := VolumeCollection{
		Volumes: []VolumeEntry{
			{
				Index:      VolumeIndex{DisplayName: "v2", VolumeRef: "ref2"},
				ConfigBlob: ConfigBlob{},
			},
			{
				Index:      VolumeIndex{DisplayName: "v1", VolumeRef: "ref1"}, // duplicate
				ConfigBlob: ConfigBlob{},
			},
		},
	}

	added := existing.Merge(newColl)

	assert.True(t, added, "expected Merge to return true when new volumes are added")
	assert.Len(t, existing.Volumes, 2, "expected two volumes after merge")
	assert.Equal(t, 2, existing.Version, "expected version to increment by 1")
}

// TestMerge_NoVolumesAdded ensures Merge returns false and version unchanged if no new volumes
func TestMerge_NoVolumesAdded(t *testing.T) {
	existing := VolumeCollection{
		Version: 5,
		Volumes: []VolumeEntry{
			{
				Index:      VolumeIndex{DisplayName: "v1", VolumeRef: "ref1"},
				ConfigBlob: ConfigBlob{},
			},
		},
	}
	newColl := VolumeCollection{
		Volumes: []VolumeEntry{
			{
				Index:      VolumeIndex{DisplayName: "v1", VolumeRef: "ref1"},
				ConfigBlob: ConfigBlob{},
			},
		},
	}

	added := existing.Merge(newColl)

	assert.False(t, added, "expected Merge to return false when no new volumes")
	assert.Equal(t, 5, existing.Version, "expected version to remain unchanged")
}

func TestExtractTarGz(t *testing.T) {
	// 1) Build an in‑memory tar.gz with a dir, a file, and a symlink
	buf := &bytes.Buffer{}
	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)

	// -- directory entry --
	dirHdr := &tar.Header{
		Name:     "dir/",
		Typeflag: tar.TypeDir,
		Mode:     0755,
	}
	assert.NoError(t, tw.WriteHeader(dirHdr))

	// -- regular file entry --
	content := []byte("hello world")
	fileHdr := &tar.Header{
		Name:     "dir/file.txt",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
	}
	assert.NoError(t, tw.WriteHeader(fileHdr))
	_, err := tw.Write(content)
	assert.NoError(t, err)

	// -- symlink entry --
	linkHdr := &tar.Header{
		Name:     "link",
		Typeflag: tar.TypeSymlink,
		Linkname: "dir/file.txt",
	}
	assert.NoError(t, tw.WriteHeader(linkHdr))

	// Close writers
	assert.NoError(t, tw.Close())
	assert.NoError(t, gw.Close())

	// 2) Extract into a temp directory
	dest := t.TempDir()
	err = UntarGzDir(bytes.NewReader(buf.Bytes()), dest)
	assert.NoError(t, err)

	// 3) Verify directory was created
	dirPath := filepath.Join(dest, "dir")
	assert.DirExists(t, dirPath)

	// 4) Verify file was created with correct content
	filePath := filepath.Join(dirPath, "file.txt")
	assert.FileExists(t, filePath)
	got, err := os.ReadFile(filePath)
	assert.NoError(t, err)
	assert.Equal(t, content, got)

	// 5) Verify symlink was created and points correctly
	linkPath := filepath.Join(dest, "link")
	info, err := os.Lstat(linkPath)
	assert.NoError(t, err)
	assert.True(t, info.Mode()&os.ModeSymlink != 0, "expected a symlink")

	target, err := os.Readlink(linkPath)
	assert.NoError(t, err)
	assert.Equal(t, "dir/file.txt", target)
}

// TODO 여기서 부터 검증해 나가자.
func TestLoadOrNewCollection_New(t *testing.T) {
	rootDir := "./testRoot"
	_, err := LoadOrNewCollection(rootDir)
	if err != nil {
		t.Fatalf("LoadOrNewCollection failed: %v", err)
	}
}

func TestManager(t *testing.T) {
	rootDir := "./testRoot"
	// 1. 매니저 초기화 (기존 파일 있으면 로드, 없으면 새로 생성)
	manager, err := NewCollectionManager(rootDir)
	if err != nil {
		t.Fatalf("failed to init collection manager: %v", err)
	}

	// 2. 새 볼륨을 하나 만든다고 가정 (예: OCI Push 후 얻은 digest)
	v1 := VolumeEntry{
		Index: VolumeIndex{
			VolumeRef:   "sha256:111aaa...",
			DisplayName: "HumanRef_GRCh38",
		},
		ConfigBlob: ConfigBlob{},
	}
	if err := manager.AddOrUpdate(v1); err != nil {
		t.Fatalf("AddOrUpdate v1: %v", err)
	}

	// 3. 같은 ref 를 내용 바꿔서 갱신
	v1Updated := VolumeEntry{
		Index: VolumeIndex{
			VolumeRef:   "sha256:111aaa...",
			DisplayName: "HumanRef_GRCh38 (patched)",
		},
		ConfigBlob: ConfigBlob{},
	}
	if err := manager.AddOrUpdate(v1Updated); err != nil {
		t.Fatalf("AddOrUpdate v1Updated: %v", err)
	}

	// 4. 다른 볼륨 추가
	v2 := VolumeEntry{
		Index: VolumeIndex{
			VolumeRef:   "sha256:222bbb...",
			DisplayName: "VCF Panel 2025-07",
		},
		ConfigBlob: ConfigBlob{},
	}
	if err := manager.AddOrUpdate(v2); err != nil {
		t.Fatalf("AddOrUpdate v2: %v", err)
	}

	// 5. 개별 조회
	gotEntry, ok := manager.Get("sha256:111aaa...")
	if !ok {
		t.Fatalf("expected to find entry for sha256:111aaa...")
	}
	fmt.Printf("Get sha256:111aaa... => Index=%+v, ConfigBlob=%+v\n", gotEntry.Index, gotEntry.ConfigBlob)

	// 6. 스냅샷 조회 (읽기용 복사본)
	snap := manager.GetSnapshot()
	fmt.Printf("Snapshot Version=%d, count=%d\n", snap.Version, len(snap.Volumes))

	// 7. 제거
	removed, err := manager.Remove("sha256:222bbb...")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if removed {
		fmt.Println("Removed sha256:222bbb...")
	} else {
		t.Fatalf("expected to remove sha256:222bbb...")
	}

	// 8. 필요 시 명시적 Flush (Add/Update/Remove 에서 이미 저장되지만 강제 저장 가능)
	if err := manager.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	fmt.Println("Done.")
}

func TestValidateVolumeDir_NonExistent(t *testing.T) {
	_, err := ValidateVolumeDir("non-existent-dir")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected error for non-existent directory, got %v", err)
	}
}

func TestValidateVolumeDir_NotDirectory(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(file, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := ValidateVolumeDir(file)
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("expected error for path that is not a directory, got %v", err)
	}
}

func TestValidateVolumeDir_EmptyDir(t *testing.T) {
	tmp := t.TempDir()

	_, err := ValidateVolumeDir(tmp)
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected error for empty directory, got %v", err)
	}
}

func TestValidateVolumeDir_CreateConfigBlob(t *testing.T) {
	tmp := t.TempDir()
	// non-hidden 파일 추가해서 empty 체크 통과
	dataFile := filepath.Join(tmp, "data.txt")
	if err := os.WriteFile(dataFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write data file: %v", err)
	}

	raw, err := ValidateVolumeDir(tmp)
	if err != nil {
		t.Fatalf("ValidateVolumeDir failed: %v", err)
	}
	expected := "{}"
	if strings.TrimSpace(string(raw)) != expected {
		t.Fatalf("expected raw %q, got %q", expected, raw)
	}

	// configblob.json 파일이 생성되었는지 확인
	cfgPath := filepath.Join(tmp, ConfigBlobFileName)
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read configblob.json: %v", err)
	}
	if strings.TrimSpace(string(b)) != expected {
		t.Fatalf("expected configblob.json to contain %q, got %q", expected, b)
	}
}

func TestValidateVolumeDir_LoadConfigBlob(t *testing.T) {
	tmp := t.TempDir()
	// non-hidden 파일 추가
	if err := os.WriteFile(filepath.Join(tmp, "data.txt"), []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write data file: %v", err)
	}
	// 유효한 JSON configblob.json 생성
	blob := `{"key":"value"}`
	cfgPath := filepath.Join(tmp, ConfigBlobFileName)
	if err := os.WriteFile(cfgPath, []byte(blob), 0644); err != nil {
		t.Fatalf("failed to write configblob.json: %v", err)
	}

	raw, err := ValidateVolumeDir(tmp)
	if err != nil {
		t.Fatalf("ValidateVolumeDir failed: %v", err)
	}
	if string(raw) != blob {
		t.Fatalf("expected raw %q, got %q", blob, raw)
	}
}

// TODO oci 폴더 권한 문제가 발생함. 이건 사전에 맞춰저야 한다.

func TestOciService01(t *testing.T) {
	conf, err := InitConfig("sori-oci.json")
	if err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}

	err = conf.EnsureDir()
	if err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	// 전체적인 볼륨의 정보를 담고 있는 volume-collection.json 을 만들어줌.
	cm, err := NewCollectionManager(conf.Local.Path)
	if err != nil {
		t.Fatalf("NewCollectionManager failed: %v", err)
	}

	// TODO
	// 볼륨하나 만들어줌. TODO 여기서 volumeindex 포인터를 만들어 주는데 이거 재활용할 것인지 생각해야 함. 이것과 연관해서 validation 메서드 만들어줘야 함.
	// podbridge5 볼륨 메서드에서 사용할때, 필요한 메서드 만들어줘야 함.
	// displayName 같은 경우 중복성 검사를 않하고 있음. 해줄 필요가 있는지 생각해야 함. 이건 옵션으로 두어서 중복으로 저장할 경우는 옵션을 true 로 주면 중복으로 저장되게 하자.
	// 볼륨 폴더가 빈 폴더면, 저장할 필요가 없음.
	// 지금은 볼더만을 잡아주고 있는데, 파일, 압축파일 등도 할지 생각해봐야 함.
	volDir := "./test-vol"

	configblob, err := ValidateVolumeDir(volDir)
	if err != nil {
		t.Fatalf("ValidateVolumeDir failed: %v", err)
	}

	vi, err := GenerateVolumeIndex(volDir, "테스트 하는 볼륨")
	if err != nil {
		t.Fatalf("GenerateVolumeIndex failed: %v", err)
	}

	var cb ConfigBlob
	if err := json.Unmarshal(configblob, &cb); err != nil {
		t.Fatalf("failed to parse configblob.json into ConfigBlob: %v", err)
	}

	newvi, err := vi.PublishVolume(context.Background(), volDir, "test.v1.0.0", configblob)
	if newvi == nil {
		t.Fatalf("PublishVolume returned nil VolumeIndex")
	}

	// 만들어준 볼륨을 cm 에 넣어줌. TODO pointer 로 쓸지 value 로 쓸지 결정하자. 지금 혼재되어 있음.
	entry := VolumeEntry{
		Index:      *newvi,
		ConfigBlob: cb,
	}
	if err := cm.AddOrUpdate(entry); err != nil {
		t.Fatalf("AddOrUpdate failed: %v", err)
	}
}
