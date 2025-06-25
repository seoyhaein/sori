package main

import (
	"fmt"
	pb "github.com/seoyhaein/api-protos/gen/go/volres/ichthys"
)

func main() {
	fmt.Println("Hello, Sori!")
	// TODO 아래는 에러 코드임.
	vl := pb.VolumeList{}
	fmt.Println(vl.GetVolumes())
}
