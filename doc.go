// Package sori provides OCI-based packaging, push/fetch, and metadata helpers
// for reference datasets.
//
// Preferred entrypoint:
//  1. Load configuration with LoadConfig
//  2. Build a Client with Config.NewClient or NewClient
//  3. Package with Client.PackageVolumeWithOptions
//  4. Push with Client.PushPackagedVolumeWithOptions
//  5. Build generic metadata with BuildArtifactMetadata
//
// Core APIs are intentionally separated from NodeVault-oriented adapter APIs.
// For API stability levels, see docs/public-api.md.
package sori
