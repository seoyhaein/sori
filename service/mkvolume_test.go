package service

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestPackDirToTarGzWithDigest 기본 동작 및 해시 일치 검증
func TestPackDirToTarGzWithDigest(t *testing.T) {
	// 1) 임시 소스 디렉터리 생성
	srcDir, err := os.MkdirTemp("", "volres-test-src-*")
	if err != nil {
		t.Fatalf("임시 디렉터리 생성 실패: %v", err)
	}
	defer os.RemoveAll(srcDir)

	// 2) 그 안에 테스트용 파일 작성
	filePath := filepath.Join(srcDir, "hello.txt")
	content := []byte("hello, world")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("테스트 파일 생성 실패: %v", err)
	}

	// 3) 임시 tar.gz 경로 설정
	tarPath := filepath.Join(os.TempDir(), "volres-test-output.tar.gz")
	// 테스트 반복 시 덮어쓰기 위해 있으면 삭제
	os.Remove(tarPath)
	defer os.Remove(tarPath)

	// 4) PackDirToTarGzWithDigest 호출
	digest, err := PackDirToTarGzWithDigest(srcDir, tarPath)
	if err != nil {
		t.Fatalf("PackDirToTarGzWithDigest 실패: %v", err)
	}

	// 5) 실제 파일 해시 계산과 비교
	expected, err := sha256File(tarPath)
	if err != nil {
		t.Fatalf("tar.gz 해시 계산 실패: %v", err)
	}
	if digest != expected {
		t.Errorf("잘못된 digest:\n반환: %s\n기대: %s", digest, expected)
	}

	// 6) tar.gz 내부에 "hello.txt"가 있는지 간단히 검사
	f, err := os.Open(tarPath)
	if err != nil {
		t.Fatalf("tar.gz 열기 실패: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip 리더 생성 실패: %v", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar 읽기 실패: %v", err)
		}
		if hdr.Name == "hello.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("tarball에 hello.txt가 없습니다")
	}
}
