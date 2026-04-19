# Sori Maturity Sprint Plan

이 문서는 현재 `sori`의 수준을 기준으로, 앞으로 어느 부분을 더 진행해야
“실사용 가능한 코어 라이브러리”에서 “더 안정된 범용 라이브러리”로 올라갈 수
있는지 정리한 실행 문서다.

현재 판단은 아래와 같다.

- 코어 packaging / push / fetch 흐름은 실사용 가능
- `Client` 기반 API와 generic metadata는 충분히 정리됨
- 문서와 예제, 운영 가이드도 기본 진입 수준은 충족
- 하지만 일부 경계는 아직 experimental 이거나 미완

즉, `sori`는 지금 “충분히 쓸 수 있는 중간 단계”이며, 아래 스프린트는
이 상태를 더 안정적인 라이브러리 수준으로 끌어올리기 위한 후속 작업이다.

## 현재 충분한 영역

- `Client` 기반 package / push / fetch API
- deterministic tar.gz packaging
- local OCI store publish
- remote registry push + digest 반환
- generic metadata (`ArtifactMetadata`)
- 기본 운영 문서와 예제

## 아직 미완인 영역

- NodeVault 친화 타입과 루트 패키지 경계가 완전히 분리되지 않음
- typed error가 low-level helper 전체에 일관되게 닿아 있지 않음
- registry 실증 테스트가 외부 환경 의존 골격 수준에 머무름
- 일부 experimental API는 안정 계약으로 보기 어려움
- release readiness 관점의 known limitation 정리가 아직 더 필요함

## Sprint 17: Low-level Error 정리 마무리

목표:
low-level helper와 utility 경로의 에러를 typed error 체계에 더 일관되게 맞춘다.

범위:
- `referrer.go`
- `archiveutil`
- `registryutil`
- `catalogutil`
- root package 내부의 남은 문자열 래핑 경로

완료 조건:
- 주요 low-level 경로가 `ErrValidation`, `ErrNotFound`, `ErrTransport`,
  `ErrIntegrity`, `ErrAuth` 중 하나로 분류 가능
- 문자열 비교에 의존하는 테스트/호출 경로가 더 줄어듦

비목표:
- 모든 내부 에러를 무조건 하나의 kind로 강제하지는 않음

## Sprint 18: Registry 실증 테스트 강화

목표:
실제 registry 환경에서 어떤 단계가 깨지는지 더 빠르게 구분할 수 있게 한다.

범위:
- push-only / push+referrer 시나리오 유지
- 필요 시 fetch 또는 resolve 검증 추가
- Harbor 기준 운영 메모 보강
- 가능하면 `distribution` 또는 `zot` 대상 실행 가이드 추가

완료 조건:
- registry 문제를 `push`, `resolve`, `referrer` 단계로 나눠 진단 가능
- `docs/registry-integration.md`가 실제 실행 기준 문서로 충분함

비목표:
- 테스트용 registry를 저장소 내부에서 자동으로 띄우는 harness까지는 포함하지 않음

## Sprint 19: Experimental API 경계 재정리

목표:
지금 바로 안정 API로 봐도 되는 것과 아직 experimental 로 남길 것을 더 명확히 한다.

범위:
- `docs/public-api.md` 재검토
- root package의 compatibility / experimental 항목 재분류
- README의 권장 진입 경로 정리

완료 조건:
- 새 사용자가 어떤 API를 바로 써도 되는지 더 분명함
- 실험적 경계가 문서와 코드에서 일관되게 드러남

비목표:
- NodeVault 경계를 강제로 지금 분리하지는 않음

## Sprint 20: Stub 제거 여부 결정

목표:
현재 deprecated 상태의 stub 파일을 실제로 제거할지, 보관할지 결정한다.

범위:
- `local-registry.go`
- `pipeline-index.go`
- `oci-crud.go`

선택지:
- 제거
- 별도 패키지/저장소로 이동
- 유지하되 명확한 이유를 추가

완료 조건:
- 세 파일 각각에 대해 “왜 남기는지 / 왜 지우는지”가 최종 결정됨

비목표:
- 별도 도메인 기능 자체를 여기서 새로 구현하지는 않음

진행 메모:
- `local-registry.go`, `pipeline-index.go`, `oci-crud.go`는 코드 참조가 없어서 제거했다.
- 관련 판단과 후속 원칙은 `docs/stub-status.md`에 반영했다.

## Sprint 21: Release Readiness 점검

목표:
현재 상태를 내부 공용 라이브러리로 선언해도 되는지 최종 점검한다.

범위:
- 문서 교차 검토
- example 실행 경로 재확인
- 기본 운영 체크리스트 재검토
- known limitations 문서화

완료 조건:
- README, public API 문서, 운영 문서, sprint 문서 간 모순이 없음
- “지금 바로 써도 되는 영역”과 “주의할 영역”이 명확함

비목표:
- 외부 공개용 마케팅 문서 작성

진행 메모:
- README의 빠른 시작 흐름을 stable core / experimental 계층으로 분리해 다시 정리했다.
- `docs/public-api.md`, `docs/operations.md`, `readme.md` 사이의 권장 진입 경로를 맞췄다.
- 현재 known limitation을 README에 명시했다.

## 우선순위

1. Sprint 17
2. Sprint 18
3. Sprint 19
4. Sprint 20
5. Sprint 21

## 실행 원칙

1. NodeVault 경계는 현재 건드리지 않는다.
2. 코어 API 안정성을 해치지 않는 방향으로만 정리한다.
3. 새 기능 추가보다 에러 모델, 테스트, 문서, 경계 정리에 우선순위를 둔다.

## 현재 상태

Sprint 17~21 기준 문서화와 정리 작업은 완료했다.
