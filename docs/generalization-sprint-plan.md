# Sori Generalization Sprint Plan

`sori`를 `NodeVault` 연계용 실용 라이브러리에서 한 단계 더 올려, 다른 프로젝트도 자연스럽게 가져다 쓸 수 있는 범용 OCI data packaging 라이브러리로 정리하기 위한 실행 문서다.

이 문서는 코드 작업 전에 확인하는 기준 문서다.  
범위를 넓히지 말고, 각 스프린트의 완료 조건을 만족하는 방식으로만 진행한다.

## 현재 판단

현재 `sori`는 아래 경로는 이미 갖추고 있다.

- 디렉터리 검증
- deterministic tar.gz layer 생성
- local OCI store publish
- remote registry push
- dataspec referrer push
- registered data definition 생성
- local JSON catalog 저장/조회

하지만 범용 라이브러리 관점에서는 아래가 아직 부족하다.

- 전역 상태(`ociStore`) 잔존
- low-level / high-level API 혼재
- NodeVault 전용 metadata와 범용 metadata 경계 불명확
- typed error 부재
- 옵션/주입 구조 부족
- registry/TLS/auth 확장성 부족
- public API 안정 경계 미정

## 원칙

- 범용 계층과 제품 전용 계층을 분리한다.
- 전역 상태를 제거하고 명시적 주입으로 바꾼다.
- public contract는 작고 안정적으로 유지한다.
- low-level 조합 가능성을 해치지 않는다.
- NodeVault 친화 API는 유지하되 core API 위의 adapter로 내린다.

## Sprint 5: Core API 정리

목표:
전역 상태를 제거하고, low-level 작업을 명확한 서비스 객체 기반 API로 재구성한다.

범위:
- `ociStore` 전역 제거
- `Client` 또는 `Service` 객체 도입
- `PackageVolume`, `PushPackagedVolume`, `FetchVolSeq`, `FetchVolParallel`이 명시적 설정 객체를 사용하도록 정리
- logger, clock, local store path, retry client를 옵션으로 주입 가능하게 정리
- 기존 전역 기반 API는 deprecated 경로로 유지

예상 영향 파일:
- [volume-index.go](/opt/go/src/github.com/HeaInSeo/sori/volume-index.go)
- [config.go](/opt/go/src/github.com/HeaInSeo/sori/config.go)
- 신규 `client.go` 또는 동등 파일

완료 조건:
- 새 코드 경로에서 전역 변수 없이 패키징/푸시/페치 가능
- 기존 테스트가 새 객체 기반 API 기준으로도 통과
- deprecated API가 새 구현을 감싸는 thin wrapper가 됨

비목표:
- metadata 스키마 개편
- registry provider 다변화

## Sprint 6: 패키지 경계 분리

목표:
재사용 단위를 분리해서 라이브러리 구조를 읽기 쉽게 만들고, 도메인 결합을 낮춘다.

범위:
- archive/tar 유틸 분리
- OCI publish/fetch 로직 분리
- registry push/referrer 로직 분리
- catalog/data registration 계층 분리
- NodeVault 전용 타입과 core 타입을 물리적으로 구분

권장 구조:
- `archive`
- `ociutil`
- `registryutil`
- `catalog`
- `nodevault` 또는 `adapters/nodevault`

완료 조건:
- 호출자가 core 계층만 import해서 package/push/fetch 가능
- NodeVault 특화 타입이 core 계층 public API에 직접 섞이지 않음
- README와 pkg-level 문서가 계층별 역할을 설명함

진행 메모:
- 1차 분리로 `archiveutil`, `registryutil`, `catalogutil` 하위 패키지를 추가했다.
- 현재는 `sori` 공개 API가 이 하위 패키지를 감싸는 구조다.
- 다음 단계에서는 NodeVault adapter 타입을 별도 경계로 더 분리해야 한다.

비목표:
- 기능 추가
- 대규모 알고리즘 변경

## Sprint 7: Error / Option 모델 정리

목표:
호출자가 실패 원인을 타입으로 구분하고, 동작 정책을 옵션으로 조정할 수 있게 한다.

범위:
- typed error 도입
- validation/auth/notfound/conflict/integrity/transport 구분
- `PackageOptions`, `PushOptions`, `FetchOptions`, `ReferrerOptions` 도입
- retry policy, concurrency, path policy, overwrite policy를 옵션화
- 문자열 비교 기반 테스트를 에러 타입 기반 검증으로 전환

완료 조건:
- 주요 public 함수가 sentinel error 또는 typed error를 반환
- 호출자가 `errors.Is` / `errors.As`로 분기 가능
- README에 에러 처리 예제가 포함됨

진행 메모:
- `ErrValidation`, `ErrNotFound`, `ErrConflict`, `ErrIntegrity`, `ErrTransport`, `ErrAuth`를 도입했다.
- `Client`에 `PackageOptions`, `PushOptions`, `FetchOptions`, `ReferrerOptions` 진입점을 추가했다.
- 1차 적용은 입력 검증, fetch, catalog 조회 같은 public 경로 중심이다.

비목표:
- gRPC 서비스 구현
- catalog API 외부 노출

## Sprint 8: Metadata 중립화

목표:
범용 metadata 모델과 NodeVault adapter 모델의 경계를 명확히 한다.

범위:
- generic artifact metadata 모델 정의
- `DataSpec`, `RegisteredDataDefinition`을 adapter 계층으로 이동 또는 wrapping
- `VolumeIndex`와 generic metadata의 역할 재정의
- schema version 필드 도입
- 직렬화 안정성 테스트 추가

완료 조건:
- core 계층이 NodeVault 용어 없이도 사용 가능
- NodeVault는 adapter를 통해 기존 계약 유지 가능
- schema versioning 정책이 문서화됨

진행 메모:
- `ArtifactMetadata`와 `ArtifactMetadataInput`을 추가했다.
- `DataSpec`, `RegisteredDataDefinition`은 이제 generic metadata를 변환하는 adapter 역할을 가진다.
- schema version은 `sori.artifact.v1`로 두었다.

비목표:
- 기존 NodeVault 통합 제거
- 외부 proto 자동 생성

## Sprint 9: Registry 확장성

목표:
Harbor 중심 구현에서 registry-agnostic 클라이언트로 한 단계 확장한다.

범위:
- auth provider 주입
- custom HTTP transport 주입
- TLS/CA/insecure policy 구조화
- Harbor 외 registry 호환성 테스트 추가
- remote capability 체크와 referrers fallback 전략 문서화

검증 대상 예시:
- Harbor
- distribution registry
- zot

완료 조건:
- registry 설정이 `RemoteTarget` 이상의 구조로 정리됨
- remote client 구성이 테스트 가능한 독립 계층이 됨
- 최소 2종 이상의 registry 시나리오 테스트가 존재

진행 메모:
- `RemoteTarget`/`registryutil.RemoteConfig`에 `InsecureTLS`, `Transport`, `AuthProvider`, `ReferrersCapability`를 추가했다.
- `ReferrersCapability=nil`이면 oras-go 기본 자동 감지를 그대로 사용한다.
- 실제 다중 registry 호환성 통합 테스트는 아직 없고, 현재는 registry client 구성 단위 테스트 수준이다.

비목표:
- 운영 환경 자동 배포
- webhook controller 구현

## Sprint 10: Public API 안정화

목표:
v1 라이브러리로 배포 가능한 수준의 안정 public contract를 확정한다.

범위:
- public API 목록 확정
- deprecated API 정리 계획 수립
- examples 추가
- compatibility promise 문서화
- package-level docs/godoc 정리

완료 조건:
- “안정 API”와 “실험 API”가 문서에 분리 표기됨
- 예제 코드가 최소 package/push/fetch/register 시나리오를 포함
- 다음 breaking change가 필요한 지점이 명확히 표시됨

진행 메모:
- `docs/public-api.md`를 추가해 Stable / Compatibility / Experimental API를 구분했다.
- `example_client_test.go`를 추가해 client + generic metadata 사용 예제를 고정했다.
- 다음 major에서 정리할 deprecated 후보를 문서에 표시했다.

## 작업 우선순위

반드시 아래 순서로 진행한다.

1. Sprint 5
2. Sprint 6
3. Sprint 7
4. Sprint 8
5. Sprint 9
6. Sprint 10

이 순서를 바꾸면 안 되는 이유:

- 전역 상태와 계층 경계가 먼저 정리되지 않으면 이후 refactor 비용이 커진다.
- typed error와 option 모델은 구조 분리 후 넣어야 깔끔하다.
- metadata 중립화는 core/adaptor 경계가 먼저 있어야 안전하다.
- registry 확장성은 API 안정화 직전에 다루는 편이 낫다.

## 각 스프린트 공통 체크리스트

- 새로운 public 타입이 정말 필요한지 검토했는가
- NodeVault 전용 요구사항이 core 계층으로 새고 있지 않은가
- 전역 상태를 추가하지 않았는가
- 테스트가 temp dir 기준으로 독립 실행 가능한가
- README와 문서가 새 경계를 설명하는가
- 기존 사용자를 깨는 change라면 deprecated 경로가 있는가

## 금지 사항

- `NodeKit` 또는 `NodeVault`의 미확정 요구를 core API에 직접 박아 넣지 말 것
- 새로운 전역 변수를 만들지 말 것
- low-level helper를 바로 high-level workflow에 묶어버리지 말 것
- Harbor 전용 동작을 범용 기본값처럼 노출하지 말 것

## 권장 작업 방식

각 스프린트는 아래 순서로 진행한다.

1. 설계 메모를 먼저 짧게 남긴다.
2. public API 변경을 최소 diff로 도입한다.
3. adapter 계층은 core 계층 위에서 얇게 유지한다.
4. 회귀 테스트와 새 테스트를 같이 추가한다.
5. README와 이 문서를 같이 갱신한다.

## 완료 후 기대 상태

이 계획이 끝나면 `sori`는 아래 두 모드를 모두 만족해야 한다.

- 범용 모드: 임의 프로젝트가 OCI data packaging/push/fetch 라이브러리로 사용
- 제품 모드: NodeVault가 adapter를 통해 dataspec/referrer/catalog 계약을 그대로 사용
