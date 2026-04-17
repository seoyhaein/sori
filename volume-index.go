package sori

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/seoyhaein/sori/registryutil"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

type (
	Partition struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		ManifestRef string `json:"manifest_ref"`
		CreatedAt   string `json:"created_at"`
		Compression string `json:"compression"`
	}
	VolumeIndex struct {
		VolumeRef   string      `json:"volume_ref"`
		DisplayName string      `json:"display_name"`
		CreatedAt   string      `json:"created_at"`
		Partitions  []Partition `json:"partitions"`
	}
	ConfigBlob  map[string]interface{}
	VolumeEntry struct {
		Index      VolumeIndex `json:"index"`
		ConfigBlob ConfigBlob  `json:"configBlob"`
	}
	VolumeCollection struct {
		Version int           `json:"version"`
		Volumes []VolumeEntry `json:"volumes"`
	}
	CollectionManager struct {
		mu    sync.RWMutex
		root  string
		coll  *VolumeCollection
		byRef map[string]int
	}
	PackageRequest struct {
		SourceDir   string            `json:"source_dir"`
		DisplayName string            `json:"display_name"`
		Tag         string            `json:"tag"`
		Dataset     string            `json:"dataset,omitempty"`
		Version     string            `json:"version,omitempty"`
		StableRef   string            `json:"stable_ref,omitempty"`
		Description string            `json:"description,omitempty"`
		Annotations map[string]string `json:"annotations,omitempty"`
		ConfigBlob  []byte            `json:"-"`
	}
	PackageResult struct {
		StableRef      string      `json:"stable_ref"`
		LocalTag       string      `json:"local_tag"`
		ManifestDigest string      `json:"manifest_digest"`
		ConfigDigest   string      `json:"config_digest"`
		TotalSize      int64       `json:"total_size"`
		CreatedAt      string      `json:"created_at"`
		Partitions     []Partition `json:"partitions"`
		VolumeIndex    VolumeIndex `json:"volume_index"`
	}
	RemoteTarget struct {
		Registry            string              `json:"registry"`
		Repository          string              `json:"repository"`
		PlainHTTP           bool                `json:"plain_http"`
		InsecureTLS         bool                `json:"insecure_tls,omitempty"`
		Username            string              `json:"username,omitempty"`
		Password            string              `json:"password,omitempty"`
		Token               string              `json:"token,omitempty"`
		CAFile              string              `json:"ca_file,omitempty"`
		HTTPClient          *http.Client        `json:"-"`
		Transport           http.RoundTripper   `json:"-"`
		AuthProvider        auth.CredentialFunc `json:"-"`
		ReferrersCapability *bool               `json:"-"`
	}
	PushResult struct {
		Reference      string `json:"reference"`
		Repository     string `json:"repository"`
		Tag            string `json:"tag"`
		ManifestDigest string `json:"manifest_digest"`
	}
	ReferrerPushResult struct {
		SubjectDigest  string `json:"subject_digest"`
		ManifestDigest string `json:"manifest_digest"`
		ConfigDigest   string `json:"config_digest"`
		Repository     string `json:"repository"`
		ArtifactType   string `json:"artifact_type"`
	}
	DataSpec struct {
		Identity   DataIdentity   `json:"identity"`
		Data       DataSection    `json:"data"`
		Display    DataDisplay    `json:"display"`
		Provenance DataProvenance `json:"provenance"`
	}
	DataIdentity struct {
		StableRef string `json:"stableRef"`
		Dataset   string `json:"dataset,omitempty"`
		Version   string `json:"version,omitempty"`
	}
	DataSection struct {
		ArtifactType   string      `json:"artifactType"`
		Repository     string      `json:"repository,omitempty"`
		Reference      string      `json:"reference,omitempty"`
		ManifestDigest string      `json:"manifestDigest,omitempty"`
		ConfigDigest   string      `json:"configDigest"`
		TotalSize      int64       `json:"totalSize"`
		Partitions     []Partition `json:"partitions"`
	}
	DataDisplay struct {
		Name        string            `json:"name"`
		Description string            `json:"description,omitempty"`
		Annotations map[string]string `json:"annotations,omitempty"`
	}
	DataProvenance struct {
		PackagedAt string `json:"packagedAt"`
		SourceDir  string `json:"sourceDir"`
		LocalTag   string `json:"localTag"`
	}
)

const (
	ConfigBlobJson    = "configblob.json"
	CollectionJson    = "volume-collection.json"
	VolumeIndexJson   = "volume-index.json"
	DataSpecMediaType = "application/vnd.nodevault.dataspec.v1+json"
)

func PackageVolume(ctx context.Context, req PackageRequest) (*PackageResult, error) {
	return NewClient().PackageVolume(ctx, req)
}

func PackageVolumeToStore(ctx context.Context, localStorePath string, req PackageRequest) (*PackageResult, error) {
	return packageVolumeToStoreWithOptions(ctx, localStorePath, req, PackageOptions{ConfigBlob: req.ConfigBlob})
}

func packageVolumeToStoreWithOptions(ctx context.Context, localStorePath string, req PackageRequest, opts PackageOptions) (*PackageResult, error) {
	if strings.TrimSpace(localStorePath) == "" {
		return nil, validationError("PackageVolumeToStore", "local store path is required", nil)
	}
	if strings.TrimSpace(req.SourceDir) == "" {
		return nil, validationError("PackageVolumeToStore", "source_dir is required", nil)
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		return nil, validationError("PackageVolumeToStore", "display_name is required", nil)
	}
	if strings.TrimSpace(req.Tag) == "" {
		return nil, validationError("PackageVolumeToStore", "tag is required", nil)
	}

	var configBlob []byte
	if len(opts.ConfigBlob) > 0 {
		req.ConfigBlob = opts.ConfigBlob
	}
	if len(req.ConfigBlob) == 0 {
		if opts.RequireConfigBlob {
			return nil, validationError("PackageVolumeToStore", "config blob is required by options", nil)
		}
		var err error
		configBlob, err = ValidateVolumeDir(req.SourceDir)
		if err != nil {
			return nil, err
		}
	} else {
		if err := validateJSONBytes(req.ConfigBlob); err != nil {
			return nil, validationError("PackageVolumeToStore", "invalid config blob", err)
		}
		configBlob = append([]byte(nil), req.ConfigBlob...)
	}

	vi, err := GenerateVolumeIndex(req.SourceDir, req.DisplayName)
	if err != nil {
		return nil, transportError("PackageVolumeToStore", "generate volume index", err)
	}

	published, err := vi.publishVolumeToStore(ctx, localStorePath, req.SourceDir, req.Tag, configBlob)
	if err != nil {
		return nil, err
	}

	totalSize, err := dirRegularFileSize(req.SourceDir)
	if err != nil {
		return nil, transportError("PackageVolumeToStore", "compute total size", err)
	}

	return &PackageResult{
		StableRef:      deriveStableRef(req),
		LocalTag:       req.Tag,
		ManifestDigest: published.VolumeRef,
		ConfigDigest:   digest.FromBytes(configBlob).String(),
		TotalSize:      totalSize,
		CreatedAt:      published.CreatedAt,
		Partitions:     append([]Partition(nil), published.Partitions...),
		VolumeIndex:    *published,
	}, nil
}

func PushPackagedVolume(ctx context.Context, localStorePath string, pkg *PackageResult, target RemoteTarget) (*PushResult, error) {
	if pkg == nil {
		return nil, validationError("PushPackagedVolume", "package result is required", nil)
	}
	if strings.TrimSpace(pkg.LocalTag) == "" {
		return nil, validationError("PushPackagedVolume", "package result local tag is empty", nil)
	}
	if strings.TrimSpace(target.Registry) == "" {
		return nil, validationError("PushPackagedVolume", "remote target registry is required", nil)
	}
	if strings.TrimSpace(target.Repository) == "" {
		return nil, validationError("PushPackagedVolume", "remote target repository is required", nil)
	}

	remoteRepo := strings.TrimRight(target.Registry, "/") + "/" + strings.TrimLeft(target.Repository, "/")
	repo, err := newRemoteRepository(remoteRepo, target)
	if err != nil {
		return nil, err
	}
	return pushLocalTagToRepository(ctx, localStorePath, pkg.LocalTag, repo)
}

func BuildDataSpec(pkg *PackageResult, push *PushResult, req PackageRequest) (*DataSpec, error) {
	meta, err := BuildArtifactMetadata(ArtifactMetadataInput{
		Kind:        "dataset",
		Name:        defaultString(req.Dataset, req.Tag),
		Version:     req.Version,
		StableRef:   defaultString(pkg.StableRef, req.StableRef),
		DisplayName: req.DisplayName,
		Description: req.Description,
		SourceDir:   req.SourceDir,
		Annotations: req.Annotations,
	}, pkg, push)
	if err != nil {
		return nil, err
	}
	return ArtifactMetadataToDataSpec(meta), nil
}

func ArtifactMetadataToDataSpec(meta *ArtifactMetadata) *DataSpec {
	if meta == nil {
		return nil
	}
	return &DataSpec{
		Identity: DataIdentity{
			StableRef: meta.Identity.StableRef,
			Dataset:   meta.Identity.Name,
			Version:   meta.Identity.Version,
		},
		Data: DataSection{
			ArtifactType:   ocispec.MediaTypeImageManifest,
			Repository:     meta.Location.Repository,
			Reference:      meta.Location.Reference,
			ManifestDigest: meta.Location.ManifestDigest,
			ConfigDigest:   meta.Location.ConfigDigest,
			TotalSize:      meta.Contents.TotalSize,
			Partitions:     append([]Partition(nil), meta.Contents.Partitions...),
		},
		Display: DataDisplay{
			Name:        meta.Display.Name,
			Description: meta.Display.Description,
			Annotations: cloneAnnotations(meta.Annotations),
		},
		Provenance: DataProvenance{
			PackagedAt: meta.Contents.CreatedAt,
			SourceDir:  meta.Source.SourceDir,
			LocalTag:   meta.Location.LocalTag,
		},
	}
}

func PushRemoteDataSpecReferrer(ctx context.Context, push *PushResult, target RemoteTarget, spec *DataSpec) (*ReferrerPushResult, error) {
	if push == nil {
		return nil, validationError("PushRemoteDataSpecReferrer", "push result is required", nil)
	}
	if spec == nil {
		return nil, validationError("PushRemoteDataSpecReferrer", "data spec is required", nil)
	}
	if strings.TrimSpace(push.Repository) == "" {
		return nil, validationError("PushRemoteDataSpecReferrer", "push result repository is empty", nil)
	}
	if strings.TrimSpace(push.ManifestDigest) == "" {
		return nil, validationError("PushRemoteDataSpecReferrer", "push result manifest digest is empty", nil)
	}

	repo, err := newRemoteRepository(push.Repository, target)
	if err != nil {
		return nil, err
	}
	subjectDesc, err := repo.Resolve(ctx, push.ManifestDigest)
	if err != nil {
		return nil, notFoundError("PushRemoteDataSpecReferrer", "resolve subject manifest", err)
	}

	result, err := pushDataSpecManifest(ctx, repo, subjectDesc, spec)
	if err != nil {
		return nil, err
	}
	result.Repository = push.Repository
	return result, nil
}

func deriveStableRef(req PackageRequest) string {
	if ref := strings.TrimSpace(req.StableRef); ref != "" {
		return ref
	}
	dataset := strings.TrimSpace(req.Dataset)
	version := strings.TrimSpace(req.Version)
	switch {
	case dataset != "" && version != "":
		return dataset + ":" + version
	case dataset != "":
		return dataset
	default:
		return strings.TrimSpace(req.Tag)
	}
}

func cloneAnnotations(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func newRemoteRepository(remoteRepo string, target RemoteTarget) (*remote.Repository, error) {
	return registryutil.NewRepository(remoteRepo, registryutil.RemoteConfig{
		PlainHTTP:           target.PlainHTTP,
		InsecureTLS:         target.InsecureTLS,
		Username:            target.Username,
		Password:            target.Password,
		Token:               target.Token,
		CAFile:              target.CAFile,
		HTTPClient:          target.HTTPClient,
		Transport:           target.Transport,
		AuthProvider:        target.AuthProvider,
		ReferrersCapability: target.ReferrersCapability,
	})
}
