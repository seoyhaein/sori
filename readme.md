# sori

OCI 기반 참조 데이터(볼륨) 패키징 라이브러리.  
디렉터리를 OCI 아티팩트로 변환하고, 로컬 OCI 스토어와 원격 레지스트리(Harbor 등) 사이의 push/fetch를 담당한다.

## 개요

`sori`는 바이오인포매틱스 파이프라인에서 사용하는 참조 데이터(genome, annotation 등)를  
OCI 이미지 형식으로 패키징하는 Go 라이브러리다.

주요 기능:
- 디렉터리를 OCI 레이어(tar.gz)로 변환 — 결정론적(deterministic) 해시 보장
- 로컬 OCI 스토어(`oras-go` `oci.Store`)에 푸시
- 원격 레지스트리(Harbor, registry:2 등)로 복사
- 볼륨 컬렉션 인덱스(`volume-collection.json`) 관리
- 순차/병렬 fetch 지원

## 의존성

| 패키지 | 버전 |
|--------|------|
| `oras.land/oras-go/v2` | v2.6.0 |
| `github.com/opencontainers/image-spec` | v1.1.1 |
| `github.com/opencontainers/go-digest` | v1.0.0 |
| `github.com/sirupsen/logrus` | v1.9.3 |

## 빠른 시작

```go
import "github.com/seoyhaein/sori"

ctx := context.Background()

// 1) 설정 로드
cfg, err := sori.LoadConfig("sori-oci.json")
if err != nil { ... }
if err := cfg.EnsureDir(); err != nil { ... }

// 2) 컬렉션 매니저 초기화
cm, err := sori.NewCollectionManager(cfg.Local.Path)
if err != nil { ... }

// 3) 볼륨 디렉터리를 OCI로 패키징 + 컬렉션 등록
if err := cm.PublishVolumeFromDir(ctx, "./my-genome-dir", "HumanRef GRCh38", "grch38.v1.0.0"); err != nil { ... }

// 4) 원격 레지스트리로 푸시
if err := sori.PushLocalToRemote(ctx, cfg.Local.Path, "grch38.v1.0.0", "harbor.local/project/repo", "user", "pass", true); err != nil { ... }
```

## 설정 파일 (`sori-oci.json`)

```json
{
  "local": {
    "type": "oci",
    "path": "/path/to/writable/oci-store"
  },
  "remotes": [
    {
      "name": "harbor",
      "type": "registry",
      "registry": "harbor.local",
      "repository": "project/repo",
      "tls": { "insecure": false, "ca_file": "" },
      "auth": { "username": "admin", "password": "Harbor12345", "token": "" }
    }
  ]
}
```

> `/var/lib/sori/oci` (기본값)는 root 권한이 필요하다. 개발/테스트 시에는 `path`를 쓰기 가능한 경로로 설정할 것.

## 공개 API

### Config

```go
func LoadConfig(path string) (*Config, error)
func InitConfig(path string) (*Config, error)          // LoadConfig + ociStore 전역 초기화
func (conf *Config) EnsureDir() error                  // local.path 디렉터리 생성
```

### VolumeIndex / 생성

```go
func GenerateVolumeIndex(rootPath, displayName string) (*VolumeIndex, error)
func (vi *VolumeIndex) SaveToFile(rootPath string) error
func (vi *VolumeIndex) PublishVolume(ctx, volPath, volName string, configBlob []byte) (*VolumeIndex, error)
```

`PublishVolume`은 각 레이어 descriptor에 `"org.example.partitionPath"` 어노테이션을 설정한다.  
이 어노테이션이 없으면 `FetchVolSeq` / `FetchVolParallel` 시 오류가 발생하므로 직접 descriptor를 만들 때 반드시 포함해야 한다.

### CollectionManager

```go
func NewCollectionManager(rootDir string, initial ...VolumeEntry) (*CollectionManager, error)
func (m *CollectionManager) AddOrUpdate(v VolumeEntry) error
func (m *CollectionManager) Remove(ref string) (bool, error)
func (m *CollectionManager) Get(ref string) (VolumeEntry, bool)
func (m *CollectionManager) GetSnapshot() VolumeCollection
func (m *CollectionManager) Flush() error
func (m *CollectionManager) PublishVolumeFromDir(ctx, volDir, displayName, tag string) error
```

`NewCollectionManager`는 `rootDir`이 없으면 자동으로 생성한다.

### 검증 유틸리티

```go
func ValidateVolumeDir(volDir string) ([]byte, error)
// - 빈 디렉터리 → 에러
// - configblob.json 없으면 빈 JSON으로 생성
// - configblob.json 있으면 로드해 반환
```

### 원격 push / fetch

```go
func PushLocalToRemote(ctx, localRepoPath, tag, remoteRepo, user, pass string, plainHTTP bool) error
func FetchVolSeq(ctx, destRoot, repo, tag string) (*VolumeIndex, error)
func FetchVolParallel(ctx, destRoot, repo, tag string, concurrency int) (*VolumeIndex, error)
```

### tar.gz 유틸리티

```go
func TarGzDir(fsDir, prefixPath string) ([]byte, error)   // 결정론적 tar.gz 생성
func UntarGzDir(gzipStream io.Reader, dest string) error   // tar.gz 해제
```

## 테스트 실행

```bash
# 단위 테스트 (외부 인프라 불필요)
go test -v -run "TestGenerateAndSaveVolumeIndex|TestTarGzDirDeterministic|TestExtractTarGz|TestMerge|TestLoadOrNewCollection_New|TestManager|TestValidateVolumeDir|TestLoadConfig_TempDir|TestPublishFetchRoundTrip" ./...

# 전체 테스트 (TestPublishVolumeOther, TestOciService01 등은 로컬 OCI 스토어 필요)
go test -v ./...
```

root 권한이 없는 환경에서는 `TestLoadConfig` / `TestInitConfig`가 자동으로 skip된다.  
`TestLoadConfig_TempDir`으로 동일 기능을 검증할 수 있다.

## 알려진 제한 사항

- `FetchVolParallel`: 워커 오류 발생 시 채널 드레인 로직 개선 예정 (`TODO`).
- `local-registry.go`, `pipeline-index.go`, `oci-crud.go`: 미구현 stub. 향후 채워질 예정.
- Harbor webhook 연동은 미구현. 현재 수동 push/fetch로만 동작.

## 라이선스

MIT
