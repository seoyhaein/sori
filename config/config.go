package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type VolumeRootsConfig struct {
	VolumeRoots []VolumeRoot `json:"volumeRoots"`
}

type VolumeRoot struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`              // "local" 또는 "remote"
	Address string   `json:"address,omitempty"` // remote 인 경우에만 설정
	Paths   []string `json:"paths"`             // 관리하는 경로 목록
}

// LoadVolumeRoots TODO 일단 통일된 기준을 만들어 놓자. 매번 메서드 만드는 방식 말고 한번 생각해보자.
func LoadVolumeRoots(path string) (*VolumeRootsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg VolumeRootsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}
