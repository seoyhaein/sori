package sori

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	conf, err := LoadConfig("sori-oci.json")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	err = conf.EnsureDir()
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("skipping EnsureDir: no permission to create %q (run as root or set a writable path)", conf.Local.Path)
		}
		t.Fatalf("EnsureDir failed: %v", err)
	}
}

func TestInitConfig(t *testing.T) {
	conf, err := InitConfig("sori-oci.json")
	if err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}

	err = conf.EnsureDir()
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("skipping EnsureDir: no permission to create %q (run as root or set a writable path)", conf.Local.Path)
		}
		t.Fatalf("EnsureDir failed: %v", err)
	}
}

// TestLoadConfig_TempDir verifies LoadConfig+EnsureDir with a writable temp path.
func TestLoadConfig_TempDir(t *testing.T) {
	tmp := t.TempDir()
	localPath := filepath.Join(tmp, "oci")

	cfg := Config{
		Local:   LocalStore{Type: "oci", Path: localPath},
		Remotes: []RemoteStore{{Name: "test", Registry: "reg.example.com", Repository: "test/repo"}},
	}
	cfgPath := filepath.Join(tmp, "test-sori.json")
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	conf, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if err := conf.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}

	info, err := os.Stat(localPath)
	if err != nil {
		t.Fatalf("expected directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", localPath)
	}
}

func TestConfigNewClient_UsesLocalPath(t *testing.T) {
	tmp := t.TempDir()
	localPath := filepath.Join(tmp, "oci")
	cfg := &Config{
		Local: LocalStore{Type: "oci", Path: localPath},
	}

	client := cfg.NewClient()
	if got := client.LocalStorePath(); got != localPath {
		t.Fatalf("LocalStorePath mismatch: got %q want %q", got, localPath)
	}
}

func TestLoadConfig_NotFoundTypedError(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "missing.json"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TODO 여기서 테스트 몇가지 더 진행해야 한다.
// TODO configblob.json 에 대해서도 처리 해줘야 한다. 볼륨 만들어줘야 하는 폴더에 있어야 한다. 그래야 oci 에 저장할 수 있음.
// TODO 파일 읽기 다양하게 하는데 표준정해 놓고, 가장 좋은 것을 선택하자. 일단 여기서 부터 시작하자.
