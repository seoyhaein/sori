package sori

// referrer.go — OCI referrer artifact push (NodeVault spec referrer 연결용).
//
// 사용 패턴:
//
//	specJSON, _ := sori.MarshalSpec(mySpec)
//	result, err := sori.PushToolSpecReferrer(ctx, store, imageDigest, specJSON)
//
// subject (툴 이미지) digest에 spec JSON을 OCI referrer로 연결한다.
// Harbor Referrers API (GET /v2/{name}/referrers/{digest})로 조회 가능.

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/seoyhaein/sori/registryutil"
	"strings"

	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	orasoras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote/auth"
)

const (
	// MediaTypeToolSpec is the OCI media type for a NodeVault tool spec referrer.
	MediaTypeToolSpec = "application/vnd.nodevault.toolspec.v1+json"

	// MediaTypeDataSpec is the OCI media type for a NodeVault data spec referrer.
	MediaTypeDataSpec = "application/vnd.nodevault.dataspec.v1+json"
)

// ReferrerTarget is an oras-go content store that can accept pushes.
// Satisfied by *oci.Store (local) and *remote.Repository (Harbor).
type ReferrerTarget interface {
	orasoras.Target
}

// SpecReferrerResult is returned by a successful referrer push.
type SpecReferrerResult struct {
	// ReferrerDigest is the digest of the pushed referrer manifest.
	ReferrerDigest string
	// SubjectDigest is the subject image digest the referrer is attached to.
	SubjectDigest string
	// MediaType is the artifact media type used for the referrer config.
	MediaType string
}

// PushToolSpecReferrer attaches specJSON as an OCI referrer artifact linked
// to subjectDigest in the given target store.
// mediaType of the config blob is MediaTypeToolSpec.
//
// Both oci.Store (local testing) and remote.Repository (Harbor) satisfy ReferrerTarget.
func PushToolSpecReferrer(ctx context.Context, target ReferrerTarget, subjectDigest string, specJSON []byte) (SpecReferrerResult, error) {
	return pushSpecReferrer(ctx, target, subjectDigest, specJSON, MediaTypeToolSpec)
}

// PushDataSpecReferrer attaches specJSON as an OCI referrer artifact linked
// to subjectDigest in the given target store.
// mediaType of the config blob is MediaTypeDataSpec.
func PushDataSpecReferrer(ctx context.Context, target ReferrerTarget, subjectDigest string, specJSON []byte) (SpecReferrerResult, error) {
	return pushSpecReferrer(ctx, target, subjectDigest, specJSON, MediaTypeDataSpec)
}

// NewReferrerLocalStore opens (or creates) an OCI layout store at the given path.
// Useful for local testing without a running registry.
func NewReferrerLocalStore(path string) (ReferrerTarget, error) {
	store, err := oci.New(path)
	if err != nil {
		return nil, transportError("NewReferrerLocalStore", "open OCI layout store", err)
	}
	return store, nil
}

// NewReferrerRemoteRepository creates a remote.Repository pointed at repoRef.
// If plainHTTP is true, HTTP is used instead of HTTPS.
// credential may be nil for anonymous access.
func NewReferrerRemoteRepository(repoRef string, plainHTTP bool, credential *auth.Credential) (ReferrerTarget, error) {
	cfg := registryutil.RemoteConfig{PlainHTTP: plainHTTP}
	if credential != nil {
		cfg.Username = credential.Username
		cfg.Password = credential.Password
		cfg.Token = credential.AccessToken
	}
	repo, err := registryutil.NewRepository(repoRef, cfg)
	if err != nil {
		return nil, transportError("NewReferrerRemoteRepository", "create remote repository "+repoRef, err)
	}
	return repo, nil
}

// MarshalSpec marshals v to JSON for use as specJSON in Push* functions.
func MarshalSpec(v interface{}) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, transportError("MarshalSpec", "marshal spec JSON", err)
	}
	return data, nil
}

// ── internal ──────────────────────────────────────────────────────────────────

func pushSpecReferrer(
	ctx context.Context,
	target ReferrerTarget,
	subjectDigest string,
	specJSON []byte,
	mediaType string,
) (SpecReferrerResult, error) {
	if subjectDigest == "" {
		return SpecReferrerResult{}, validationError("pushSpecReferrer", "subjectDigest must not be empty", nil)
	}
	if len(specJSON) == 0 {
		return SpecReferrerResult{}, validationError("pushSpecReferrer", "specJSON must not be empty", nil)
	}

	// 1. Config blob = specJSON (payload는 config에 담는 ORAS referrer 관례)
	configDesc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    godigest.FromBytes(specJSON),
		Size:      int64(len(specJSON)),
	}
	if err := pushBlobIfAbsent(ctx, target, configDesc, specJSON); err != nil {
		return SpecReferrerResult{}, transportError("pushSpecReferrer", "push config blob", err)
	}

	// 2. Empty layers — referrer artifact carries payload in config
	layers := []ocispec.Descriptor{}

	// 3. Build referrer manifest; subject points to the tool image
	subjectDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    godigest.Digest(subjectDigest),
	}
	manifestDesc, err := orasoras.PackManifest(
		ctx, target,
		orasoras.PackManifestVersion1_1,
		ocispec.MediaTypeImageManifest,
		orasoras.PackManifestOptions{
			Subject:          &subjectDesc,
			ConfigDescriptor: &configDesc,
			Layers:           layers,
		},
	)
	if err != nil {
		return SpecReferrerResult{}, transportError("pushSpecReferrer", "pack referrer manifest", err)
	}

	return SpecReferrerResult{
		ReferrerDigest: manifestDesc.Digest.String(),
		SubjectDigest:  subjectDigest,
		MediaType:      mediaType,
	}, nil
}

// pushBlobIfAbsent pushes data to target; "already exists" errors are silently ignored.
// Target always satisfies content.Pusher via orasoras.Target.
func pushBlobIfAbsent(ctx context.Context, target ReferrerTarget, desc ocispec.Descriptor, data []byte) error {
	if err := target.Push(ctx, desc, bytes.NewReader(data)); err != nil {
		if !isExistError(err) {
			return err
		}
	}
	return nil
}

// isExistError reports whether err indicates a blob already exists in the target.
func isExistError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, needle := range []string{"already exists", "conflict", "409"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
