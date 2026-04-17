package sori

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/opencontainers/go-digest"
	"github.com/seoyhaein/sori/catalogutil"
	"sort"
	"strings"
	"sync"
	"time"
)

const registeredDataCatalogJSON = "registered-data.json"

type DisplaySpec struct {
	Label       string   `json:"label,omitempty"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type DataRegisterRequest struct {
	RequestID   string      `json:"request_id,omitempty"`
	DataName    string      `json:"data_name"`
	Version     string      `json:"version,omitempty"`
	Description string      `json:"description,omitempty"`
	Format      string      `json:"format,omitempty"`
	SourceURI   string      `json:"source_uri,omitempty"`
	Checksum    string      `json:"checksum,omitempty"`
	StorageURI  string      `json:"storage_uri,omitempty"`
	StableRef   string      `json:"stable_ref,omitempty"`
	Display     DisplaySpec `json:"display,omitempty"`
}

type RegisteredDataDefinition struct {
	CASHash         string      `json:"cas_hash"`
	DataName        string      `json:"data_name"`
	Version         string      `json:"version,omitempty"`
	Description     string      `json:"description,omitempty"`
	Format          string      `json:"format,omitempty"`
	SourceURI       string      `json:"source_uri,omitempty"`
	Checksum        string      `json:"checksum,omitempty"`
	StorageURI      string      `json:"storage_uri,omitempty"`
	StableRef       string      `json:"stable_ref"`
	Display         DisplaySpec `json:"display,omitempty"`
	RegisteredAt    int64       `json:"registered_at"`
	LifecyclePhase  string      `json:"lifecycle_phase"`
	IntegrityHealth string      `json:"integrity_health"`
}

type DataRegisterResponse struct {
	CASHash string                    `json:"cas_hash"`
	Data    *RegisteredDataDefinition `json:"data"`
}

type DataCatalog struct {
	mu      sync.RWMutex
	rootDir string
}

type registeredDataCatalog struct {
	Version int                        `json:"version"`
	Data    []RegisteredDataDefinition `json:"data"`
}

func NewDataCatalog(rootDir string) *DataCatalog {
	return &DataCatalog{rootDir: rootDir}
}

func RegisterPackagedData(ctx context.Context, rootDir string, req DataRegisterRequest, pkg *PackageResult, push *PushResult) (*DataRegisterResponse, error) {
	cat := NewDataCatalog(rootDir)
	return cat.Register(ctx, req, pkg, push)
}

func (c *DataCatalog) Register(_ context.Context, req DataRegisterRequest, pkg *PackageResult, push *PushResult) (*DataRegisterResponse, error) {
	def, err := BuildRegisteredDataDefinition(req, pkg, push)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	coll, err := c.load()
	if err != nil {
		return nil, err
	}

	for i := range coll.Data {
		if coll.Data[i].CASHash == def.CASHash {
			coll.Data[i] = *def
			if err := c.save(coll); err != nil {
				return nil, err
			}
			return &DataRegisterResponse{CASHash: def.CASHash, Data: def}, nil
		}
	}

	coll.Data = append(coll.Data, *def)
	coll.Version++
	sort.Slice(coll.Data, func(i, j int) bool {
		if coll.Data[i].StableRef == coll.Data[j].StableRef {
			return coll.Data[i].RegisteredAt > coll.Data[j].RegisteredAt
		}
		return coll.Data[i].StableRef < coll.Data[j].StableRef
	})
	if err := c.save(coll); err != nil {
		return nil, err
	}
	return &DataRegisterResponse{CASHash: def.CASHash, Data: def}, nil
}

func (c *DataCatalog) Get(casHash string) (*RegisteredDataDefinition, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	coll, err := c.load()
	if err != nil {
		return nil, err
	}
	for i := range coll.Data {
		if coll.Data[i].CASHash == casHash {
			item := coll.Data[i]
			return &item, nil
		}
	}
	return nil, notFoundError("DataCatalog.Get", fmt.Sprintf("registered data %q not found", casHash), nil)
}

func (c *DataCatalog) List(stableRef string) ([]RegisteredDataDefinition, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	coll, err := c.load()
	if err != nil {
		return nil, err
	}
	if stableRef == "" {
		out := make([]RegisteredDataDefinition, len(coll.Data))
		copy(out, coll.Data)
		return out, nil
	}

	out := make([]RegisteredDataDefinition, 0, len(coll.Data))
	for _, item := range coll.Data {
		if item.StableRef == stableRef {
			out = append(out, item)
		}
	}
	return out, nil
}

func BuildRegisteredDataDefinition(req DataRegisterRequest, pkg *PackageResult, push *PushResult) (*RegisteredDataDefinition, error) {
	meta, err := BuildArtifactMetadata(ArtifactMetadataInput{
		Kind:        "dataset",
		Name:        req.DataName,
		Version:     req.Version,
		StableRef:   defaultString(req.StableRef, buildRegisteredStableRef(req.DataName, req.Version)),
		DisplayName: defaultString(req.Display.Label, req.DataName),
		Description: req.Description,
		Category:    req.Display.Category,
		Tags:        req.Display.Tags,
		Format:      req.Format,
		SourceURI:   req.SourceURI,
	}, pkg, push)
	if err != nil {
		return nil, err
	}
	def := ArtifactMetadataToRegisteredDataDefinition(meta, req)
	raw, err := json.Marshal(struct {
		DataName    string      `json:"data_name"`
		Version     string      `json:"version,omitempty"`
		Description string      `json:"description,omitempty"`
		Format      string      `json:"format,omitempty"`
		SourceURI   string      `json:"source_uri,omitempty"`
		Checksum    string      `json:"checksum,omitempty"`
		StorageURI  string      `json:"storage_uri,omitempty"`
		StableRef   string      `json:"stable_ref"`
		Display     DisplaySpec `json:"display,omitempty"`
	}{
		DataName:    def.DataName,
		Version:     def.Version,
		Description: def.Description,
		Format:      def.Format,
		SourceURI:   def.SourceURI,
		Checksum:    def.Checksum,
		StorageURI:  def.StorageURI,
		StableRef:   def.StableRef,
		Display:     def.Display,
	})
	if err != nil {
		return nil, transportError("BuildRegisteredDataDefinition", "marshal registered data definition", err)
	}
	def.CASHash = digest.FromBytes(raw).String()
	return def, nil
}

func ArtifactMetadataToRegisteredDataDefinition(meta *ArtifactMetadata, req DataRegisterRequest) *RegisteredDataDefinition {
	if meta == nil {
		return nil
	}
	display := DisplaySpec{
		Label:       defaultString(req.Display.Label, meta.Display.Name),
		Description: defaultString(req.Display.Description, meta.Display.Description),
		Category:    defaultString(req.Display.Category, meta.Display.Category),
		Tags:        cloneStringSlice(firstNonEmptyTags(req.Display.Tags, meta.Display.Tags)),
	}
	checksum := req.Checksum
	if strings.TrimSpace(checksum) == "" {
		checksum = meta.Location.ManifestDigest
	}
	storageURI := req.StorageURI
	if strings.TrimSpace(storageURI) == "" {
		storageURI = firstNonEmptyString(meta.Location.Reference, meta.Location.LocalTag)
	}
	return &RegisteredDataDefinition{
		DataName:        meta.Identity.Name,
		Version:         meta.Identity.Version,
		Description:     meta.Display.Description,
		Format:          meta.Contents.Format,
		SourceURI:       meta.Source.SourceURI,
		Checksum:        checksum,
		StorageURI:      storageURI,
		StableRef:       meta.Identity.StableRef,
		Display:         display,
		RegisteredAt:    time.Now().Unix(),
		LifecyclePhase:  "Active",
		IntegrityHealth: "Healthy",
	}
}

func buildRegisteredStableRef(dataName, version string) string {
	dataName = strings.TrimSpace(dataName)
	version = strings.TrimSpace(version)
	if dataName == "" {
		return ""
	}
	if version == "" {
		return dataName
	}
	return dataName + "@" + version
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstNonEmptyTags(values ...[]string) []string {
	for _, v := range values {
		if len(v) > 0 {
			return v
		}
	}
	return nil
}

func (c *DataCatalog) load() (*registeredDataCatalog, error) {
	coll, err := catalogutil.LoadOrInit(c.rootDir, registeredDataCatalogJSON, registeredDataCatalog{Version: 1, Data: nil})
	if err != nil {
		return nil, err
	}
	if coll.Version == 0 {
		coll.Version = 1
	}
	return coll, nil
}

func (c *DataCatalog) save(coll *registeredDataCatalog) error {
	return catalogutil.Save(c.rootDir, registeredDataCatalogJSON, coll)
}
