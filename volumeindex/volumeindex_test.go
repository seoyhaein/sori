package volumeindex

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
