package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	volresv1 "github.com/seoyhaein/api-protos/gen/go/volres/ichthys"
	globallog "github.com/seoyhaein/sori/log"
	"io"
	"oras.land/oras-go/v2"
	contentFile "oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var logger = globallog.Log

// TODO 파일이름은 일단 그냥 생각없이 지었음.

// NewVolumeManifest 폴더(rootPath)로부터 VolumeManifest를 생성
// volumeRef, displayName, description, format, annotations는 호출자가 지정
// TODO 최적화 해야 함.
func NewVolumeManifest(rootPath, displayName, description, format string, annotations map[string]string) (*volresv1.VolumeManifest, error) {
	// 1) 디렉터리 스캔 후 Resource 트리 & 메타 집계
	res, totalSize, recordCount, err := buildResource(rootPath, rootPath)
	if err != nil {
		return nil, err
	}

	// 2) 트리 기반으로 볼륨 해시 생성
	volumeRef := computeVolumeRef(res)

	// 3) Manifest 생성
	return &volresv1.VolumeManifest{
		VolumeRef:   volumeRef,
		DisplayName: displayName,
		Description: description,
		Format:      format,
		TotalSize:   totalSize,
		RecordCount: recordCount,
		CreatedAt:   time.Now().Format(time.RFC3339),
		Annotations: annotations,
		Root:        res,
	}, nil
}

// buildResource 는 path 위치의 파일/디렉터리에 대한 VolumeResource 를 만들고,
// 그 아래로 들어가면서 totalSize, recordCount 를 집계하여 반환
// TODO path, rootPath 들어가 있는데 파악해야힘. 최적화 해야함.
func buildResource(path, rootPath string) (*volresv1.VolumeResource, uint64, uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, 0, err
	}

	// 상대 경로 계산
	rel, err := filepath.Rel(rootPath, path)
	if err != nil {
		return nil, 0, 0, err
	}

	id, err := idFromPath(path)
	if err != nil {
		return nil, 0, 0, err
	}
	vr := &volresv1.VolumeResource{
		Id:          id,
		Basename:    info.Name(),
		FullPath:    rel,
		IsDirectory: info.IsDir(),
		Size:        uint64(info.Size()),
		ModTime:     info.ModTime().Unix(),
		Attrs:       map[string]string{}, // 필요하면 여기에 추가 속성 채우기
	}

	var totalSize uint64
	var recordCount uint64

	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, 0, 0, err
		}
		for _, e := range entries {
			childPath := filepath.Join(path, e.Name())
			child, cs, rc, err := buildResource(childPath, rootPath)
			if err != nil {
				return nil, 0, 0, err
			}
			vr.Children = append(vr.Children, child)
			totalSize += cs
			recordCount += rc
		}
	} else {
		// 파일인 경우 체크섬 계산
		sum, err := sha256File(path)
		if err != nil {
			return nil, 0, 0, err
		}
		vr.Checksum = sum
		totalSize = uint64(info.Size())
		recordCount = 1
	}

	return vr, totalSize, recordCount, nil
}

// idFromPath path 가 디렉토리면 nil 값을 적용하고, 파일이면 SHA-256 해쉬 를 붙여서 파일이 변경되었는지 알 수 있도록 함.
func idFromPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		// 디렉터리는 ID를 비워둡니다.
		return "", nil
	}
	// 파일인 경우에만 SHA-256 해시 계산
	sum, err := sha256File(path)
	if err != nil {
		return "", err
	}
	return sum, nil
}

// sha256File 파일 내용을 읽어 SHA-256 헥사 문자열을 반환
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	// TODO closure 라서 이렇게 않해도 되지만. 일단 그냥 이렇게 해줌. 별로 상관없지만, 코드 스타일 맞추기 위해서 전체적으로 한번 봐줘야 함.
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			logger.Warnf("failed to close file %s: %v", path, err)
		}
	}(f)

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// computeVolumeRef는 트리 구조의 루트 리소스(res)를 순회하며
// 파일별 ID(=SHA256)와 경로를 일관된 순서로 해시해
// 볼륨 전체의 고유 해시를 반환합니다.
func computeVolumeRef(res *volresv1.VolumeResource) string {
	hasher := sha256.New()
	// 재귀적으로 flatten
	var walk func(r *volresv1.VolumeResource)
	walk = func(r *volresv1.VolumeResource) {
		// 파일인 경우 “경로:파일ID” 문자열을 해시
		if !r.IsDirectory && r.Id != "" {
			hasher.Write([]byte(r.FullPath))
			hasher.Write([]byte{0})    // 구분자
			hasher.Write([]byte(r.Id)) // 파일 콘텐츠 해시
			hasher.Write([]byte{0})    // 구분자
		}
		// 디렉터리는 Children만 순회하되, 이름순 정렬을 보장
		if r.IsDirectory && len(r.Children) > 0 {
			// 이름순 정렬
			sort.Slice(r.Children, func(i, j int) bool {
				return r.Children[i].Basename < r.Children[j].Basename
			})
			for _, c := range r.Children {
				walk(c)
			}
		}
	}
	walk(res)

	return hex.EncodeToString(hasher.Sum(nil))
}

// 1) 폴더를 tar.gz로 묶으면서 동시에 해시 계산 TODO 일단 그냥 넣음. 최적화 해야 함.
func PackDirToTarGzWithDigest(srcDir, destTarGz string) (string, error) {
	out, err := os.Create(destTarGz)
	if err != nil {
		return "", err
	}
	defer out.Close()

	// gzip + tar writer
	gz := gzip.NewWriter(out)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	// 해시 계산용 reader
	// (gzip된 바이트 스트림 전체에 대한 해시를 원하면 tar→gzip 순서 뒤에 wrap 해야 함)
	hasher := sha256.New()
	//mw := io.MultiWriter(out, hasher) // out 대신 gz나 tw를 쓸 수도 있음, 여기서는 gzip 레벨 해시 예시
	io.MultiWriter(out, hasher) // out 대신 gz나 tw를 쓸 수도 있음, 여기서는 gzip 레벨 해시 예시

	// 실제 tar 쓰기는 tw, 해시는 mw 로 따로 흐름이 필요하니
	// 간단하게, TarGz 완성 후 파일 전체를 다시 읽어 해시를 계산할 수도 있습니다.

	// (편의상) tar.gz 작성은 tw, 나중에 파일 전체 읽어 sha256File(destTarGz)
	if err := filepath.Walk(srcDir, func(file string, fi os.FileInfo, _ error) error {
		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(filepath.Dir(srcDir), file)
		if err != nil {
			return err
		}
		hdr.Name = relPath
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return "", err
	}

	// tar+gzip 모두 Close 한 뒤, 파일 전체 해시 계산
	return sha256File(destTarGz)
}

// TODO oras-go 설치 해야함. 버그 그대로 나둠.
// 예: oras-go로 레이어 push 후 digest 받기
// PushLayerAndGetDigest
//   - ctx: 호출 컨텍스트
//   - tarGzPath: 로컬에 생성된 tar.gz 파일 전체 경로
//   - targetRef: "registry.io/repo/name:tag" 또는 "@sha256:..." 형태
//
// 반환값: 성공 시 "sha256:xxxxx…" 형태의 레이어 다이제스트
func PushLayerAndGetDigest(ctx context.Context, tarGzPath, targetRef string) (string, error) {
	// 1) 레퍼런스 파싱
	ref, err := registry.ParseReference(targetRef)
	if err != nil {
		return "", fmt.Errorf("reference parse failed: %w", err)
	}

	// 2) 파일 콘텐츠 스토어 준비 (tar.gz가 있는 디렉터리)
	workDir := filepath.Dir(tarGzPath)
	store, err := contentFile.New(workDir)
	if err != nil {
		return "", fmt.Errorf("file store init failed: %w", err)
	}
	defer store.Close()

	// 3) tar.gz 파일을 콘텐츠 스토어에 추가
	//    mediaType은 gzip 압축된 OCI 레이어 타입
	layerName := filepath.Base(tarGzPath)
	desc, err := store.Add(ctx, layerName, ocispec.MediaTypeImageLayerGzip, "")
	if err != nil {
		return "", fmt.Errorf("adding file to store failed: %w", err)
	}

	// 4) 원격 레포(repository) 클라이언트 생성
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return "", fmt.Errorf("repository client init failed: %w", err)
	}

	// 5) 콘텐츠 복사(push)
	//    store에 담긴 desc를 repo로 복사(push)합니다.
	pushed, err := oras.Copy(ctx, store, desc, repo, oras.WithCopyConfig(oras.DefaultCopyConfig()))
	if err != nil {
		return "", fmt.Errorf("push failed: %w", err)
	}

	// 6) 푸시된 descriptors 중 gzip 레이어의 digest를 찾아 반환
	for _, d := range pushed {
		if d.MediaType == ocispec.MediaTypeImageLayerGzip {
			return d.Digest.String(), nil
		}
	}

	return "", fmt.Errorf("layer digest not found in pushed descriptors")
}
