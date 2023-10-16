# WriteAs Sync

This is a bidirectional synchronizer for Write.As blogs, written in Go. It allows you to write your blog locally,
using your favorite text editor or a specialized tool like [Obsidian](https://obsidian.md/), and then sync your 
posts to your Write.As blog.

It also allows you to make changes in your Write.As blog, and then sync them back to your local copy. 

`writeas-sync` also integrates with Snap.As, and transparently syncs images with your Snap.As account.

# Installation

You can install `writeas-sync` using `go get`:

```shell
$ go install github.com/Cyberax/writeas-sync@latest
```

This will install the `writeas-sync` binary into your GOROOT:

```shell
$ ~/go/bin/writeas-sync

Synchronizer for your Markdown-based blog

Usage:
  writeas-sync [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  download    Pull remote changes to your local blog
  help        Help about any command
  sync        Synchronize your blog (upload and download)
  upload      Push your local changes to the remote blog

Flags:
  -a, --alias string              Write.as alias
  -h, --help                      help for writeas-sync
  -l, --login string              Write.as login
  -p, --password string           Write.as password (uses WRITEAS_PASS environment variable if not specified)
  -r, --root string               Root directory of your blog (default "/Users/cyberax/go/bin")
  -s, --snapas-endpoint string    Snap.as API endpoint (default "https://snap.as/api")
  -w, --writeas-endpoint string   Write.as API endpoint (default "https://write.as/api")

Use "writeas-sync [command] --help" for more information about a command.
```

Brew support and a Nix package are ComingSoon(tm).

# Initial setup

First, you'll need to create a Write.As account. Then, you'll need to create a blog and get its alias. Make sure to 
create at least one blog post there.

Then you need to do the initial download, which will create a local copy of your blog in `~/blog`:

```shell
$   export WRITEAS_PASS=<your password>
$ writeas-sync download --alias <your blog alias> --login <your login> --root ~/blog
2023/11/02 11:13:25 INFO Logging into Write.as
2023/11/02 11:13:26 INFO Building image map
2023/11/02 11:13:29 INFO Enumerating local posts rootDir=/Users/cyberax/blog
2023/11/02 11:13:29 INFO Found local posts num=0
2023/11/02 11:13:29 INFO Fetching the remote posts
2023/11/02 11:13:29 INFO Found remote posts num=5
2023/11/02 11:13:29 INFO Downloading new or changed remote posts
2023/11/02 11:13:29 INFO New remote post slug=testing-upload
2023/11/02 11:13:29 INFO Ensuring that the image path exists path=testing-upload
2023/11/02 11:13:29 INFO Downloading image url=https://i.snap.as/DFbKfX6U.jpeg dest=testing-upload/Bridge.jpeg
...
```

This will create a directory tree that looks like this:

```
2022-08-14-multitenancy-in-postgresql.md
2023-01-24-untangling-the-aws-ssm.md
2023-03-17-benchmarking-go-and-c-async-await.md
2023-10-08-obsidian-writeas
2023-10-08-obsidian-writeas.md
2023-10-09-testing-upload.md
testing-upload
```

# Writing new posts blog

`writeas-sync` requires blog filenames to conform to the following format: `YYYY-MM-DD-post-slug.md`. The date
part is used to ensure the correct sorting order. The `post-slug` part is used to generate the post URL. 
It is immutable, as all content URLs must forever be. So make sure to get it right on the first try :)

The first first-level caption of the post is used as the post title.

For example, you can create a simple post that references an image (of course, the image `minerals/obsidian.jpg` 
needs to be present):

```markdown
# This is an upload test

I like minerals. Here's Obsidian:

![Obsidian](minerals/obsidian.jpg)
```

Save the post as `2023-11-02-frist-post.md` and run the upload command:

```shell
$   export WRITEAS_PASS=<your password>
$ writeas-sync upload --alias <your blog alias> --login <your login> --root ~/blog
```

This will publish the post to your WriteAs blog, under the url: `https://write.as/<alias>/frist-post`. If you press
the `Edit` link on WriteAs, you'll see that the local image reference has been replaced with a link to SnapAs:

```markdown
![Obsidian](https://i.snap.as/qV2YQG45.jpg)
```

As a shortcut for download and upload, you can use the `sync` command that runs both:

```shell
$   export WRITEAS_PASS=<your password>
$ writeas-sync sync --alias <your blog alias> --login <your login> --root ~/blog
```

# Updating the posts and conflict resolution

You can edit your posts using the Write.As web interface and synchronize the changes back to your local copy. However,
keep in mind that `writeas-sync` doesn't support merging changes. So if you edit the same post both locally and on
Write.As, the one with the latest timestamp will win.

I suggest keeping your local posts inside a Git repository, so that you can easily revert the changes if something
ever goes wrong.

`writeas-sync` also doesn't support post deletion, so you need to make sure that you delete an unwanted post both
locally and remotely. Otherwise, it'll keep getting 'resurrected' with each sync.

# Notes on working with images

`writeas-sync` supports simple image management. When you write a post locally, you can reference images in 
subdirectories of your post directory. For example, if you have a post `2023-11-02-frist-post.md`, you can reference
an image `minerals/obsidian.jpg` in it.

When you upload the post, `writeas-sync` will upload the image to Snap.As and replace the local image reference with
a link to the uploaded image. The source directory will be preserved in the `filename` property of the image on SnapAs.

If you edit or create a post on Write.As, they will lack the `filename` property, so `writeas-sync` will download
them into the subdirectory named after the post slug (essentially, the filename without the `.md` suffix).

# Limitations and TODOs

1. The blog structure is very simple: it's just a list of posts, prefixed with a timestamp.  
2. The blog is stored in a single directory. If you have multiple blogs, you'll need to run multiple instances of the
   synchronizer.
3. Snap.As processes the images, seriously degrading their quality.
4. The WriteAs API doesn't support pagination, so if you have a large blog, synchronization is going to take 
   a lot of time. And it'll probably break at some point.
5. SnapAs integration doesn't support mutating images. So if you want to edit a local image, you need to change 
   its filename. 
6. TODO: add support for tags.
7. TODO: add support for collections in Snap.As.
8. TODO: add GC support for unreferenced images in Snap.As.
