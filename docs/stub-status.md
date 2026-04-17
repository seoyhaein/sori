# Stub Status

현재 저장소에 남아 있는 stub 파일과 처리 방향을 정리한 문서다.

## 대상 파일

- `local-registry.go`
- `pipeline-index.go`
- `oci-crud.go`

세 파일은 Sprint 20에서 저장소에서 제거했다.

## 현재 판단

### `local-registry.go`

상태:
- 제거 완료

판단:
- 현재 `sori`의 stable core API 범위에 직접 필요하지 않음
- registry 테스트/운영은 `registryutil`과 env-gated integration test가 우선

처리 결과:
- 제거
- 향후 필요가 생기면 `registryutil` 또는 별도 패키지에서 다시 설계

### `pipeline-index.go`

상태:
- 제거 완료

판단:
- 현재 라이브러리 범위는 reference dataset packaging 중심
- pipeline artifact index는 별도 도메인으로 보는 편이 맞음

처리 결과:
- 제거
- pipeline artifact index는 별도 프로젝트/패키지 요구가 생길 때 다시 시작

### `oci-crud.go`

상태:
- 제거 완료

판단:
- Sprint 6, 14에서 일부 역할 분리가 이미 진행됨
- 실제 대체 위치는 `volume_publish_fetch.go`, `registryutil`, `archiveutil`

처리 결과:
- 제거
- OCI helper 필요는 `volume_publish_fetch.go`, `archiveutil`, `registryutil` 또는 미래의 `ociutil` 패키지로 흡수

## 결론

세 파일 모두 “존재 이유가 약한 stub” 상태였고, Sprint 20에서 제거했다.

권장 순서:

1. 필요가 생기면 적절한 패키지로 새로 구현한다
2. stub 파일 이름을 되살리기보다 역할 중심 패키지로 설계한다
