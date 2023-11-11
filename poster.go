package main

import (
	"github.com/djherbis/times"
	"github.com/writeas/go-writeas/v2"
	"log/slog"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

const AllowedFileTimestampSkew = 2 * time.Second

var ObsiSyncFilePattern = regexp.MustCompile(`\d\d\d\d-\d\d-\d\d-.*\.md`)

type LocalImage struct {
	fname   string
	relPath string
	size    int64
}

type LocalPost struct {
	fname          string
	datePart, slug string
	images         []LocalImage
	ctime, mtime   time.Time
	content        string
	title          string
}

type PostSynchronizer struct {
	conv      *ImageConverter
	client    *writeas.Client
	rootDir   string
	collAlias string

	posts map[string]LocalPost
}

func NewPostSynchronizer(conv *ImageConverter, client *writeas.Client, rootDir, collAlias string) *PostSynchronizer {

	return &PostSynchronizer{
		conv:      conv,
		client:    client,
		rootDir:   rootDir,
		collAlias: collAlias,
		posts:     make(map[string]LocalPost),
	}
}

func (p *PostSynchronizer) FindFiles() error {
	dir, err := os.ReadDir(p.rootDir)
	if err != nil {
		return err
	}
	slices.SortFunc(dir, func(a, b os.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	for _, d := range dir {
		fname := d.Name()
		if !ObsiSyncFilePattern.MatchString(fname) || d.IsDir() {
			continue
		}
		finfo, err := d.Info()
		if err != nil {
			return err
		}

		// Extract the slug
		parts := strings.SplitN(fname, "-", 4)
		datePart := strings.Join(parts[0:3], "-")
		slug := strings.TrimSuffix(parts[3], ".md")

		content, err := os.ReadFile(path.Join(p.rootDir, fname))
		if err != nil {
			return err
		}

		images, title, err := p.conv.GatherPostImagesAndTitle(content)
		if err != nil {
			return err
		}

		stat, err := times.Stat(path.Join(p.rootDir, fname))
		if err != nil {
			return err
		}

		lp := LocalPost{
			fname:    fname,
			datePart: datePart,
			slug:     slug,
			images:   images,
			mtime:    finfo.ModTime(),
			ctime:    stat.BirthTime(),
			content:  string(content),
			title:    title,
		}
		p.posts[slug] = lp
	}

	return nil
}

func (p *PostSynchronizer) UploadLocalImages() (map[string]string, error) {
	urlMap := make(map[string]string)

	for _, curPost := range p.posts {
		for _, i := range curPost.images {
			imgUrl, err := p.conv.EnsureLocalImageIsUploaded(i)
			if err != nil {
				return nil, err
			}
			urlMap[i.relPath] = imgUrl
		}
	}

	return urlMap, nil
}

func (p *PostSynchronizer) LoadRemotePosts() ([]writeas.Post, error) {
	var res []writeas.Post

	page := uint64(1)
	for {
		slog.Default().Info("Fetching a page", slog.Uint64("page", page))
		posts, err := ReqWithRetries[*[]writeas.Post](func() (*[]writeas.Post, error) {
			return GetCollectionPostsPaginated(p.client, p.collAlias, page)
		})
		if err != nil {
			return nil, err
		}
		if len(*posts) == 0 {
			break
		}

		res = append(res, *posts...)
		page++
	}
	return res, nil
}

func (p *PostSynchronizer) UpdateOrCreateLocalPosts(remotePosts []writeas.Post) error {
	for _, curPost := range remotePosts {
		// Find the local file?
		localPost, ok := p.posts[curPost.Slug]
		if ok {
			// Check the modification time
			timeDiff := localPost.mtime.Sub(curPost.Updated)
			if timeDiff < -AllowedFileTimestampSkew {
				// The file is substantially newer than the server's post
				slog.Default().Info("Post has been updated on the server, syncing locally",
					slog.String("slug", curPost.Slug))
				err := p.createOrUpdateLocalFile(curPost, &localPost)
				if err != nil {
					return err
				}
			}
		} else {
			slog.Default().Info("New remote post", slog.String("slug", curPost.Slug))
			err := p.createOrUpdateLocalFile(curPost, nil)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *PostSynchronizer) createOrUpdateLocalFile(post writeas.Post, local *LocalPost) error {
	datePart := post.Created.UTC().Format("2006-01-02")
	if local != nil {
		// Override the remote post's creation date, it might be different from the local one
		datePart = local.datePart
	}

	fname := path.Join(p.rootDir, datePart+"-"+post.Slug+".md")

	file, err := os.OpenFile(fname, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	linkFixMap, err := p.conv.DownloadPostImages(post.Content, datePart, post.Slug)
	if err != nil {
		return err
	}

	fixedContent := post.Content
	for oldLnk, newLnk := range linkFixMap {
		fixedContent = strings.ReplaceAll(fixedContent, "("+oldLnk+")", "("+newLnk+")")
	}

	// Fixup the final "discuss" link: <a href=\"....\">Discuss...</a>
	re := regexp.MustCompile(`\n\n<a href=".*">Discuss...</a> $`)
	fixedContent = string(re.ReplaceAll([]byte(fixedContent), []byte("")))

	// We excluded the title during the upload, re-add it
	if strings.TrimSpace(post.Title) != "" {
		fixedContent = "# " + post.Title + "\n" + fixedContent
	}

	_, err = file.WriteString(fixedContent)
	if err != nil {
		return err
	}

	err = os.Chtimes(fname, time.Time{}, post.Updated)
	if err != nil {
		return err
	}

	return nil
}

func (p *PostSynchronizer) UpdateOrCreateRemotePosts(remotePosts []writeas.Post,
	imageUrlMap map[string]string) error {

	remotes := make(map[string]writeas.Post)
	for _, post := range remotePosts {
		remotes[post.Slug] = post
	}

	for _, localPost := range p.posts {
		// Do we have the remote post?
		remote, ok := remotes[localPost.slug]
		if ok {
			timeDiff := localPost.mtime.Sub(remote.Updated)

			if timeDiff > AllowedFileTimestampSkew {
				slog.Default().Info("File has been updated locally, updating on the server",
					slog.String("slug", localPost.slug))
				err := p.uploadLocalPostToServer(localPost, &remote, imageUrlMap)
				if err != nil {
					return err
				}
			} else {
				slog.Default().Info("Up-to-date file", slog.String("slug", localPost.slug))
			}

		} else {
			slog.Default().Info("Uploading new local post", slog.String("slug", localPost.slug))
			err := p.uploadLocalPostToServer(localPost, nil, imageUrlMap)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *PostSynchronizer) uploadLocalPostToServer(local LocalPost, remote *writeas.Post,
	imageUrlMap map[string]string) error {

	// First, fixup the URLs
	content := local.content
	for relPath, imgUrl := range imageUrlMap {
		content = strings.ReplaceAll(content, "("+relPath+")", "("+imgUrl+")")
	}

	// Remove the title
	if local.title != "" {
		content = strings.Replace(content, "# "+local.title+"\n", "", 1)
	}

	if remote != nil {
		_, err := ReqWithRetries[*writeas.Post](func() (*writeas.Post, error) {
			return p.client.UpdatePost(remote.ID, "", &writeas.PostParams{
				ID:      remote.ID,
				Updated: &local.mtime,
				Content: content,
				Title:   local.title,
			})
		})
		if err != nil {
			return err
		}
	} else {

		// The timestamp is basically useless in the current WriteAs API, so we just use the slug part
		// for the date, to preserve the post order. We use the UTC date at noon.
		ctime, err := time.Parse("2006-01-02T15:04:05", local.datePart+"T12:00:00")
		if err != nil {
			return err
		}

		_, err = ReqWithRetries[*writeas.Post](func() (*writeas.Post, error) {
			return p.client.CreatePost(&writeas.PostParams{
				Collection: p.collAlias,
				Slug:       local.slug,
				Created:    &ctime,
				Content:    content,
				Title:      local.title,
			})
		})
		if err != nil {
			return err
		}

		//err = p.setPostCtime(*newPost, local.title, ctime)
		//if err != nil {
		//	return err
		//}
	}

	return nil
}
