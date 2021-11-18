package dev

import (
	"fmt"
	"path/filepath"
	"strings"
	goSync "sync"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/sidkik/kelda-v1/pkg/config"
	keldaClientset "github.com/sidkik/kelda-v1/pkg/crd/client/clientset/versioned"
	"github.com/sidkik/kelda-v1/pkg/errors"
	"github.com/sidkik/kelda-v1/pkg/fswatch"
	"github.com/sidkik/kelda-v1/pkg/proto/dev"
	"github.com/sidkik/kelda-v1/pkg/sync"
	syncClient "github.com/sidkik/kelda-v1/pkg/sync/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The interval to poll the filesystem for any changes that need to be synced.
const pollSeconds = 15

type syncer struct {
	namespace   string
	service     string
	servicePath string
	syncConfig  dev.SyncConfig

	fileWatcher chan struct{}
	msWatcher   watch.Interface

	keldaClient keldaClientset.Interface
	kubeClient  kubernetes.Interface
	restConfig  *rest.Config

	log *logrus.Logger
}

func newSyncer(log *logrus.Logger, keldaClient keldaClientset.Interface,
	kubeClient kubernetes.Interface, restConfig *rest.Config,
	namespace string, devCfg config.SyncConfig) (syncer, error) {

	syncConfig := devCfg.GetSyncConfigProto()
	servicePath := filepath.Dir(devCfg.GetPath())
	fileWatcher, err := fswatch.Watch(syncConfig, servicePath)
	if err != nil {
		rootCause := errors.RootCause(err)
		if dneErr, ok := rootCause.(errors.FileNotFound); ok {
			return syncer{}, errors.NewFriendlyError(
				"Failed to watch files for syncing.\n"+
					"%q doesn't exist.\n\n"+
					"Is the sync config in %q correct?",
				dneErr.Path, devCfg.GetPath())
		} else if strings.Contains(rootCause.Error(), "too many open files") {
			log.WithField("service", devCfg.Name).Warnf(
				"Too many files for Kelda to automatically watch for changes. "+
					"Kelda will poll for changes every %d seconds instead.", pollSeconds)
			log.Warn("See the docs at http://docs.kelda.io/common-issues/" +
				"#too-many-files-to-watch-for-changes for information on how " +
				"to increase the file watching limit.")

			// Disable the file watcher channel.
			fileWatcher = nil
		} else {
			return syncer{}, errors.WithContext(err, "watch files")
		}
	}

	opts := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", devCfg.Name),
	}
	msWatcher, err := keldaClient.KeldaV1alpha1().Microservices(namespace).Watch(opts)
	if err != nil {
		return syncer{}, errors.WithContext(err, "watch microservices")
	}

	return syncer{
		namespace:   namespace,
		service:     devCfg.Name,
		servicePath: servicePath,
		syncConfig:  syncConfig,
		fileWatcher: fileWatcher,
		msWatcher:   msWatcher,
		keldaClient: keldaClient,
		kubeClient:  kubeClient,
		restConfig:  restConfig,
		log:         log,
	}, nil
}

func combineEvents(in <-chan watch.Event) <-chan struct{} {
	combined := make(chan struct{}, 1)
	// Make sure the update is triggerred at least once.
	combined <- struct{}{}
	go func() {
		for range in {
			select {
			case combined <- struct{}{}:
			default:
			}
		}
	}()
	return combined
}

func (s syncer) Run() {
	hasSyncedOnce := false
	msWatcher := combineEvents(s.msWatcher.ResultChan())
	ticker := time.NewTicker(pollSeconds * time.Second)
	for {
		select {
		case <-s.fileWatcher:
		case <-msWatcher:
		case <-ticker.C:
		}

		ms, err := s.keldaClient.KeldaV1alpha1().Microservices(s.namespace).Get(
			s.service, metav1.GetOptions{})
		if err != nil {
			s.log.WithError(err).Error("Failed to get development microservice")
			continue
		}

		if ms.DevStatus.Pod == "" {
			continue
		}

		sc, err := syncClient.New(s.kubeClient, s.restConfig, s.namespace, ms.DevStatus.Pod)
		if err != nil {
			s.log.WithError(err).Error("Failed to get sync client")
			continue
		}

		if err := s.syncOnce(sc, hasSyncedOnce); err != nil {
			s.log.WithError(err).Error("Sync failed")
			continue
		}
		sc.Close()
		hasSyncedOnce = true
	}
}

type mirrorResult struct {
	localPath string
	err       error
}

var snapshotSource = sync.SnapshotSource

// syncOnce mirrors the local files tracked by the sync config into the remote pod.
// The `kelda dev-server` process running in the pod is then responsible for
// syncing the mirrored files to their final destination.
// See the package comment in the `pkg/sync` package for more information on
// the syncing algorithm.
func (s syncer) syncOnce(sc syncClient.Client, hasSyncedOnce bool) error {
	local, err := snapshotSource(s.syncConfig, s.servicePath)
	if err != nil {
		return errors.WithContext(err, "get local files")
	}

	version := sync.Version{s.syncConfig, local}
	if err := sc.SetTargetVersion(s.syncConfig, version.String()); err != nil {
		return errors.WithContext(err, "set target version")
	}

	mirror, err := sc.GetMirrorSnapshot()
	if err != nil {
		return errors.WithContext(err, "get mirror files")
	}

	toAdd, toRemove := local.Diff(mirror)
	if len(toAdd) == 0 && len(toRemove) == 0 {
		// If it's the first sync and we didn't have to do anything, let the
		// user know so that they don't think the sync is stalled.
		if !hasSyncedOnce {
			s.log.WithField("service", s.service).Info("Already synced. " +
				"A previous run of `kelda dev` probably synced the files over already.")
		}
		return nil
	}

	// Start the mirror workers.
	numWorkers := 8
	if len(toAdd) < numWorkers {
		numWorkers = len(toAdd)
	}

	var mirrorWaitGroup goSync.WaitGroup
	toMirrorChan := make(chan sync.SourceFile, numWorkers*2)
	mirrorResults := make(chan mirrorResult, numWorkers)
	for i := 0; i < numWorkers; i++ {
		mirrorWaitGroup.Add(1)
		go func() {
			defer mirrorWaitGroup.Done()
			for f := range toMirrorChan {
				mirrorResults <- mirrorResult{
					localPath: f.ContentsPath,
					err:       sc.Mirror(f),
				}
			}
		}()
	}

	// Feed the mirror workers.
	go func() {
		for _, f := range toAdd {
			toMirrorChan <- f
		}
		close(toMirrorChan)

		mirrorWaitGroup.Wait()
		close(mirrorResults)
	}()

	// Process the results from mirroring.
	var abortedSyncs int
	for res := range mirrorResults {
		if res.err != nil {
			if rootCause := errors.RootCause(res.err); rootCause == errors.ErrFileChanged {
				abortedSyncs++
				continue
			}
			return errors.WithContext(res.err, fmt.Sprintf("mirror %s", res.localPath))
		}
	}

	for _, f := range toRemove {
		if err := sc.Remove(f); err != nil {
			return errors.WithContext(err, "remove")
		}
	}

	s.log.WithField("service", s.service).Infof(
		"Copied %d files, removed %d.", len(toAdd)-abortedSyncs, len(toRemove))

	if err := sc.SyncComplete(); err != nil {
		return errors.WithContext(err, "notify sync complete")
	}
	return nil
}
