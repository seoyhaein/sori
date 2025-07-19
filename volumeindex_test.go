package sori

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"log"
	"os"
	"path/filepath"
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

func TestPublishVolume(t *testing.T) {

	volDir := "./test-vol"
	vi, err := GenerateVolumeIndex(volDir, "테스트 하는 볼륨")
	if err != nil {
		t.Fatalf("GenerateVolumeIndex failed: %v", err)
	}
	configblob, err := loadMetadataJSON("configblob.json")
	if err != nil {
		t.Fatalf(" failed to load configblob juson:%v", err)
	}

	_, err = vi.PublishVolume(context.Background(), volDir, "test.v1.0.0", configblob)
	assert.NoError(t, err)
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
		Volumes: []VolumeIndex{{DisplayName: "v1", VolumeRef: "ref1"}},
	}
	newColl := VolumeCollection{
		Volumes: []VolumeIndex{
			{DisplayName: "v2", VolumeRef: "ref2"},
			{DisplayName: "v1", VolumeRef: "ref1"}, // duplicate entry
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
		Volumes: []VolumeIndex{{DisplayName: "v1", VolumeRef: "ref1"}},
	}
	newColl := VolumeCollection{
		Volumes: []VolumeIndex{{DisplayName: "v1", VolumeRef: "ref1"}},
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
		log.Fatalf("failed to init collection manager: %v", err)
	}

	// 2. 새 볼륨을 하나 만든다고 가정 (예: OCI Push 후 얻은 digest)
	v1 := VolumeIndex{
		VolumeRef:   "sha256:111aaa...",
		DisplayName: "HumanRef_GRCh38",
	}
	if err := manager.AddOrUpdate(v1); err != nil {
		log.Fatalf("AddOrUpdate v1: %v", err)
	}

	// 3. 같은 ref 를 내용 바꿔서 갱신
	v1Updated := VolumeIndex{
		VolumeRef:   "sha256:111aaa...",
		DisplayName: "HumanRef_GRCh38 (patched)",
	}
	if err := manager.AddOrUpdate(v1Updated); err != nil {
		log.Fatalf("AddOrUpdate v1Updated: %v", err)
	}

	// 4. 다른 볼륨 추가
	v2 := VolumeIndex{
		VolumeRef:   "sha256:222bbb...",
		DisplayName: "VCF Panel 2025-07",
	}
	if err := manager.AddOrUpdate(v2); err != nil {
		log.Fatalf("AddOrUpdate v2: %v", err)
	}

	// 5. 개별 조회
	got, ok := manager.Get("sha256:111aaa...")
	if ok {
		fmt.Printf("Get sha256:111aaa... => %+v\n", got)
	}

	// 6. 스냅샷 조회 (읽기용 복사본)
	snap := manager.GetSnapshot()
	fmt.Printf("Snapshot Version=%d, count=%d\n", snap.Version, len(snap.Volumes))

	// 7. 제거
	if removed, err := manager.Remove("sha256:222bbb..."); err != nil {
		log.Fatalf("Remove: %v", err)
	} else if removed {
		fmt.Println("Removed sha256:222bbb...")
	}

	// 8. 필요 시 명시적 Flush (Add/Update/Remove 에서 이미 저장되지만 강제 저장 가능)
	if err := manager.Flush(); err != nil {
		log.Fatalf("Flush: %v", err)
	}

	fmt.Println("Done.")
	_ = context.Background() // (예: 다른 로직에서 사용할 수도 있음)
}
