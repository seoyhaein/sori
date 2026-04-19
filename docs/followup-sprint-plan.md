# Sori Follow-up Sprint Plan

이 문서는 현재 `sori` 상태를 기준으로, 이미 진행한 성숙화 작업 이후에
남아 있는 후속 스프린트를 정리한 문서다.

전제는 다음과 같다.

- core candidate surface는 코드와 문서에서 어느 정도 분명해졌다
- experimental / compatibility 경계도 코드에 반영됐다
- 지금 남은 일은 대규모 구현보다 “승격 판단”, “보장 범위 확정”,
  “release hardening”에 가깝다

즉, 이후 스프린트는 기능을 크게 늘리는 단계라기보다
`어디까지를 약속할 것인가`를 확정하는 단계다.

## Sprint 22: Stable Promotion Decision

목표:
어느 API를 실제 stable로 올리고, 어느 API를 계속 experimental로 둘지
결정한다.

범위:
- `Client` 기반 core path 재확인
- `BuildArtifactMetadata`까지를 stable core로 고정할지 결정
- `DataSpec` / registration / referrer 계층을 계속 experimental로 둘지 결정
- 필요 시 `adapters/nodevault` 경계 활용 방안 재검토

완료 조건:
- stable 승격 대상과 보류 대상을 명확히 결정
- root package의 장기 public surface가 더 선명해짐

비목표:
- NodeVault canonical model을 새로 설계하지 않음

## Sprint 23: Registry Support Contract

목표:
registry 지원 범위를 어디까지 약속할지 정한다.

범위:
- Harbor 중심 보장을 유지할지 결정
- 추가 registry(`distribution`, `zot` 등) 검증이 필요한지 판단
- referrer 동작 보장 범위를 어디까지 문서화할지 결정
- registry integration test 운영 기준 정리

완료 조건:
- registry support 범위가 문서와 테스트 관점에서 설명 가능함
- “지원함”과 “검증해봤음”의 차이가 명확해짐

비목표:
- 대규모 registry harness 도입

## Sprint 24: Error / Option Contract Freeze

목표:
호출자가 의존해도 되는 에러/옵션 계약을 더 명확히 고정한다.

범위:
- root package `Err*` 사용 계약 재검토
- `PackageOptions`, `PushOptions`, `FetchOptions`, `ReferrerOptions` freeze 여부 판단
- 추가 정책 옵션이 필요한지 최종 확인
- `errors.Is` 기대 수준 정리

완료 조건:
- callers가 어떤 `Err*`와 option field를 계약처럼 봐도 되는지 더 분명함

비목표:
- 옵션 surface 대규모 재설계

## Sprint 25: Release Hardening

목표:
내부 공용 라이브러리로 쓰는 기준에서 마무리 점검을 한다.

범위:
- example / GoDoc / README 최종 교차 검토
- compatibility API 사용 흔적 최소화
- known limitation 재정리
- tag/release 기준 검토

완료 조건:
- 실사용 팀이 무엇을 써야 하는지 더 헷갈리지 않음
- release 후보 기준이 문서와 코드에서 일관됨

비목표:
- 새로운 대형 기능 추가

## 우선순위

1. Sprint 22
2. Sprint 23
3. Sprint 24
4. Sprint 25

## 현재 추천 시작점

지금 바로 다음으로 들어갈 작업은 `Sprint 22: Stable Promotion Decision`이다.
