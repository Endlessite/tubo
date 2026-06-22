package main

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// prevent ../../ escapes
func sanitizeTarPath(name string) string {
	name = filepath.Clean(name)
	name = filepath.ToSlash(name)
	if strings.HasPrefix(name, "../") || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return filepath.Base(name)
	}
	return name
}

func tarDirectory(dirPath string, writer io.Writer) error {
	tw := tar.NewWriter(writer)
	defer tw.Close()

	baseName := filepath.Base(dirPath)

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			relPath = baseName
		} else {
			relPath = filepath.Join(baseName, relPath)
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
		}

		return nil
	})
	return err
}

func extractTar(reader io.Reader, destDir string) error {
	tr := tar.NewReader(reader)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		cleanName := sanitizeTarPath(header.Name)
		parts := strings.Split(cleanName, "/")
		if len(parts) > 1 {
			cleanName = strings.Join(parts[1:], "/")
		} else {
			if header.Typeflag == tar.TypeDir {
				continue
			}
		}

		target := filepath.Join(destDir, cleanName)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			os.Remove(target)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}

func dirSize(path string) int64 {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
