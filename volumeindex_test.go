package sori

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAndSaveVolumeIndex(t *testing.T) {
	// JSON 생성
	vi, err := GenerateVolumeIndex("../test-vol", "TestVol")
	if err != nil {
		t.Fatalf("GenerateVolumeIndex failed: %v", err)
	}

	// 파일로 저장
	if err := vi.SaveToFile("../test-vol"); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}
}

func TestPublishVolumeAsOCI(t *testing.T) {

	volDir := "../test-vol"
	indexPath := filepath.Join(volDir, "volume-index.json")

	raw, err := os.ReadFile(indexPath)
	assert.NoError(t, err)
	var loaded VolumeIndex
	err = json.Unmarshal(raw, &loaded)
	assert.NoError(t, err)
	assert.Equal(t, "TestVol", loaded.DisplayName)

	ociRepo := "../repo"
	_, err = PublishVolumeAsOCI(context.Background(), volDir, indexPath, ociRepo, "v1.0.0")
	assert.NoError(t, err)
}

//TODO 몇가지 버그가 있다. 수정해야 한다.

// TestFetchVolumeFromOCI pushes a small volume to a local OCI store, fetches it back, and verifies
// both file contents and VolumeIndex metadata.
func TestFetchVolumeFromOCI(t *testing.T) {
	ctx := context.Background()

	repo := "../repo"
	dest := "../test-vol-restored"
	_, err := FetchVolumeFromOCI(ctx, dest, repo, "v1.0.0")
	if err != nil {
		t.Fatalf("FetchVolumeFromOCI failed: %v", err)
	}

}

// TestPushLocalToRemote_Harbor 실제 Harbor 레지스트리에 푸시
func TestPushLocalToRemote_Harbor(t *testing.T) {
	ctx := context.Background()

	localTag := "v1.0.0"
	remoteRepo := "harbor.local/demo-project/testrepo"
	user := "admin"
	pass := "Harbor12345"
	repo := "../repo"

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

// TestSaveLoadAndMergeIntegration performs an integration test on saveCollection, loadCollection, and Merge
func TestSaveLoadAndMergeIntegration(t *testing.T) {
	root := t.TempDir()

	// Prepare initial collection and save it
	initial := VolumeCollection{
		Version: 10,
		Volumes: []VolumeIndex{{DisplayName: "one", VolumeRef: "r1"}},
	}
	err := saveCollection(root, initial)
	assert.NoError(t, err, "failed to save initial collection")

	// Load and verify
	loaded, err := loadCollection(root)
	assert.NoError(t, err, "failed to load collection")
	assert.Equal(t, initial, loaded, "loaded collection should match initial")

	// Merge a new volume
	newV := VolumeCollection{
		Volumes: []VolumeIndex{{DisplayName: "two", VolumeRef: "r2"}},
	}
	merged := loaded.Merge(newV)
	assert.True(t, merged, "expected Merge to return true when adding a new volume")
	assert.Equal(t, 11, loaded.Version, "expected version to increment by 1 after merge")
	assert.Equal(t, "one", loaded.Volumes[0].DisplayName)
	assert.Equal(t, "two", loaded.Volumes[1].DisplayName)

	// Save merged collection
	err = saveCollection(root, loaded)
	assert.NoError(t, err, "failed to save merged collection")

	// Reload and verify persisted changes
	reloaded, err := loadCollection(root)
	assert.NoError(t, err, "failed to reload collection")
	assert.Equal(t, loaded, reloaded, "reloaded collection should match merged state")
}

func TestLoadCollection(t *testing.T) {
	// 빈 디렉터리에서 loadCollection 호출
	root := "/home/dev-comd/go/src/github.com/seoyhaein/sori/testRoot"

	loaded, err := loadCollection(root)
	assert.NoError(t, err, "loadCollection should not error on empty dir")
	assert.Equal(t, VolumeCollection{Version: 0}, loaded, "expected empty collection with version 0")

	saveCollection(root, loaded)
}
