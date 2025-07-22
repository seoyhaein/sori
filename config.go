package sori

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type Config struct {
	Local   LocalStore    `json:"local"`
	Remotes []RemoteStore `json:"remotes"`
}

type LocalStore struct {
	Type string `json:"type"` // "oci"
	Path string `json:"path"`
}

type RemoteStore struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`       // "registry"
	Registry   string     `json:"registry"`   // e.g. harbor.local
	Repository string     `json:"repository"` // e.g. harbor 인 경우 project/repo
	Push       bool       `json:"push"`
	Pull       bool       `json:"pull"`
	TLS        TLSConfig  `json:"tls"`
	Auth       AuthConfig `json:"auth"`
}

type TLSConfig struct {
	Insecure bool   `json:"insecure"`
	CAFile   string `json:"ca_file"`
}

type AuthConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

// TODO 이렇게 하는 방식으로 표준을 정하자.
const defaultDirPerm fs.FileMode = 0o755

func InitConfig(path string) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}
	ociStore = cfg.Local.Path
	return nil
}

// LoadConfig reads and unmarshals the JSON file.
func LoadConfig(path string) (*Config, error) {
	// TODO 통일적으로 config 읽는 코드는 이런식으로 표준을 정하자.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	// (방어 코드) TODO 심볼릭 링크로 들어왔을대 읽지 않고 에러 리턴함. 방어적 코드. 이거 다른 코드에도 적용하자.
	fi, err := os.Lstat(abs) // 심볼릭 링크를 따라가지 않고 메타정보 조회
	if err != nil {
		return nil, fmt.Errorf("stat config: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("config is not a regular file: %s", abs)
	}

	f, err := os.Open(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) { // 또는 fs.ErrNotExist
			return nil, fmt.Errorf("config file not found: %s", abs)
		}
		return nil, fmt.Errorf("open config: %w", err)
	}

	// TODO defer close 이렇게 하는 거 표준으로 정하자. 다른곳에서는 다소 다르게 하고 있는데.
	defer func() {
		if cErr := f.Close(); cErr != nil {
			Log.Warnf("failed to close file %s: %v", abs, cErr)
		}
	}()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}

	// 유효성 검사 TODO 지금 이렇게 간단히 하지만, 별도의 메서드를 만들어서 configblob 도 확인해줘야 함.
	if cfg.Local.Path == "" {
		return nil, fmt.Errorf("local.path is empty")
	}

	if cfg.Local.Type != "oci" {
		return nil, fmt.Errorf("config error: local.type must be 'oci', but got '%s'", cfg.Local.Type)
	}
	for i, r := range cfg.Remotes {
		if r.Name == "" || r.Registry == "" || r.Repository == "" {
			return nil, fmt.Errorf("remotes[%d] missing required fields", i)
		}
	}

	return &cfg, nil
}

// EnsureDir sori-oci.json 에 있는 path 에 실제 디렉토리가 있는지
func (conf *Config) EnsureDir() error {
	// 방어적 코드
	if conf == nil {
		return errors.New("cannot ensure directory from a nil config")
	}
	if conf.Local.Path == "" {
		return errors.New("local.path is empty")
	}
	// 해당 디렉토리가 있으면 넘어감.
	p := filepath.Clean(conf.Local.Path)
	info, err := os.Stat(p)
	if err == nil {
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("path '%s' already exists but is not a directory", p)
	}
	// 해당 디렉토리가 없으면 만들어줌.
	if errors.Is(err, os.ErrNotExist) {
		if mkdirErr := os.MkdirAll(p, defaultDirPerm); mkdirErr != nil {
			return fmt.Errorf("failed to create directory '%s': %w", p, mkdirErr)
		}
		return nil
	}
	return fmt.Errorf("failed to check directory '%s': %w", p, err)
}
