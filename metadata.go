package sori

import "strings"

const ArtifactMetadataSchemaVersion = "sori.artifact.v1"

type ArtifactMetadata struct {
	SchemaVersion string                 `json:"schema_version"`
	Kind          string                 `json:"kind"`
	Identity      ArtifactIdentity       `json:"identity"`
	Display       ArtifactDisplay        `json:"display"`
	Source        ArtifactSource         `json:"source"`
	Location      ArtifactLocation       `json:"location"`
	Contents      ArtifactContents       `json:"contents"`
	Annotations   map[string]string      `json:"annotations,omitempty"`
	Extras        map[string]interface{} `json:"extras,omitempty"`
}

type ArtifactIdentity struct {
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	StableRef string `json:"stable_ref"`
}

type ArtifactDisplay struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type ArtifactSource struct {
	SourceDir string `json:"source_dir,omitempty"`
	SourceURI string `json:"source_uri,omitempty"`
}

type ArtifactLocation struct {
	LocalTag       string `json:"local_tag,omitempty"`
	Repository     string `json:"repository,omitempty"`
	Reference      string `json:"reference,omitempty"`
	ManifestDigest string `json:"manifest_digest,omitempty"`
	ConfigDigest   string `json:"config_digest,omitempty"`
}

type ArtifactContents struct {
	Format     string      `json:"format,omitempty"`
	TotalSize  int64       `json:"total_size"`
	CreatedAt  string      `json:"created_at,omitempty"`
	Partitions []Partition `json:"partitions,omitempty"`
}

type ArtifactMetadataInput struct {
	Kind        string
	Name        string
	Version     string
	StableRef   string
	DisplayName string
	Description string
	Category    string
	Tags        []string
	Format      string
	SourceDir   string
	SourceURI   string
	Annotations map[string]string
	Extras      map[string]interface{}
}

func BuildArtifactMetadata(input ArtifactMetadataInput, pkg *PackageResult, push *PushResult) (*ArtifactMetadata, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, validationError("BuildArtifactMetadata", "name is required", nil)
	}
	if pkg == nil {
		return nil, validationError("BuildArtifactMetadata", "package result is required", nil)
	}

	stableRef := strings.TrimSpace(input.StableRef)
	if stableRef == "" {
		if strings.TrimSpace(input.Version) != "" {
			stableRef = input.Name + "@" + input.Version
		} else {
			stableRef = input.Name
		}
	}

	meta := &ArtifactMetadata{
		SchemaVersion: ArtifactMetadataSchemaVersion,
		Kind:          defaultString(input.Kind, "dataset"),
		Identity: ArtifactIdentity{
			Name:      input.Name,
			Version:   input.Version,
			StableRef: stableRef,
		},
		Display: ArtifactDisplay{
			Name:        defaultString(input.DisplayName, input.Name),
			Description: input.Description,
			Category:    input.Category,
			Tags:        cloneStringSlice(input.Tags),
		},
		Source: ArtifactSource{
			SourceDir: input.SourceDir,
			SourceURI: input.SourceURI,
		},
		Location: ArtifactLocation{
			LocalTag:       pkg.LocalTag,
			ManifestDigest: pkg.ManifestDigest,
			ConfigDigest:   pkg.ConfigDigest,
		},
		Contents: ArtifactContents{
			Format:     input.Format,
			TotalSize:  pkg.TotalSize,
			CreatedAt:  pkg.CreatedAt,
			Partitions: append([]Partition(nil), pkg.Partitions...),
		},
		Annotations: cloneAnnotations(input.Annotations),
		Extras:      cloneInterfaceMap(input.Extras),
	}
	if push != nil {
		meta.Location.Repository = push.Repository
		meta.Location.Reference = push.Reference
		meta.Location.ManifestDigest = push.ManifestDigest
	}
	return meta, nil
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func cloneStringSlice(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

func cloneInterfaceMap(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
