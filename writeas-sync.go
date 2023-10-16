package main

import (
	"fmt"
	"github.com/snapas/go-snapas"
	"github.com/spf13/cobra"
	"github.com/writeas/go-writeas/v2"
	"log/slog"
	"os"
)

func doSync(conv *ImageConverter, ps *PostSynchronizer, doDownload, doUpload bool) error {
	slog.Default().Info("Building image map")
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
	conv *ImageConverter
	ps   *PostSynchronizer
}

type Settings struct {
	Alias         string
	RootDirectory string
	Login         string
	Password      string

	SnapAsEndpoint  string
	WriteAsEndpoint string
}

func initApp(sets *Settings) (*Application, error) {
	writeAsClient := writeas.NewClient()

	slog.Default().Info("Logging into Write.as")
	user, err := writeAsClient.LogIn(sets.Login, sets.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to login: %w", err)
	}

	writeAsClient.SetToken(user.AccessToken)
	snapAsClient := snapas.NewClient(user.AccessToken)

	conv := NewImageConverter(snapAsClient, sets.RootDirectory)
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
	})

	rootCmd.PersistentFlags().StringVarP(&setts.SnapAsEndpoint, "snapas-endpoint", "s",
		"https://snap.as/api", "Snap.as API endpoint")
	rootCmd.PersistentFlags().StringVarP(&setts.WriteAsEndpoint, "writeas-endpoint", "w",
		"https://write.as/api", "Write.as API endpoint")

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
