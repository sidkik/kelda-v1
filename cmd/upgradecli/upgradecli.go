package upgradecli

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"runtime"
	"syscall"

	goversion "github.com/hashicorp/go-version"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/sidkik/kelda-v1/cmd/util"
	"github.com/sidkik/kelda-v1/pkg/config"
	"github.com/sidkik/kelda-v1/pkg/errors"
	minionClient "github.com/sidkik/kelda-v1/pkg/minion/client"
	"github.com/sidkik/kelda-v1/pkg/version"
)

var (
	endpoint  = "https://update.kelda.io"
	fileParam = "kelda"
	osToParam = map[string]string{
		"darwin": "osx",
		"linux":  "linux",
	}
	fs = afero.NewOsFs()
)

// Token is the token used to download the release. It's set at compilation
// time so that CI can use a token that doesn't affect our analytics.
var Token string

// New creates a new `upgrade-cli` command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade-cli",
		Short: "Upgrade the local CLI binary to match the Minion's version",
		Long: "Upgrade the local Kelda CLI binary to match the minion's version." +
			"Also allows the CLI to be downgraded if the minion is " +
			"at a lower version.",
		Run: func(_ *cobra.Command, _ []string) {
			if err := run(); err != nil {
				util.HandleFatalError(err)
			}
		},
	}
}

func run() error {
	minionVersion, err := getMinionVersion()
	if err != nil {
		return errors.WithContext(err, "get minion version")
	}

	fmt.Printf("Your Kelda CLI is at version: %s\n", version.Version)
	fmt.Printf("Your cluster's Kelda minion is at version: %s\n\n", minionVersion.String())

	targetVersion, shouldInstall, err := promptShouldInstall(minionVersion)
	if err != nil {
		return errors.WithContext(err, "prompt")
	} else if !shouldInstall {
		return nil
	}

	pp := util.NewProgressPrinter(os.Stdout, fmt.Sprintf("Downloading Kelda release: %s", targetVersion.String()))
	go pp.Run()
	err = downloadKelda(targetVersion)
	pp.Stop()
	if err != nil {
		return errors.WithContext(err, "download kelda")
	}
	fmt.Println("Release successfully downloaded.")
	fmt.Println()

	installedPath, writableByUser, err := getInstalledPath()
	if err != nil {
		return errors.WithContext(err, "get installed path")
	}

	command := fmt.Sprintf("cp ./kelda %s", installedPath)
	if !writableByUser {
		command = "sudo " + command
	}

	fmt.Printf("Kelda has been downloaded to the current working directory.\n"+
		"Please execute the following command in your shell to install it:\n\n"+
		"\t %s \n\n", command)

	return nil
}

func getMinionVersion() (*goversion.Version, error) {
	userConfig, err := config.ParseUser()
	if err != nil {
		return nil, errors.WithContext(err, "parse user config")
	}

	kubeClient, restConfig, err := util.GetKubeClient(userConfig.Context)
	if err != nil {
		return nil, errors.WithContext(err, "get kube client")
	}

	mc, err := minionClient.New(kubeClient, restConfig)
	if err != nil {
		return nil, errors.WithContext(err, "connect to Kelda minion")
	}
	defer mc.Close()

	pp := util.NewProgressPrinter(os.Stdout, "Checking for updates to the Kelda "+
		"CLI.")
	go pp.Run()
	minionVersionStr, err := mc.GetVersion()
	if err != nil {
		return nil, errors.WithContext(err, "get updates")
	}
	pp.Stop()
	fmt.Println()

	minionVersion, err := goversion.NewVersion(minionVersionStr)
	if err != nil {
		return nil, errors.WithContext(err, "parse minion version")
	}

	return minionVersion, nil
}

func getInstalledPath() (string, bool, error) {
	path, err := os.Executable()
	if err != nil {
		return "", false, errors.WithContext(err, "get executable path")
	}

	// Resolve path with symlinks
	path, err = resolveLinks(path)
	if err != nil {
		return "", false, errors.WithContext(err, "resolve links")
	}

	isWritable, err := checkWritable(path)
	if err != nil {
		return "", false, errors.WithContext(err, "check permissions")
	}

	return path, isWritable, nil
}

// downloadKelda downloads the specified version of Kelda and stores the binary in
// the current working directory.
func downloadKelda(targetVersion *goversion.Version) error {
	osParam, ok := osToParam[runtime.GOOS]
	if !ok {
		return errors.New("invalid OS")
	}

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return errors.WithContext(err, "new request")
	}

	q := req.URL.Query()
	q.Add("release", targetVersion.String())
	q.Add("file", fileParam)
	q.Add("token", Token)
	q.Add("os", osParam)
	req.URL.RawQuery = q.Encode()

	resp, err := http.Get(req.URL.String())
	if err != nil {
		return errors.WithContext(err, "get")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server responded with %s", resp.Status)
	}

	ctype := resp.Header.Get("Content-Type")
	if !(ctype == "application/x-gzip" || ctype == "application/gzip") {
		return fmt.Errorf("incorrect content-type: %s", ctype)
	}

	err = extractRelease(resp.Body)
	if err != nil {
		return errors.WithContext(err, "extract file")
	}

	return nil
}

// extractRelease takes a .tar.gz Reader, and extracts the Kelda binary to the
// current working directory.
func extractRelease(src io.Reader) error {
	gzr, err := gzip.NewReader(src)
	if err != nil {
		return errors.WithContext(err, "new gzip reader")
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	// Search for a header for a file named "kelda" in the tar archive.
	var header *tar.Header
	for {
		header, err = tr.Next()

		switch {
		case err == io.EOF:
			return errors.WithContext(err, "find kelda in tar")
		case err != nil:
			return errors.WithContext(err, "read tar header")
		case header == nil:
			continue
		}

		if header.Typeflag == tar.TypeReg && header.Name == "kelda" {
			break
		}
	}

	dir, err := os.Getwd()
	if err != nil {
		return errors.WithContext(err, "get working dir")
	}
	dPath := path.Join(dir, "kelda")
	file, err := fs.OpenFile(dPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode))
	if err != nil {
		return errors.WithContext(err, "create path")
	}
	defer file.Close()

	_, err = io.Copy(file, tr)
	if err != nil {
		return errors.WithContext(err, "io copy")
	}

	return nil
}

func promptShouldInstall(remoteVersion *goversion.Version) (*goversion.Version, bool, error) {
	ownVersion, err := goversion.NewVersion(version.Version)
	if err != nil {
		return nil, false, errors.WithContext(err, "parse version")
	}

	if ownVersion.Equal(remoteVersion) {
		fmt.Println("Your CLI is already up to date with the minion.")
		return nil, false, nil
	}

	// Strip all metadata and prerelease information as we cannot upgrade to those
	// This also allows CLI to upgrade to stable if minion is at prerelease.
	segments := remoteVersion.Segments()
	targetVersion, _ := goversion.NewVersion(fmt.Sprintf("%d.%d.%d",
		segments[0], segments[1], segments[2]))

	var term string
	if ownVersion.LessThan(remoteVersion) {
		fmt.Println("Your CLI version is behind the minion version.")
		term = "upgrade"
	} else if ownVersion.GreaterThan(remoteVersion) {
		fmt.Println("Your CLI version is ahead of the minion version.")

		// This check is so developers (-dev-.*) aren't prompted to downgrade to
		// the exact same version if their minion is a prerelease version.
		if ownVersion.Equal(targetVersion) {
			fmt.Println("However, there is no update since you are on the stable release.")
			return nil, false, nil
		}

		fmt.Println("You may downgrade to the minion's version, but be warned that " +
			"downgrading below 0.12.0 will prevent you from upgrading via the CLI.")
		term = "downgrade"
	}

	doUpgrade, err := util.PromptYesOrNo(fmt.Sprintf("Would you like to "+term+
		" to release %s?", targetVersion.String()))
	if err != nil {
		return nil, false, errors.WithContext(err, "prompt")
	}

	return targetVersion, doUpgrade, nil
}

// resolveLinks takes a path and resolves symlinks up to a depth of 5.
func resolveLinks(path string) (string, error) {
	maxDepth := 5

	for i := 0; i < maxDepth; i++ {
		info, err := os.Lstat(path)
		if err != nil {
			return "", errors.WithContext(err, "get lstat")
		}

		if info.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}

		path, err = os.Readlink(path)
		if err != nil {
			return "", errors.WithContext(err, "follow link")
		}
	}

	return "", errors.New("maximum symlink traversal depth exceeded")
}

// checkWritable returns true if the user has write permissions to the file.
// This is Unix-only due to syscall dependency.
func checkWritable(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	uid := os.Getuid()
	uGids, err := os.Getgroups()
	if err != nil {
		return false, err
	}
	fStat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("couldn't get stat_t")
	}
	mode := fi.Mode()

	writable := isWritable(mode, fStat, uid, uGids)
	return writable, nil
}

func isWritable(fMode os.FileMode, fStat *syscall.Stat_t, uid int, uGids []int) bool {
	// Check if user owns the file (uids are equal) and has write permission
	// The permissions check is done by bit-shifting a `1` to the correct
	// position in `rwxrwxrwx` and performing an AND.
	if fStat.Uid == uint32(uid) {
		return fMode&(1<<7) != 0
	}

	// Check if group has write permissions and user is in group.
	fileGID := fStat.Gid
	for _, gid := range uGids {
		if uint32(gid) == fileGID {
			return fMode&(1<<4) != 0
		}
	}

	// Check if all others have write permissions.
	return fMode&(1<<1) != 0
}
