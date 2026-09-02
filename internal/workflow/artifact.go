// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
package workflow

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

func validateArtifact(artifact, artifactType string) error {
	switch artifactType {
	case "tar.gz", "tgz":
		f, err := os.Open(artifact)
		if err != nil {
			return err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("无效 gzip: %w", err)
		}
		defer gz.Close()
		return validateTar(tar.NewReader(gz))
	case "tar":
		f, err := os.Open(artifact)
		if err != nil {
			return err
		}
		defer f.Close()
		return validateTar(tar.NewReader(f))
	case "zip":
		zr, err := zip.OpenReader(artifact)
		if err != nil {
			return fmt.Errorf("无效 zip: %w", err)
		}
		defer zr.Close()
		for _, f := range zr.File {
			if err := validateArchivePath(f.Name); err != nil {
				return err
			}
			if f.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("zip 包含不允许的符号链接: %q", f.Name)
			}
		}
	}
	return nil
}

func validateTar(tr *tar.Reader) error {
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("无效 tar: %w", err)
		}
		if err := validateArchivePath(h.Name); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeReg, tar.TypeRegA, tar.TypeDir:
		default:
			return fmt.Errorf("tar 包含不允许的链接或特殊文件: %q", h.Name)
		}
	}
}

func validateArchivePath(name string) error {
	if name == "" || strings.ContainsAny(name, "\x00\r\n\\") || strings.HasPrefix(name, "/") {
		return fmt.Errorf("发布包包含不安全路径: %q", name)
	}
	clean := path.Clean(name)
	// 允许 clean == "." (即 "." 或 "./") : 这是 `tar -czf pkg.tar.gz .` 这类
	// CI 常见打包方式生成的根目录条目, 解包时落在目标目录内, 不构成穿越。
	// 只拒绝真正向上逃逸的 ".." / "../xxx"。
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("发布包包含路径穿越: %q", name)
	}
	return nil
}
