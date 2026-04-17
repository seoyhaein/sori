package sori

import "fmt"

func ExampleBuildRegisteredDataDefinition() {
	pkg := &PackageResult{
		LocalTag:       "dataset.v1",
		ManifestDigest: "sha256:local",
		ConfigDigest:   "sha256:config",
		StableRef:      "dataset@v1",
	}

	def, _ := BuildRegisteredDataDefinition(DataRegisterRequest{
		DataName:    "dataset",
		Version:     "v1",
		Description: "example dataset",
		Format:      "FASTA",
	}, pkg, nil)

	fmt.Println(def.DataName)
	fmt.Println(def.StableRef)
	// Output:
	// dataset
	// dataset@v1
}
