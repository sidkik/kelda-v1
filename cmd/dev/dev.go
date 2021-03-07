package dev

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kelda-inc/kelda/cmd/util"
	"github.com/kelda-inc/kelda/pkg/config"
	keldaClientset "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned"
	"github.com/kelda-inc/kelda/pkg/crd/controller/tunnel"
	"github.com/kelda-inc/kelda/pkg/errors"
	minionClient "github.com/kelda-inc/kelda/pkg/minion/client"
	"github.com/kelda-inc/kelda/pkg/proto/messages"
	"github.com/kelda-inc/kelda/pkg/proto/minion"
	syncClient "github.com/kelda-inc/kelda/pkg/sync/client"
	"github.com/kelda-inc/kelda/pkg/version"
)

type devCmd struct {
	devConfigs []config.SyncConfig

	namespace       string
	workspaceConfig config.Workspace
	kubeClient      kubernetes.Interface
	keldaClient     keldaClientset.Interface
	restConfig      *rest.Config
	minionClient    minionClient.Client
	gui             keldaGUI
}

// chanWriter provides an io.Writer interface for writing to a channel.
type chanWriter chan []byte

func (w chanWriter) Write(p []byte) (int, error) {
	cpy := make([]byte, len(p))
	copy(cpy, p)
	w <- cpy
	return len(p), nil
}

// New creates a new `dev` command.
func New() *cobra.Command {
	var cmd devCmd
	var disableGUI bool
	var noSync bool
	var demo bool
	cobraCmd := &cobra.Command{
		Use:   "dev [path_to_service] ...",
		Short: "Start development on one or more services",
		Long: `Deploy the services in the specified directories to the remote cluster.
"dev" also boots all application dependencies.

If no service paths are provided, "dev" boots the service in the current directory.`,
		Run: func(_ *cobra.Command, args []string) {
			if demo {
				servicePath, err := setupDemo()
				if err != nil {
					util.HandleFatalError(err)
				}

				fmt.Println("Setup complete!")
				fmt.Printf("Starting development on the `web-server` service at %s.\n", servicePath)
				fmt.Printf("Hit [Enter] to continue.")
				bufio.NewReader(os.Stdin).ReadBytes('\n')

				args = []string{servicePath}
			}

			userConfig, err := config.ParseUser()
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "parse user config"))
			}

			if disableGUI {
				cmd.gui = noOutputGUI{}
			} else {
				cmd.gui = newKeldaGUI()
			}
			syncLogger := cmd.gui.GetLogger()

			if userConfig.Context == config.KeldaDemoContext ||
				userConfig.Context == demoContext {
				syncLogger.Info("Running against the public demo cluster. This cluster " +
					"is insecure and should only be used for public code.")
				syncLogger.Info("Development environments are deleted hourly.")
				syncLogger.Infof("Starting sync for: %s", args)
			}

			if userConfig.Workspace == "" {
				err := errors.NewFriendlyError("Workspace field is required in %s. "+
					"Run `kelda config` in the workspace directory to fix.",
					config.UserConfigPath)
				util.HandleFatalError(err)
			}

			cmd.namespace = userConfig.Namespace
			cmd.workspaceConfig, err = config.ParseWorkspace(syncLogger, userConfig.Workspace, userConfig.Namespace)
			if err != nil {
				cause := errors.RootCause(err)
				switch cause := cause.(type) {
				case errors.FileNotFound:
					err = errors.NewFriendlyError("Workspace config not found at %q. "+
						"Try running `kelda config` in the workspace directory to "+
						"update the workspace path.", cause.Path)
				default:
					err = errors.WithContext(err, "parse workspace config")
				}
				util.HandleFatalError(err)
			}

			logrus.SetFormatter(&logrus.TextFormatter{
				// Show the full timestamp rather than the time elapsed since the kelda
				// started. This makes correlating logs between the client and minion
				// easier.
				FullTimestamp: true,

				// Disable colors since we'll be logging to a file.
				DisableColors: true,
			})

			logPath := filepath.Join(filepath.Dir(userConfig.Workspace), "kelda.log")
			logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "open log file"))
			}
			defer logFile.Close()
			logrus.SetOutput(logFile)

			// Log unhandled errors originating from the Kubernetes codebase to
			// our log file, rather than through glog. The error handler is
			// invoked when runtime.HandleError is invoked -- for example, when
			// portforwarding has transient errors.
			runtime.ErrorHandlers = []func(error){
				func(err error) {
					// noLogPatterns is a list of Kubernetes error patterns that
					// we will not log.
					var noLogPatterns = []*regexp.Regexp{
						// This error occurs if nothing is listening in the remote
						// container on this port. For example, if the user tries
						// to use the tunnel before the microservice is listening.
						// Another case is if the tunnel is set up to the wrong
						// port. In both cases, the error already appears in the
						// browser and may alarm users if duplicated in the logs.
						regexp.MustCompile("^an error occurred forwarding [0-9]+ -> [0-9]+:(.*)Connection refused"),
					}
					for _, pattern := range noLogPatterns {
						if pattern.Match([]byte(err.Error())) {
							logrus.Debug(err)
							return
						}
					}

					logrus.WithError(err).Error("Generic Kubernetes error")
				},
			}

			cmd.kubeClient, cmd.restConfig, err = util.GetKubeClient(userConfig.Context)
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "get kube client"))
			}

			cmd.keldaClient, err = keldaClientset.NewForConfig(cmd.restConfig)
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "get kelda client"))
			}

			cmd.minionClient, err = minionClient.New(cmd.kubeClient, cmd.restConfig)
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "connect to Kelda server"))
			}
			defer cmd.minionClient.Close()

			remoteVersion, err := cmd.minionClient.GetVersion()
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "get remote version"))
			}

			if !compatibleVersions(remoteVersion, version.Version) {
				err = errors.NewFriendlyError("Incompatible version of Kelda detected.\n" +
					"Please upgrade the Kelda CLI to match the version of the Kelda " +
					"minion running in the cluster with `kelda upgrade-cli`.")
				util.HandleFatalError(err)
			}

			if !noSync {
				cmd.devConfigs, err = getSyncConfigs(cmd.workspaceConfig, args)
				if err != nil {
					cause := errors.RootCause(err)
					switch cause := cause.(type) {
					case errors.FileNotFound:
						err = errors.NewFriendlyError("Service config (%s) not found. "+
							"Are you in the right directory?", cause.Path)
					default:
						err = errors.WithContext(err, "parse dev services")
					}
					util.HandleFatalError(err)
				}
			}

			if err := cmd.run(); err != nil {
				util.HandleFatalError(err)
			}
		},
	}
	cobraCmd.Flags().BoolVar(&demo, "demo", false,
		"Run the Kelda demo. This downloads the example application, and runs it "+
			"on the Hosted Kelda demo cluster.")
	cobraCmd.Flags().BoolVar(&disableGUI, "no-gui", false,
		"Disable the GUI. Only used by integration testing")
	cobraCmd.Flags().BoolVar(&noSync, "no-sync", false,
		"Don't sync the development service.")
	return cobraCmd
}

func compatibleVersions(remoteVersion, localVersion string) bool {
	remoteVersionSplit := strings.Split(remoteVersion, ".")
	localVersionSplit := strings.Split(localVersion, ".")
	if len(remoteVersionSplit) != 3 || len(localVersionSplit) != 3 {
		// Only compare versions if they're of the form X.Y.Z. Official
		// releases always follow this format, but our internal builds don't.
		return true
	}
	return remoteVersionSplit[0] == localVersionSplit[0] && remoteVersionSplit[1] == localVersionSplit[1]
}

func getSyncConfigs(ws config.Workspace, paths []string) ([]config.SyncConfig, error) {
	// If no paths are specified, look in the current directory.
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var configs []config.SyncConfig
	for _, path := range paths {
		cfg, err := config.ParseSyncConfig(path)
		if err != nil {
			return nil, err
		}

		svc, ok := ws.GetService(cfg.Name)
		if !ok {
			serviceList := getServiceNames(ws.Services)
			sort.Strings(serviceList)
			return nil, errors.NewFriendlyError("Cannot start development on service %q "+
				"since it doesn't exist in the Workspace configuration.\n\n"+
				"You may need to update the name field in %q to be one of [%s].",
				cfg.Name, filepath.Join(path, "kelda.yaml"), strings.Join(serviceList, ", "))
		}

		// If the service doesn't have a dev command set, try to guess it from
		// the Kubernetes YAML.
		if len(cfg.Command) == 0 {
			cfg.Command, err = svc.GetDevCommand()
			if err != nil {
				return nil, errors.WithContext(err, "guess dev command")
			}
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

func (dc devCmd) run() error {
	// Set up the remote workspace.
	msgs, err := dc.setupWorkspace()
	if err != nil {
		return err
	}

	syncLogger := dc.gui.GetLogger()

	// Propagate any messages we got from the minion to the client.
	for _, msg := range msgs {
		switch msg.GetType() {
		case messages.Message_WARNING:
			syncLogger.Warn(msg.GetText())
		default:
			syncLogger.Info(msg.GetText())
		}
	}

	defer syncClient.CloseAllSyncTunnels()

	if err := dc.startFileSync(syncLogger); err != nil {
		return err
	}

	go func() {
		defer util.HandlePanic()
		tunnel.Run(dc.kubeClient, dc.keldaClient, dc.restConfig, dc.namespace)
	}()
	return dc.gui.Run(dc.keldaClient, dc.namespace)
}

func (dc devCmd) setupWorkspace() ([]*messages.Message, error) {
	keldaArt := " _          _      _      \n" +
		"| |        | |    | |       \n" +
		"| | __ ___ | |  __| |  __ _ \n" +
		"| |/ // _ \\| | / _` | / _` |\n" +
		"|   <|  __/| || (_| || (_| |\n" +
		"|_|\\_\\____||_| \\__,_| \\__,_|\n"
	pp := util.NewProgressPrinter(os.Stdout, keldaArt+"Initializing development environment..")
	go pp.Run()

	// This escape sequence clears 7 lines of text.
	defer pp.StopWithPrint("\033[2K\033[1A\033[2K\033[1A\033[2K" +
		"\033[1A\033[2K\033[1A\033[2K\033[1A\033[2K\033[1A\033[2K\r")

	workspace := minion.Workspace{Namespace: dc.namespace}
	for _, service := range dc.workspaceConfig.Services {
		pbService := minion.Service{
			Name:      service.Name,
			Manifests: service.Manifests,
		}

		for _, cfg := range dc.devConfigs {
			if service.Name == cfg.Name {
				pbService.DevMode = true
				pbService.DevImage = cfg.Image
			}
		}
		workspace.Services = append(workspace.Services, &pbService)
	}

	for _, tunnel := range dc.workspaceConfig.Tunnels {
		workspace.Tunnels = append(workspace.Tunnels, &minion.Tunnel{
			ServiceName: tunnel.ServiceName,
			LocalPort:   tunnel.LocalPort,
			RemotePort:  tunnel.RemotePort,
		})
	}

	msgs, err := dc.minionClient.CreateWorkspace(workspace)
	if err != nil {
		return msgs, errors.WithContext(err, "create workspace")
	}
	return msgs, err
}

func (dc devCmd) startFileSync(syncLogger *logrus.Logger) error {
	if err := setOpenFilesLimit(); err != nil {
		syncLogger.WithError(err).Warn("Failed to increase the kernel limit on open files. " +
			"File syncing speed may be impacted.")
	}

	for _, cfg := range dc.devConfigs {
		syncer, err := newSyncer(syncLogger, dc.keldaClient, dc.kubeClient,
			dc.restConfig, dc.namespace, cfg)
		if err != nil {
			return errors.WithContext(err, "create file syncer")
		}
		go func() {
			defer util.HandlePanic()
			syncer.Run()
		}()
	}

	return nil
}

// The max file limit is 10240, even though the max returned by Getrlimit is
// 1<<63-1. This is OPEN_MAX in sys/syslimits.h.
const osxMaxSoftOpenFilesLimit = 10240

func setOpenFilesLimit() error {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return errors.WithContext(err, "get current limit")
	}

	if rLimit.Max < osxMaxSoftOpenFilesLimit {
		rLimit.Cur = rLimit.Max
	} else {
		rLimit.Cur = osxMaxSoftOpenFilesLimit
	}
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
}

func getServiceNames(services []config.Service) (names []string) {
	for _, svc := range services {
		names = append(names, svc.Name)
	}
	return names
}
