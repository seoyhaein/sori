package sori

import "testing"

func TestLoadConfig(t *testing.T) {
	_, err := LoadConfig("sori-oci.json")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
}

// TODO 여기서 테스트 몇가지 더 진행해야 한다.
// TODO configblob.json 에 대해서도 처리 해줘야 한다. 볼륨 만들어줘야 하는 폴더에 있어야 한다. 그래야 oci 에 저장할 수 있음.
// TODO 파일 읽기 다양하게 하는데 표준정해 놓고, 가장 좋은 것을 선택하자. 일단 여기서 부터 시작하자.
