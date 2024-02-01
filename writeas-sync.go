package main

import (
	"fmt"
	"github.com/snapas/go-snapas"
	"github.com/spf13/cobra"
	"github.com/studio-b12/gowebdav"
	"github.com/writeas/go-writeas/v2"
	"log/slog"
	"os"
)

func doSync(conv ImageSyncer, ps *PostSynchronizer, doDownload, doUpload bool) error {
	slog.Default().Info("Retrieving remote image names")
	err := conv.BuildImageMap()
	if err != nil {
		return err
	}

	slog.Default().Info("Enumerating local posts", slog.String("rootDir", ps.rootDir))
	err = ps.FindFiles()
	if err != nil {
		return err
	}
	slog.Default().Info("Found local posts", slog.Int("num", len(ps.posts)))

	slog.Default().Info("Fetching the remote posts")
	remotePosts, err := ps.LoadRemotePosts()
	if err != nil {
		return err
	}
	slog.Default().Info("Found remote posts", slog.Int("num", len(remotePosts)))

	if doDownload {
		slog.Default().Info("Downloading new or changed remote posts")
		err = ps.UpdateOrCreateLocalPosts(remotePosts)
		if err != nil {
			return err
		}
	}

	if doUpload {
		slog.Default().Info("Uploading new or changed images")
		imageMap, err := ps.UploadLocalImages()
		if err != nil {
			return err
		}

		slog.Default().Info("Uploading new or changed local posts")
		err = ps.UpdateOrCreateRemotePosts(remotePosts, imageMap)
		if err != nil {
			return err
		}
	}

	return nil
}

type Application struct {
	conv ImageSyncer
	ps   *PostSynchronizer
}

type Settings struct {
	Alias         string
	RootDirectory string
	Login         string
	Password      string

	ImageHostingType string
	ImageLogin       string
	ImagePassword    string

	WebDavEndpoint string
	WebDavImageUrl string

	SnapAsEndpoint  string
	WriteAsEndpoint string
}

func initApp(sets *Settings) (*Application, error) {
	writeAsClient := writeas.NewClientWith(writeas.Config{
		URL: sets.WriteAsEndpoint,
	})

	slog.Default().Info("Logging into Write.as")
	user, err := writeAsClient.LogIn(sets.Login, sets.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to login: %w", err)
	}

	writeAsClient.SetToken(user.AccessToken)
	snapAsClient := snapas.NewClient(user.AccessToken)

	var conv ImageSyncer

	if sets.ImageHostingType == "webdav" {
		client := gowebdav.NewClient(sets.WebDavEndpoint, sets.ImageLogin, sets.ImagePassword)
		err = client.Connect()
		if err != nil {
			return nil, fmt.Errorf("failed to connect to WebDAV: %w", err)
		}
		conv = NewWebDAVSync(client, sets.RootDirectory, sets.WebDavImageUrl)
	} else if sets.ImageHostingType == "snapas" {
		conv = NewSnapasSync(snapAsClient, sets.RootDirectory)
	} else {
		panic("invalid image hosting type")
	}

	ps := NewPostSynchronizer(conv, writeAsClient, sets.RootDirectory, sets.Alias)

	return &Application{
		conv: conv,
		ps:   ps,
	}, nil
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "writeas-sync",
		Short: "writeas-sync is a bidirectional synchronizer for your blog",
		Long:  `Synchronizer for your Markdown-based blog`,
	}

	setts := &Settings{}

	wd, _ := os.Getwd()
	if wd == "" {
		wd = "."
	}

	rootCmd.PersistentFlags().StringVarP(&setts.Alias, "alias", "a",
		os.Getenv("WRITEAS_ALIAS"), "Write.as alias")
	rootCmd.PersistentFlags().StringVarP(&setts.RootDirectory, "root", "r",
		wd, "Root directory of your blog")

	rootCmd.PersistentFlags().StringVarP(&setts.Login, "login", "l", os.Getenv("WRITEAS_LOGIN"),
		"Write.as login")
	rootCmd.PersistentFlags().StringVarP(&setts.Password, "password", "p", "",
		"Write.as password (uses WRITEAS_PASS environment variable if not specified)")
	cobra.OnInitialize(func() {
		if setts.Password == "" {
			setts.Password = os.Getenv("WRITEAS_PASS")
		}
		if setts.ImageLogin == "" {
			setts.ImageLogin = setts.Login
		}
		if setts.ImagePassword == "" {
			setts.ImagePassword = setts.Password
		}
	})

	rootCmd.PersistentFlags().StringVarP(&setts.ImageHostingType, "image-hosting-type", "t", "",
		"Image hosting type: snapas (default), webdav")

	rootCmd.PersistentFlags().StringVarP(&setts.ImageLogin, "image-login", "i", "",
		"Image hosting login, the same as WriteAs login if not specified")
	rootCmd.PersistentFlags().StringVarP(&setts.ImagePassword, "image-password", "v", "",
		"Image hosting password, the same as WriteAs password if not specified")

	rootCmd.PersistentFlags().StringVarP(&setts.WebDavEndpoint, "webdav-endpoint", "",
		os.Getenv("WRITEAS_WEBDAV_URL"), "WebDAV endpoint URL")
	rootCmd.PersistentFlags().StringVarP(&setts.WebDavImageUrl, "webdav-published-url", "",
		os.Getenv("WRITEAS_WEBDAV_PUBLISHED_URL"), "URL for publicly accessible WebDAV images")

	rootCmd.PersistentFlags().StringVarP(&setts.SnapAsEndpoint, "snapas-endpoint", "s",
		"https://snap.as/api", "Snap.as API endpoint")
	rootCmd.PersistentFlags().StringVarP(&setts.WriteAsEndpoint, "writeas-endpoint", "w",
		"https://write.as/api", "Write.as API endpoint")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if setts.ImageHostingType != "webdav" && setts.ImageHostingType != "snapas" {
			return fmt.Errorf("invalid image hosting type: %s", setts.ImageHostingType)
		}
		return nil
	}

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Synchronize your blog (upload and download)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp(setts)
			if err != nil {
				return err
			}
			return doSync(app.conv, app.ps, true, true)
		},
	}

	uploadCmd := &cobra.Command{
		Use:   "upload",
		Short: "Push your local changes to the remote blog",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp(setts)
			if err != nil {
				return err
			}
			return doSync(app.conv, app.ps, false, true)
		},
	}

	downloadCmd := &cobra.Command{
		Use:   "download",
		Short: "Pull remote changes to your local blog",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := initApp(setts)
			if err != nil {
				return err
			}
			return doSync(app.conv, app.ps, true, false)
		},
	}

	rootCmd.AddCommand(syncCmd, uploadCmd, downloadCmd)

	err := rootCmd.Execute()
	if err != nil {
		slog.Default().Error("Command failed", "error", err)
		os.Exit(1)
	}
}
