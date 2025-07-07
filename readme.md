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

### 사용해야 할 것.
- groupcache
- 구글에서 만든 분산 메모리 캐시 라이브러리  
- 여러 대의 애플리케이션 인스턴스가 서로 “내가 가진 캐시를 꺼내 쓸래? 아니면 네가 가져가도 돼?” 식으로 조율  
- “peer” 노드 간에 자동으로 캐시를 share  

```aiignore
import "github.com/golang/groupcache"

// 전역 그룹 선언
var volumeGroup = groupcache.NewGroup(
  "volumes",
  64<<20, // 캐시 최대 64MB
  groupcache.GetterFunc(func(ctx groupcache.Context, key string, dest groupcache.Sink) error {
    // cache miss 시 호출: 키(key)에 해당하는 VolumeManifest를 DB/파일에서 로드
    manifest, err := loadManifestFromStore(key)
    if err != nil {
      return err
    }
    data, _ := proto.Marshal(manifest)
    dest.SetBytes(data)
    return nil
  }))

// 핸들러 내부에서 사용
var data []byte
err := volumeGroup.Get(ctx, volumeRef, groupcache.AllocatingByteSliceSink(&data))
if err != nil {
  return nil, err
}
var m volresv1.VolumeManifest
proto.Unmarshal(data, &m)

```

- 이미지 처리 부분과 맞다아 있음. 같이 고려해야함.  
- https://github.com/seoyhaein/dockhouse 참고.  
- tori 와 파일 포멧에 대해서 생각해줘야 함.  

### oras-go
- https://github.com/oras-project/oras-go/blob/main/docs/tutorial/quickstart.md

### 생ㄱ각할것들 (중요, 일단 생각나는데로 해서 정리 안됨)
~~- 레어버 구분해주는 것을 tui 로 구현하는 것으로 생각했음. 이 내용을 바탕으로 json 만들어주는 방향으로.~~   
~~- 하위 디렉토리 내용을 선택해서 레이어를 해주는 것을 tui 로 하면 좀더 직관적이고 오류 가능성을 줄여 줄 수 있음.~~  
~~- 하지만, 조금더 생각을 깊게 해야함.~~    