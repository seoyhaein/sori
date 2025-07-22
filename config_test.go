package sori

import "testing"

func TestLoadConfig(t *testing.T) {
	_, err := LoadConfig("sori-oci.json")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
}
