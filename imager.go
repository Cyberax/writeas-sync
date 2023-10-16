package main

import (
	"fmt"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/md"
	"github.com/gomarkdown/markdown/parser"
	"github.com/snapas/go-snapas"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const SnapAsUrlPrefix = "https://i.snap.as/"

// DirectorySeparatorReplacement a Latin-1 Supplement "broken bar" character to escape the '/' character
const DirectorySeparatorReplacement = "¦"

// ObsidianSyncPrefix prefix for escaped paths ("not sign" from Latin-1 Supplement)
const ObsidianSyncPrefix = "¬"

type ImageConverter struct {
	rootDir                string
	client                 *snapas.Client
	imageMapByUrl          map[string]snapas.Photo
	imageMapByFilenameName map[string]snapas.Photo
}

func NewImageConverter(client *snapas.Client, rootDir string) *ImageConverter {
	return &ImageConverter{
		client:                 client,
		rootDir:                rootDir,
		imageMapByUrl:          make(map[string]snapas.Photo),
		imageMapByFilenameName: make(map[string]snapas.Photo),
	}
}

func IsImageFile(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".svg"
}

func (c *ImageConverter) BuildImageMap() error {
	var photos []snapas.Photo
	_, err := c.client.Get("/me/photos", &photos)
	if err != nil {
		return err
	}

	for _, p := range photos {
		// We can't depend on the size reported by the API
		// TODO: we don't need this for now, because we gave up on size validation
		//resp, err := http.Head(p.URL)
		//if err != nil {
		//	return err
		//}
		//p.Size = resp.ContentLength

		c.imageMapByUrl[p.URL] = p
		if len(p.Filename) > 0 {
			c.imageMapByFilenameName[p.Filename] = p
		}
	}

	return nil
}

func (c *ImageConverter) EnsureLocalImageIsUploaded(img LocalImage) (string, error) {
	// Check if the image is already present first by the filename
	escapedName := ObsidianSyncPrefix + strings.ReplaceAll(img.relPath, "/", DirectorySeparatorReplacement)
	cur, ok := c.imageMapByFilenameName[escapedName]
	if ok {
		return cur.URL, nil
		//TODO: size comparison doesn't work because snap.as does image reprocessing.
		//Leave this for now, until they have true as-is storage.
		//if cur.Size == img.size {
		//	return cur.URL, nil
		//}
		//return "", fmt.Errorf("file sizes differ for file %s, URL=%s, delete the file on the server to re-upload",
		//	img.fname, cur.URL)
	}

	// But the file could have been downloaded without a meaningful filename, so try to
	// use the filename as the URL
	cur, ok = c.imageMapByUrl[SnapAsUrlPrefix+path.Base(img.fname)]
	if ok {
		return cur.URL, nil
		//TODO: size comparison doesn't work because snap.as does image reprocessing.
		//Leave this for now, until they have true as-is storage.
		//if cur.Size == img.size {
		//	return cur.URL, nil
		//}
		//return "", fmt.Errorf("file sizes differ for file %s, URL=%s, delete the file on the server to re-upload",
		//	img.fname, cur.URL)
	}

	// Nope, image was not found so upload it
	slog.Default().Warn("Uploading a new image", slog.String("file", img.relPath))
	photo, err := UploadPhoto(c.client, img.fname, escapedName)
	if err != nil {
		return "", err
	}

	photo.Size = img.size // The API is broken, it reports incorrect file sizes
	c.imageMapByFilenameName[photo.Filename] = *photo
	c.imageMapByUrl[photo.URL] = *photo

	return photo.URL, nil
}

func IsSubPath(parent, sub string) (bool, error) {
	up := ".." + string(os.PathSeparator)

	// path-comparisons using filepath.Abs don't work reliably according to docs (no unique representation).
	rel, err := filepath.Rel(parent, sub)
	if err != nil {
		return false, err
	}
	if !strings.HasPrefix(rel, up) && rel != ".." {
		return true, nil
	}
	return false, nil
}

func (c *ImageConverter) GatherPostImagesAndTitle(input []byte) ([]LocalImage, string, error) {
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
			imgPath, err = c.sanitizePath(dst, true)
			if err != nil {
				return ast.Terminate
			}
			if imgPath == "" {
				return ast.GoToNext
			}

			if !path.IsAbs(imgPath) {
				imgPath = path.Join(c.rootDir, imgPath)
			}

			st, statErr := os.Stat(imgPath)
			if statErr == nil {
				// Path exists!
				images = append(images, LocalImage{
					fname:   imgPath,
					relPath: rawDst,
					size:    st.Size(),
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

// Sanitize the references to files, to make sure that they don't point out of the post tree
func (c *ImageConverter) sanitizePath(filePath string, absOk bool) (string, error) {
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

func (c *ImageConverter) DownloadPostImages(postContent string,
	datePart, slug string) (map[string]string, error) {

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
			newDest, err = c.ensureImageIsDownloaded(string(img.Destination), datePart, slug)
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

func (c *ImageConverter) ensureImageIsDownloaded(rawDst string, datePart string, slug string) (string, error) {
	// Check if image is relative to the post
	if !strings.HasPrefix(rawDst, SnapAsUrlPrefix) {
		return "", nil
	}

	imgUrl, err := url.Parse(rawDst)
	if err != nil {
		return "", err
	}

	existing, ok := c.imageMapByUrl[rawDst]
	// For images from other SnapAs accounts or for images that don't have an encoded path, we just
	// download them into the default location.
	if !ok || !strings.HasPrefix(existing.Filename, ObsidianSyncPrefix) {
		fileDir := datePart + "-" + slug
		err := os.MkdirAll(path.Join(c.rootDir, fileDir), 0755)
		if err != nil {
			return "", err
		}
		imgName := path.Base(imgUrl.Path)

		relPath := path.Join(fileDir, imgName)
		slog.Default().Info("Downloading image",
			slog.String("url", existing.URL), slog.String("dest", relPath))

		err = c.DownloadImage(path.Join(c.rootDir, fileDir, imgName), rawDst)
		return relPath, err
	}

	// This is our image, download it into a custom path
	imgPath := strings.ReplaceAll(strings.TrimPrefix(existing.Filename, ObsidianSyncPrefix),
		DirectorySeparatorReplacement, "/")

	sanitizedRelPath, err := c.sanitizePath(imgPath, false)
	if err != nil || sanitizedRelPath == "" {
		return "", fmt.Errorf("failed to sanitize the path: %w", err)
	}

	sanitizedAbsPath := path.Join(c.rootDir, sanitizedRelPath)

	// Just return the current path, if it exists
	if ok {
		_, err = os.Stat(sanitizedAbsPath)
		if err == nil {
			slog.Default().Info("The image already exists", slog.String("dest", sanitizedAbsPath))
			return sanitizedRelPath, nil
		}
	}

	sanitizedAbsDir := path.Dir(sanitizedAbsPath)
	if sanitizedAbsDir != "" {
		slog.Default().Info("Ensuring that the image directory exists", slog.String("dir", sanitizedAbsDir))
		err = os.MkdirAll(sanitizedAbsDir, 0755)
		if err != nil {
			return "", err
		}
	}

	slog.Default().Info("Downloading image",
		slog.String("url", existing.URL), slog.String("dest", sanitizedAbsPath))

	err = c.DownloadImage(sanitizedAbsPath, existing.URL)
	if err != nil {
		return "", err
	}

	return sanitizedRelPath, nil
}

func (c *ImageConverter) DownloadImage(dstFile string, url string) error {
	_, err := os.Stat(dstFile)
	if err == nil {
		// File already exists, nothing to do
		return nil
	}

	dirName, _ := path.Split(dstFile)
	err = os.MkdirAll(dirName, 0755)
	if err != nil {
		return err
	}

	out, err := os.Create(dstFile)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}
