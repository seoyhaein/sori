### TODO
- proto 파일과 관련 서비스 파일은 api-proto 로 옮길 예정. 일단 여기서 작업후 옮길 것임.  
- 터미널에서 어떻게 ui 를 잡아서 volume 정보들을 얻어 올까?  
- api-proto 에 트릭이 몇가지 들어갔는데 이거 정리하자. 않하니까 잊어버린다. go work 도 정리 해야함.

### 정리 안됨.
- 1 tarball 을 만들면서 SHA-256 해시(digest)를 얻고,  
- 2 사용하면 OCI 레이어의 공식 digest (sha256:...)를 대신 쓸 수도 있고,  
- 3 그 digest를 volManifest.LayerDigest 에 할당한 뒤,  
- 4 파일이나 DB에 영속화(persist)하면 됩니다.  

- 1 VolumeManifest 에 layer_digest 필드를 추가  
- 2 tarball 생성 시점에 SHA-256 해시를 계산해 기록  
- 3 OCI push 후 descriptor.Digest 로 같은 값을 기록  
- 4 클라이언트가 요청 시점에 tarball 또는 레이어 digest 를 재계산/확인 → 메타에 저장된 값과 비교  