package main

import (
	"fmt"
	"github.com/studio-b12/gowebdav"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

type RemoteImage struct {
	Url   string
	Mtime time.Time
	Size  int64
}

type WebDAVSync struct {
	rootDir       string
	remoteUrlRoot string

	client *gowebdav.Client

	fileMap map[string]RemoteImage
}

var _ ImageSyncer = &WebDAVSync{}

func NewWebDAVSync(client *gowebdav.Client, rootDir, remoteUrlRoot string) *WebDAVSync {
	return &WebDAVSync{
		client:        client,
		rootDir:       rootDir,
		remoteUrlRoot: remoteUrlRoot,
		fileMap:       make(map[string]RemoteImage),
	}
}

func (w *WebDAVSync) BuildImageMap() error {
	err := w.readList("")
	if err != nil {
		return err
	}
	return nil
}

func (w *WebDAVSync) readList(curRelPath string) error {
	fileInfos, err := w.client.ReadDir(curRelPath)
	if err != nil {
		return err
	}
	for _, fi := range fileInfos {
		if fi.IsDir() {
			dirName := fi.Name()
			// Make sure we're not getting hacked
			_, err := EnsurePathIsRelativeToItsLocation(dirName, false)
			if err != nil {
				return fmt.Errorf("DANGER! Received a malicious filename from WebDAV: %s", dirName)
			}
			err = w.readList(path.Join(curRelPath, dirName))
			if err != nil {
				return err
			}
		} else {
			w.fileMap[path.Join(curRelPath, fi.Name())] = RemoteImage{
				Url:   path.Join(curRelPath, fi.Name()),
				Mtime: fi.ModTime(),
				Size:  fi.Size(),
			}
		}
	}

	return nil
}

func (w *WebDAVSync) EnsureLocalImageIsUploaded(img LocalImage) (string, error) {
	// Check if we already have this image
	if remoteImg, ok := w.fileMap[img.relPath]; ok {
		// Check if it's the same image
		if remoteImg.Size == img.size && !remoteImg.Mtime.Before(img.mtime) {
			return remoteImg.Url, nil
		}
	}

	slog.Default().Info("Uploading new or changed local image", slog.String("path", img.relPath),
		slog.String("name", img.fullPath), slog.Int64("size", img.size))

	file, _ := os.Open(img.fullPath)
	defer func() { _ = file.Close() }()

	webDavPath, err := url.JoinPath(w.remoteUrlRoot, img.relPath)
	if err != nil {
		return "", err
	}

	err = w.client.WriteStream(img.relPath, file, 0644)
	if err != nil {
		return "", err
	}

	w.fileMap[img.fullPath] = RemoteImage{
		Url:   webDavPath,
		Mtime: img.mtime,
		Size:  img.size,
	}

	slog.Default().Info("Uploaded local image", slog.String("url", webDavPath))

	return webDavPath, nil
}

func (w *WebDAVSync) DownloadAndSaveImage(fullImageUrl string, postDatePart string, postSlug string) (string, error) {
	// Check if image is relative to the post
	if !strings.HasPrefix(fullImageUrl, w.remoteUrlRoot) {
		return "", nil
	}

	relPath := strings.TrimPrefix(fullImageUrl, w.remoteUrlRoot)
	relPath = strings.TrimPrefix(relPath, "/")

	sanitizedRelPath, err := EnsurePathIsRelativeToItsLocation(relPath, false)
	if err != nil || sanitizedRelPath == "" {
		return "", fmt.Errorf("failed to sanitize the path: %w", err)
	}

	existing, ok := w.fileMap[relPath]
	if ok {
		localFile, err := os.Stat(path.Join(w.rootDir, sanitizedRelPath))
		if err == nil {
			if localFile.Size() == existing.Size {
				slog.Default().Info("The image already exists", slog.String("dest", sanitizedRelPath))
				return sanitizedRelPath, nil
			}
			if localFile.ModTime().After(existing.Mtime) {
				slog.Default().Info("The local image is newer, skipping the download",
					slog.String("dest", sanitizedRelPath))
				return sanitizedRelPath, nil
			}
		}
	}

	slog.Default().Info("Downloading image", slog.String("dest", sanitizedRelPath))

	sanitizedAbsPath := path.Join(w.rootDir, sanitizedRelPath)

	sanitizedAbsDir := path.Dir(sanitizedAbsPath)
	if sanitizedAbsDir != "" {
		slog.Default().Info("Ensuring that the image directory exists", slog.String("dir", sanitizedAbsDir))
		err = os.MkdirAll(sanitizedAbsDir, 0755)
		if err != nil {
			return "", err
		}
	}

	stat, err := w.client.Stat(relPath)
	if err != nil {
		return "", err
	}

	err = w.doDownload(sanitizedAbsPath, sanitizedRelPath, stat.ModTime())
	if err != nil {
		return "", err
	}

	slog.Default().Info("Downloaded the image", slog.String("dest", sanitizedRelPath))

	w.fileMap[relPath] = RemoteImage{
		Url:   fullImageUrl,
		Mtime: stat.ModTime(),
		Size:  stat.Size(),
	}

	return sanitizedRelPath, nil
}

func (w *WebDAVSync) doDownload(absFilePath string, relPath string, mtime time.Time) error {
	reader, err := w.client.ReadStream(relPath)
	if err != nil {
		return err
	}

	file, err := os.Create(absFilePath)
	if err != nil {
		return err
	}
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()

	_, err = io.Copy(file, reader)
	if err != nil {
		return err
	}

	_ = file.Close()
	file = nil

	err = os.Chtimes(absFilePath, time.Time{}, mtime)
	if err != nil {
		return err
	}

	return nil
}
