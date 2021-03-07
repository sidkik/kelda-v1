package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goSync "sync"
	"syscall"
	"time"

	"github.com/golang/protobuf/ptypes"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	"github.com/kelda-inc/kelda/cmd/util"
	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
	keldaClientset "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned"
	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/proto/dev"
	"github.com/kelda-inc/kelda/pkg/sync"

	_ "google.golang.org/grpc/encoding/gzip" // Install the gzip compressor
)

// Variables mocked for unit testing.
var (
	fs           = afero.NewOsFs()
	removeFile   = fs.Remove
	copyFile     = copyFileImpl
	startCommand = (*exec.Cmd).Start
	runCommand   = (*exec.Cmd).Run
	waitCommand  = (*exec.Cmd).Wait
	kill         = syscall.Kill
)

type server struct {
	keldaClient keldaClientset.Interface
	namespace   string
	service     string
	specVersion int

	devStatus        kelda.DevStatus
	devStatusChanged chan struct{}

	// needsInit represents whether the init command needs to be run the next
	// time the child process is restarted. This is tracked between calls to
	// manageChildOnce so that we retry the init command if it fails.
	needsInit           bool
	runningCmd          *exec.Cmd
	childManagerTrigger chan struct{}

	mirroredFiles  *sync.MirrorTracker
	syncedFiles    *sync.SyncedTracker
	syncConfigLock goSync.Mutex
	syncConfig     dev.SyncConfig

	restartChild func([]string, []string, bool) error
}

// Run starts the sync server and listens for connections.
func Run(keldaClient keldaClientset.Interface, namespace, service, pod string,
	specVersion int) error {

	lis, err := net.Listen("tcp", "0.0.0.0:9001")
	if err != nil {
		return errors.WithContext(err, "listen")
	}

	grpcServer := grpc.NewServer()
	serverImpl := &server{
		keldaClient:         keldaClient,
		namespace:           namespace,
		service:             service,
		specVersion:         specVersion,
		devStatus:           kelda.DevStatus{Pod: pod},
		childManagerTrigger: make(chan struct{}, 1),
		devStatusChanged:    make(chan struct{}, 1),
		syncedFiles:         sync.NewSyncedTracker(),
		mirroredFiles:       sync.NewMirrorTracker(),
	}
	serverImpl.restartChild = serverImpl.restartChildImpl
	dev.RegisterDevServer(grpcServer, serverImpl)

	go func() {
		defer util.HandlePanic()
		serverImpl.manageChildProcess()
	}()

	go func() {
		defer util.HandlePanic()
		serverImpl.syncDevStatus()
	}()

	log.Info("Kelda dev-server is ready")
	if err := grpcServer.Serve(lis); err != nil {
		return errors.WithContext(err, "serve")
	}
	return nil
}

func (s *server) syncDevStatus() {
	ticker := time.NewTicker(30 * time.Second).C
	for {
		if err := s.syncDevStatusOnce(); err != nil {
			log.WithError(err).Error("Failed to update running version. " +
				"Will retry in 30 seconds.")
		}

		select {
		case <-s.devStatusChanged:
		case <-ticker:
		}
	}
}

func (s *server) syncDevStatusOnce() error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		curr, err := s.keldaClient.KeldaV1alpha1().Microservices(s.namespace).Get(
			s.service, metav1.GetOptions{})
		if err != nil {
			return errors.WithContext(err, "get")
		}

		// The Microservice spec has changed, so the status update is obsolete.
		if curr.SpecVersion != s.specVersion {
			return nil
		}

		if curr.DevStatus == s.devStatus {
			// The status is unchanged -- no need to update.
			return nil
		}

		curr.DevStatus = s.devStatus
		_, err = s.keldaClient.KeldaV1alpha1().Microservices(s.namespace).Update(curr)
		return err
	})
}

func (s *server) SetTargetVersion(ctx context.Context, req *dev.SetTargetVersionRequest) (
	*dev.SetTargetVersionResponse, error) {

	s.syncConfigLock.Lock()
	defer s.syncConfigLock.Unlock()

	syncConfig := req.GetVersion().GetSyncConfig()
	if syncConfig == nil {
		return nil, errors.New("SyncConfig is required")
	}

	s.devStatus.TargetVersion = req.GetVersion().GetVersion()
	s.triggerStatusUpdate()

	oldSyncConfigVersion := sync.VersionSyncConfig(s.syncConfig)
	s.syncConfig = *syncConfig
	if oldSyncConfigVersion != sync.VersionSyncConfig(s.syncConfig) {
		s.triggerChildManager()
	}

	return &dev.SetTargetVersionResponse{}, nil
}

func (s *server) GetMirrorSnapshot(ctx context.Context, _ *dev.GetMirrorSnapshotRequest) (
	*dev.GetMirrorSnapshotResponse, error) {

	snapshot, err := s.mirroredFiles.GetSnapshot().Marshal()
	if err != nil {
		return &dev.GetMirrorSnapshotResponse{
			Error: errors.Marshal(errors.WithContext(err, "marshal mirror")),
		}, nil
	}
	return &dev.GetMirrorSnapshotResponse{Snapshot: &snapshot}, nil
}

func (s *server) Mirror(stream dev.Dev_MirrorServer) error {
	// Send any error in the final message of the stream rather than at the
	// protobuf transport level. This lets clients better handle errors.
	finish := func(err error) error {
		msg := &dev.MirrorFileResponse{Error: errors.Marshal(err)}
		if err := stream.SendAndClose(msg); err != nil {
			return errors.WithContext(err, "close stream")
		}
		return nil
	}

	// The first message in the stream contains metadata on what local file the
	// file chunks are for.
	headerMsg, err := stream.Recv()
	if err != nil {
		return errors.WithContext(err, "read header")
	}
	header := headerMsg.GetHeader()

	mirrorFile, err := ioutil.TempFile("", "kelda-sync")
	if err != nil {
		return finish(errors.WithContext(err, "open"))
	}
	defer mirrorFile.Close()

	modTime, err := ptypes.Timestamp(header.GetFileAttributes().GetModTime())
	if err != nil {
		return errors.WithContext(err, "parse modtime")
	}

	if err := mirrorFile.Chmod(os.FileMode(header.GetFileAttributes().GetMode())); err != nil {
		return finish(errors.WithContext(err, "set file mode"))
	}

	// The remaining messages contain the body of the file. Keep appending each
	// chunk to the staging file.
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return errors.WithContext(err, "read stream")
		}

		if _, err := io.Copy(mirrorFile, bytes.NewBuffer(chunk.Chunk)); err != nil {
			return finish(errors.WithContext(err, "write"))
		}
	}

	// Change the modification time as the last step so that it doesn't get
	// reset by other file operations.
	if err := os.Chtimes(mirrorFile.Name(), time.Now(), modTime); err != nil {
		return finish(errors.WithContext(err, "set file modtime"))
	}

	actualContentsHash, err := sync.HashFile(mirrorFile.Name())
	if err != nil {
		return finish(errors.WithContext(err, "hash synced file"))
	}

	if actualContentsHash != header.GetFileAttributes().GetContentsHash() {
		return finish(errors.ErrFileChanged)
	}

	s.mirroredFiles.Mirrored(sync.MirrorFile{
		ContentsPath:   mirrorFile.Name(),
		SyncSourcePath: header.GetSyncSourcePath(),
		FileAttributes: sync.FileAttributes{
			ContentsHash: header.GetFileAttributes().GetContentsHash(),
			ModTime:      modTime,
			Mode:         os.FileMode(header.GetFileAttributes().GetMode()),
		},
	})
	return finish(nil)
}

func (s *server) Remove(ctx context.Context, req *dev.RemoveFileRequest) (
	*dev.RemoveFileResponse, error) {

	s.mirroredFiles.Removed(req.Path)
	return &dev.RemoveFileResponse{}, nil
}

func (s *server) SyncComplete(ctx context.Context, _ *dev.SyncCompleteRequest) (
	*dev.SyncCompleteResponse, error) {

	s.triggerChildManager()
	return &dev.SyncCompleteResponse{}, nil
}

func (s *server) manageChildProcess() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.childManagerTrigger:
		case <-ticker.C:
		}
		s.syncConfigLock.Lock()
		syncConfigCopy := s.syncConfig
		s.syncConfigLock.Unlock()
		if err := s.manageChildOnce(syncConfigCopy); err != nil {
			log.WithError(err).Error("Failed to manage child process")
		}
	}
}

func (s *server) manageChildOnce(syncConfig dev.SyncConfig) error {
	toCopy, toRemove := s.syncedFiles.Diff(s.mirroredFiles, syncConfig)
	if len(toCopy) != 0 || len(toRemove) != 0 {
		// Kill any old child processes before syncing. This is necessary if
		// the child command executes a file that would be changed, since Linux
		// doesn't allow modifying a current executing file.
		log.Info("Killing old process before sync..")
		if err := s.killOldChild(); err != nil {
			return errors.WithContext(err, "kill old process")
		}
	}

	if err := s.executeSyncOps(toCopy, toRemove); err != nil {
		return err
	}

	// Calculate the version of the runtime directory now that we've synced.
	// Don't restart the process if we haven't done an initial sync yet, or if
	// the right version of the code is already running.
	version := s.syncedFiles.Version(syncConfig).String()
	if version == s.devStatus.RunningVersion || len(s.syncedFiles.Files()) == 0 {
		return nil
	}

	log.Info("Restarting due to change..")

	// Figure out if the sync triggered the init command.
	for _, f := range toCopy {
		for _, rule := range f.SyncRules {
			s.needsInit = s.needsInit || rule.TriggerInit
		}
	}

	if err := s.restartChild(syncConfig.GetOnSyncCommand(),
		syncConfig.GetOnInitCommand(), s.needsInit); err != nil {
		return errors.WithContext(err, "restart child")
	}

	s.needsInit = false
	s.devStatus.RunningVersion = version
	s.triggerStatusUpdate()
	return nil
}

func (s *server) triggerStatusUpdate() {
	select {
	case s.devStatusChanged <- struct{}{}:
	default:
	}
}

func (s *server) triggerChildManager() {
	select {
	case s.childManagerTrigger <- struct{}{}:
	default:
	}
}

// executeSyncOps executes the given file operations.
func (s *server) executeSyncOps(toCopy []sync.DestinationFile, toRemove []string) error {
	logFields := log.Fields{}
	if len(toCopy) > 0 {
		var syncedFileNames []string
		for _, f := range toCopy {
			mirrorFile, ok := s.mirroredFiles.Get(f.SyncSourcePath)
			if !ok {
				log.WithField("localPath", f.SyncSourcePath).Warn(
					"Mirrored file no longer exists. " +
						"It was most likely removed in a concurrent sync. " +
						"This error is most likely benign.")
				continue
			}

			if err := copyFile(mirrorFile.ContentsPath, f.SyncDestinationPath); err != nil {
				return errors.WithContext(err, "copy")
			}
			s.syncedFiles.Synced(f)
			syncedFileNames = append(syncedFileNames, f.SyncDestinationPath)
		}
		logFields["synced"] = truncateSlice(syncedFileNames, 5)
	}

	if len(toRemove) > 0 {
		for _, f := range toRemove {
			// If the remove fails because the files doesn't exist, then the
			// error is benign. The user's sync command most likely removed the
			// file before the user removed the file from their local machine.
			if err := removeFile(f); err != nil && !os.IsNotExist(err) {
				return errors.WithContext(err, "remove")
			}
			s.syncedFiles.Removed(f)
		}
		logFields["removed"] = truncateSlice(toRemove, 5)
	}

	if len(logFields) > 0 {
		log.WithFields(logFields).Info("Synced files..")
	}
	return nil
}

func copyFileImpl(src, dst string) error {
	dstParent := filepath.Dir(dst)
	dstParentExists, err := afero.DirExists(fs, dstParent)
	if err != nil {
		return errors.WithContext(err, "check if parent exists")
	}

	if !dstParentExists {
		if err := fs.MkdirAll(dstParent, 0755); err != nil {
			return errors.WithContext(err, "make parent")
		}
	}

	srcFile, err := fs.Open(src)
	if err != nil {
		return errors.WithContext(err, "open source")
	}
	defer srcFile.Close()

	fileInfo, err := srcFile.Stat()
	if err != nil {
		return errors.WithContext(err, "stat")
	}

	dstFile, err := fs.Create(dst)
	if err != nil {
		return errors.WithContext(err, "open destination")
	}
	defer dstFile.Close()

	if err := fs.Chmod(dst, fileInfo.Mode()); err != nil {
		return errors.WithContext(err, "set file mode")
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return errors.WithContext(err, "copy")
	}

	// Change the modification time as the last step so that it doesn't get
	// reset by other file operations.
	if err := fs.Chtimes(dst, time.Now(), fileInfo.ModTime()); err != nil {
		return errors.WithContext(err, "set file modtime")
	}
	return nil
}

func (s *server) restartChildImpl(syncCommand, initCommand []string, shouldInit bool) error {
	if len(syncCommand) == 0 || (shouldInit && len(initCommand) == 0) {
		return errors.New("unspecified command")
	}

	if err := s.killOldChild(); err != nil {
		return errors.WithContext(err, "kill old process")
	}

	if shouldInit {
		cmd := exec.Command(initCommand[0], initCommand[1:]...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := runCommand(cmd); err != nil {
			return errors.WithContext(err, "run init command")
		}
	}

	cmd := exec.Command(syncCommand[0], syncCommand[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := startCommand(cmd); err != nil {
		return errors.WithContext(err, "start process")
	}
	s.runningCmd = cmd

	return nil
}

// killOldChild kills the processes (if any) previously spawned by the dev-server.
func (s *server) killOldChild() error {
	if s.runningCmd != nil {
		err := kill(-s.runningCmd.Process.Pid, syscall.SIGKILL)
		// Ignore the error if the previous process crashed.
		if err != nil && err.Error() != "no such process" {
			return errors.WithContext(err, "kill old process")
		}

		// Block until the previous command exits.
		_ = waitCommand(s.runningCmd)
		s.runningCmd = nil
	}
	return nil
}

// truncateSlice truncates the given slice of strings to the given length. If
// the slice is longer than `length`, a message is appended saying how many
// more items are in the slice.
func truncateSlice(slc []string, length int) (truncated []string) {
	if len(slc) <= length {
		return slc
	}
	msg := fmt.Sprintf("... %d more ...", len(slc)-length)
	return append(slc[:length], msg)
}
