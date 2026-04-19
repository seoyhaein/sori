# Sori Operations Guide

운영 환경에서 `sori`를 사용할 때 먼저 확인할 항목을 정리한 문서다.

## Local OCI Store

- 기본 경로는 `/var/lib/sori/oci`
- 루트 권한이 필요할 수 있으므로 서비스 환경에서는 writable path를 명시하는 편이 안전하다
- 애플리케이션에서는 `LoadConfig` 후 `Config.NewClient()` 사용을 권장한다

## Registry / TLS / Auth

- HTTPS + 사설 CA: `RemoteTarget.CAFile`
- self-signed 테스트 환경: `RemoteTarget.InsecureTLS=true`
- HTTP 테스트 환경: `RemoteTarget.PlainHTTP=true`
- basic auth: `Username` / `Password`
- token auth: `Token`
- custom credential flow: `AuthProvider`
- custom transport tuning: `Transport` 또는 `HTTPClient`

## Referrers Capability

- `ReferrersCapability=nil`
  기본값. oras-go 자동 감지 사용
- `ReferrersCapability=true`
  Referrers API 강제
- `ReferrersCapability=false`
  Referrers Tag fallback 강제

운영 환경에서는 자동 감지부터 시작하고, registry별 편차가 있을 때만 명시 설정을 권장한다.

## Fetch Safety

- `FetchOptions.RequireEmptyDestination=true`를 사용하면 기존 파일 위에 덮어쓰는 복원을 막을 수 있다
- 복원 대상은 temp dir 아래에서 검증 후 이동하는 방식이 가장 안전하다

## Packaging Policy

- `PackageOptions.RequireConfigBlob=true`를 사용하면 metadata 없이 자동 생성되는 `configblob.json`을 금지할 수 있다
- 재현성과 추적성이 중요한 환경에서는 이 옵션을 켜는 편이 낫다

## 권장 실행 흐름

1. `LoadConfig`
2. `Config.NewClient`
3. `Client.PackageVolumeWithOptions`
4. `Client.PushPackagedVolumeWithOptions`
5. `BuildArtifactMetadata`
6. 필요 시 NodeVault adapter/referrer/catalog 경로 사용
