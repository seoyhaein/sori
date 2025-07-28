### TODO
- proto 파일과 관련 서비스 파일은 api-proto 로 옮길 예정. 일단 여기서 작업후 옮길 것임.  
~~- 터미널에서 어떻게 ui 를 잡아서 volume 정보들을 얻어 올까?~~  
- api-proto 에 트릭이 몇가지 들어갔는데 이거 정리하자. 않하니까 잊어버린다. go work 도 정리 해야함.

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

### 생각할 것들

- 이미지 처리 부분과 맞다아 있음. 같이 고려해야함.  
- https://github.com/seoyhaein/dockhouse 참고.  
- tori 와 파일 포멧에 대해서 생각해줘야 함.  
- 이 프로젝트는 라이브러리다.  팩키지로 개발된거 지워야 함.

### todo
~~- 볼륨 만들어 주는 메서드와 통합하는 별도의 main.go 만들어서 진행하기 여기서 별도의 메서드 도출해야 함.~~  
- 생각하기에서 복원 및 백업, 버전 관리 등에 대한 메서드 개발 해야함.
- 테스트 철저하기 진행하기 어느정도 완료한다음에 codex 활용
- repo 이름 정하기 이거 config 파일에 넣어서 관리하는 것 생각하고 구현해야 함. 내부적으로 숨길 수 있도록 한다.

### oras-go
- https://github.com/oras-project/oras-go/blob/main/docs/tutorial/quickstart.md

### 생각할것들 (중요, 일단 생각나는데로 해서 정리 안됨)
~~- 레어버 구분해주는 것을 tui 로 구현하는 것으로 생각했음. 이 내용을 바탕으로 json 만들어주는 방향으로.~~   
~~- 하위 디렉토리 내용을 선택해서 레이어를 해주는 것을 tui 로 하면 좀더 직관적이고 오류 가능성을 줄여 줄 수 있음.~~  
~~- 하지만, 조금더 생각을 깊게 해야함.~~    

- vsc 같은 경우 docker 에서 이미지들을 가져와서 뿌려주는 방시을 취하고 있다. 마찬자기로, json 파일과 접목해서 가져오되 버전을 가져와서 비교해보고
- 만약 버전이 같으면 가져오지 않고 json 을 로드 하는 형식으로 하고, 다르면 다른 부분을 가져와서 업데이트 하는 방식을 취한다

- 1번 생각하기
-- TODO 정책을 정해야 하는데 일단, rootDir 안에 볼륨 폴더들이 있는 것이 원칙이다. 하지만 그렇게 하지 않다도 되게 일단 만들어 놓는다.
-- -> 이렇게 할경우 어떻게해 할지 생각해봐야 함., 복사해서 넣어줘야 할까??

- 폴더 나 압축 파일이 들어갈 수 있음. 이때 검증과정을 거쳐서 볼륨 작업의 안정성을 높이고, 사용자에게는 간단히 폴더 또는 파일 을 넣어두는 것으로 볼륨을 만들어 주도록 한다.
- 폴더, 압축 타볼, 파일 이 세가지 경우를 검증하고, no_deep_scan 검증, 파일, 폴더 수를 검증해서 제한을 걸어 놓는 것이 중요할 거 같다.
- 검증을 통해서, 해당 데이터(폴더, 압축타볼, 파일)등은 내부적으로 정해놓은 폴더에 저장해 넣는다. <- 중복 검증해야 함.

- 2번 생각 하기
- // TODO 그리고 볼륨 폴더에 는 volume-index.json 이 있어야 한다. 위치는 볼륨 폴더의 루트 위치에 있어야 한다. 그것들을 읽어서 VolumeCollection 을 만들어 주는 방식으로 간다.
- -> 만약 이렇게 안되어 있으면 에러 뱉어내야 함. 그리고 생성할때 저렇게 배치되도록 해줘야 함.


### 버전관리, 복원 및 백업 시나리오
- 볼륨과 실제 oci store 에 저장되어 있는 것은 같은 것이어야 한다. 이것을 검색하는 키는 결국 volume-collection.json 이고 이게 클라이언트로 갈때는 proto 파일로 전송된다.
~~- 만약, volume-collection.json 이게 없다면, 먼저 볼륨에 있는지 확인하고, 여기서 가져온다. 이 데이터를 통해서 volume-collection.json 을 만들어 준다.~~
~~- 만약, volume-collection.json 이 없고, 볼륨이 없다면, oci store 에서 가져와서 volume-collection.json 을 만들어 준다.~~
- volume-collection.json 을 통해서 볼륨을 만들어주거나, oci store 를 만들어준다. 
- 만약 volume-collection.json 만 있고, 데이터가 없다면, 이건 실패다.
- 가장 취약한 데이터는 볼륨과 volume-collection.json 이다. 
- 복원 할 수 있는 메서드들을 만들어 두어야 한다.
- race 테스트 해야함. 그리고 lock unlcok 에 대해서 다른 메서드들도 필요한지 생각해야 함.

