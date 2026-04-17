# Sori Post-v1 Sprint Plan

이 문서는 Sprint 5~10 이후에도 남아 있는 개선 항목을 정리한 후속 실행 문서다.

현재 `sori`는 다음 상태까지 왔다.

- `Client` 기반 stable core API
- generic metadata (`ArtifactMetadata`)
- typed error / option 모델 1차 도입
- registry-agnostic remote config 1차 도입
- public API stability 문서화

하지만 아래 항목은 아직 남아 있다.

- NodeVault adapter가 여전히 루트 패키지에 남아 있음
- 실제 multi-registry 통합 검증 부족
- typed error / option 모델의 적용 범위가 아직 제한적
- `volume-index.go` 책임이 여전히 큼
- examples / godoc / 운영성 문서 보강 필요
- 오래된 stub 파일 정리 필요

## Sprint 11: NodeVault Adapter 분리

목표:
NodeVault 친화 타입과 로직을 core `sori` API에서 한 단계 더 분리한다.

범위:
- `DataSpec`
- referrer helper
- registration/catalog helper
- NodeVault 친화 변환 함수

권장 구조:
- `adapters/nodevault`

완료 조건:
- NodeVault 친화 타입이 새 adapter 패키지에서 생성/변환 가능
- 루트 `sori` 패키지는 compatibility wrapper만 유지
- generic metadata 중심 구조가 문서에 반영됨

진행 메모:
- `adapters/nodevault` 패키지 초안을 추가했다.
- 현재는 generic metadata를 입력으로 받아 `DataSpec`, `RegisteredDataDefinition`을 만드는 변환 계층부터 분리했다.
- 루트 패키지의 기존 NodeVault 친화 타입은 아직 compatibility 목적으로 남아 있다.

## Sprint 12: Registry 통합 검증

목표:
실제 registry 상호운용성을 코드로 검증한다.

범위:
- Harbor 통합 검증 시나리오 정리
- `distribution` 또는 동등 OSS registry 통합 테스트
- `zot` 또는 경량 registry 통합 테스트
- referrers capability 자동/강제/fallback 동작 확인

완료 조건:
- 최소 2종 이상의 registry 통합 시나리오 존재
- referrer push/fetch 관련 차이를 문서화

## Sprint 13: Error / Option 확장

목표:
typed error와 option 모델을 core 전반으로 확대한다.

범위:
- 아직 남은 문자열 기반 에러 정리
- overwrite / collision / retry / fetch path policy 옵션 확장
- `errors.Is` / `errors.As` 기반 테스트 확대

완료 조건:
- 주요 public 경로에서 문자열 비교가 거의 사라짐
- option 모델이 실사용 정책을 충분히 표현함

진행 메모:
- `PackageOptions.RequireConfigBlob`와 `FetchOptions.RequireEmptyDestination`를 추가했다.
- config / fetch / publish 일부 경로에 typed error 적용 범위를 넓혔다.
- 아직 collection 관리와 일부 low-level helper에는 문자열 래핑이 남아 있다.

## Sprint 14: OCI / Core 분해

목표:
`volume-index.go`의 책임을 더 작은 단위로 나눈다.

범위:
- publish / fetch 관련 `ociutil` 분리
- collection / validation / index 생성을 별도 파일 또는 패키지로 이동
- 큰 함수 분해

완료 조건:
- 단일 거대 파일 의존이 줄어듦
- core 로직이 역할별로 추적 가능

진행 메모:
- `collection.go`, `volume_validation.go`, `volume_publish_fetch.go`로 주요 책임을 분리했다.
- `volume-index.go`는 타입과 상위 API 조합, 공용 helper 중심으로 정리했다.

## Sprint 15: Examples / Godoc / 운영 문서

목표:
라이브러리 사용성과 유지보수성을 높인다.

범위:
- example 추가
- package-level docs 정리
- registry/TLS/auth 운영 가이드
- migration notes 작성

완료 조건:
- 최소 package/push/fetch/register/referrer 예제가 존재
- 새 사용자가 README + docs만으로 진입 가능

진행 메모:
- `doc.go`에 package-level 문서를 추가했다.
- `example_fetch_test.go`, `example_register_test.go`로 fetch/register 예제를 보강했다.
- 운영 체크리스트를 `docs/operations.md`에 정리했다.

## Sprint 16: Stub 정리

목표:
방향이 불분명한 stub 파일을 정리한다.

범위:
- `local-registry.go`
- `pipeline-index.go`
- `oci-crud.go`

선택지:
- 구현
- deprecated 처리
- 제거

완료 조건:
- 각 파일의 존치 이유가 문서화되거나, 정리 완료

진행 메모:
- `docs/stub-status.md`를 추가해 `local-registry.go`, `pipeline-index.go`, `oci-crud.go`의 현재 판단과 처리 방향을 문서화했다.
- 새 기능은 해당 stub 파일에 더 추가하지 않는 기준을 명시했다.
- stub 파일 본문에도 `Deprecated` 주석을 넣어 코드 수준에서 같은 기준을 남겼다.
- 이후 Sprint 20에서 세 파일은 실제로 제거했다.

## 우선순위

1. Sprint 11
2. Sprint 12
3. Sprint 13
4. Sprint 14
5. Sprint 15
6. Sprint 16

## 즉시 시작 항목

지금 바로 시작할 작업은 `Sprint 11: NodeVault Adapter 분리`다.
