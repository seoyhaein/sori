package sori

import (
	"context"
	"fmt"
)

func ExampleClient_packagePushMetadata() {
	client := NewClient(WithLocalStorePath("/tmp/sori-oci"))

	pkg, _ := client.PackageVolumeWithOptions(context.Background(), PackageRequest{
		SourceDir:   "./test-vol",
		DisplayName: "HumanRef GRCh38",
		Tag:         "grch38.v1.0.0",
		Dataset:     "grch38-reference",
		Version:     "v1.0.0",
	}, PackageOptions{})

	metadata, _ := BuildArtifactMetadata(ArtifactMetadataInput{
		Kind:        "dataset",
		Name:        "grch38-reference",
		Version:     "v1.0.0",
		DisplayName: "HumanRef GRCh38",
		Description: "Human reference genome",
		SourceDir:   "./test-vol",
	}, pkg, nil)

	fmt.Println(pkg.LocalTag)
	fmt.Println(metadata.Identity.StableRef)
	// Output:
	// grch38.v1.0.0
	// grch38-reference@v1.0.0
}
