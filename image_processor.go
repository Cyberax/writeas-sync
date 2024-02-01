package main

import (
	"fmt"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/md"
	"github.com/gomarkdown/markdown/parser"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type ImageSyncer interface {
	BuildImageMap() error
	EnsureLocalImageIsUploaded(img LocalImage) (string, error)
	DownloadAndSaveImage(fullImageUrl string, postDatePart string, postSlug string) (string, error)
}

func IsImageFile(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".svg"
}

// EnsurePathIsRelativeToItsLocation Make sure that the `filePath` is a relative path that
// does not point outside its directory
func EnsurePathIsRelativeToItsLocation(filePath string, absOk bool) (string, error) {
	// TODO: DANGER, RADIOACTIVE, BAD.
	// This code might conceivably be used to read files outside of the current directory.
	// I _hope_ I sanitized the inputs well enough.
	if filePath == "" {
		return "", fmt.Errorf("empty path")
	}
	if path.IsAbs(filePath) {
		if absOk {
			return "", nil // Failed to sanitize the path, but it's OK
		}
		return "", fmt.Errorf("absolute path, look out for malicious input")
	}
	curPath := filePath
	for {
		dir, file := path.Split(curPath)
		if file == "." || file == ".." {
			return "", fmt.Errorf("path contains '..' or '.', look out for malicious input")
		}
		if dir == "" {
			break
		}
		curPath = file
	}

	return filePath, nil
}

func GatherPostImagesAndTitle(rootDir string, input []byte) ([]LocalImage, string, error) {
	extensions := parser.CommonExtensions
	mdParser := parser.NewWithExtensions(extensions)
	doc := mdParser.Parse(input)

	// Collect the local images
	var images []LocalImage
	var err error
	var title string
	ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
		// Extract the document title (the first first-level header)
		if head, ok := node.(*ast.Heading); ok && entering && title == "" {
			mdr := md.NewRenderer()
			title = string(markdown.Render(head, mdr))
			title = strings.TrimSpace(strings.TrimPrefix(title, "#"))
		}
		if img, ok := node.(*ast.Image); ok && entering {
			// Check if image is relative to the post
			rawDst := string(img.Destination)

			// Skip non-image file
			if !IsImageFile(rawDst) {
				return ast.GoToNext
			}

			// Skip absolute URLs (with the schema)
			var imgUrl *url.URL
			imgUrl, err = url.Parse(rawDst)
			if imgUrl.IsAbs() {
				return ast.GoToNext
			}
			dst := path.Clean(rawDst)

			var imgPath string
			// On reflection, we shouldn't allow using absolute paths to files in posts, as it might be a vector
			// for an attacker to read arbitrary files from the author's computer.
			imgPath, err = EnsurePathIsRelativeToItsLocation(dst, false)
			if err != nil {
				return ast.Terminate
			}
			if imgPath == "" {
				return ast.GoToNext
			}

			if !path.IsAbs(imgPath) {
				imgPath = path.Join(rootDir, imgPath)
			}

			st, statErr := os.Stat(imgPath)
			if statErr == nil {
				// Path exists!
				images = append(images, LocalImage{
					fullPath: imgPath,
					relPath:  rawDst,
					size:     st.Size(),
					mtime:    st.ModTime(),
				})
			}
		}
		return ast.GoToNext
	})
	if err != nil {
		return nil, "", err
	}

	return images, title, nil
}

func ParsePostAndDownloadReferencedImages(syncer ImageSyncer,
	postContent string, datePart, slug string) (map[string]string, error) {

	extensions := parser.CommonExtensions
	mdParser := parser.NewWithExtensions(extensions)
	doc := mdParser.Parse([]byte(postContent))

	// Download
	var err error
	linkFixMap := make(map[string]string)
	ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
		if img, ok := node.(*ast.Image); ok && entering {
			// Skip non-image file
			if !IsImageFile(string(img.Destination)) {
				return ast.GoToNext
			}

			var newDest string
			newDest, err = syncer.DownloadAndSaveImage(string(img.Destination), datePart, slug)
			if err != nil {
				return ast.Terminate
			}
			if newDest != "" {
				linkFixMap[string(img.Destination)] = newDest
			}
		}
		return ast.GoToNext
	})
	if err != nil {
		return nil, err
	}

	return linkFixMap, nil
}
