package sori

import (
	"context"
	"fmt"
	"path/filepath"
)

func ExampleClient_fetchVolume() {
	client := NewClient(WithLocalStorePath(filepath.Join("/tmp", "sori-oci")))
	_, _ = client.FetchVolume(context.Background(), "/tmp/restored", "/tmp/sori-oci", "example.v1", FetchOptions{
		Concurrency:             1,
		RequireEmptyDestination: true,
	})

	fmt.Println("fetch options supported")
	// Output:
	// fetch options supported
}
