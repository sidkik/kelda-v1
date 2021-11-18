package dev

import (
	"sort"
	"time"

	"github.com/buger/goterm"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	kelda "github.com/sidkik/kelda-v1/pkg/crd/apis/kelda/v1alpha1"
	keldaClientset "github.com/sidkik/kelda-v1/pkg/crd/client/clientset/versioned"
	informerfactory "github.com/sidkik/kelda-v1/pkg/crd/client/informers/externalversions"
)

// registerInformerHandler adds an event handler to the informer, which simply
// calls updateFunc for addition, deletion, and updates.
func registerInformerHandler(informer cache.SharedIndexInformer,
	updateFunc func()) {

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(_ interface{}) {
			updateFunc()
		},
		DeleteFunc: func(_ interface{}) {
			updateFunc()
		},
		UpdateFunc: func(_, _ interface{}) {
			updateFunc()
		},
	})
}

// newTunnelWatcher returns a channel that updates whenever the status of the
// tunnels in the cluster change.
func newTunnelWatcher(keldaClient keldaClientset.Interface,
	namespace string) chan map[kelda.TunnelSpec]kelda.TunnelStatus {

	updates := make(chan map[kelda.TunnelSpec]kelda.TunnelStatus, 16)
	factory := informerfactory.NewSharedInformerFactoryWithOptions(
		keldaClient, 30*time.Second, informerfactory.WithNamespace(namespace))
	informer := factory.Kelda().V1alpha1().Tunnels().Informer()
	lister := factory.Kelda().V1alpha1().Tunnels().Lister()
	sendUpdate := func() {
		tunnels, err := lister.List(labels.Everything())
		if err != nil {
			log.WithError(err).Error("Failed to list tunnels")
			return
		}

		tunnelsMap := map[kelda.TunnelSpec]kelda.TunnelStatus{}
		for _, tunnel := range tunnels {
			tunnelsMap[tunnel.Spec] = tunnel.Status
		}
		updates <- tunnelsMap
	}

	registerInformerHandler(informer, sendUpdate)
	go informer.Run(nil)

	if !cache.WaitForCacheSync(nil, informer.HasSynced) {
		return updates
	}
	return updates
}

// newMicroserviceWatcher returns two channels that updates whenever the status
// of the microservices in the cluster change.
func newMicroserviceWatcher(keldaClient keldaClientset.Interface,
	namespace string) (chan map[string]statusString, chan map[string]statusString) {

	serviceChan := make(chan map[string]statusString, 16)
	jobChan := make(chan map[string]statusString, 16)
	factory := informerfactory.NewSharedInformerFactoryWithOptions(
		keldaClient, 30*time.Second, informerfactory.WithNamespace(namespace))
	informer := factory.Kelda().V1alpha1().Microservices().Informer()
	lister := factory.Kelda().V1alpha1().Microservices().Lister()
	sendUpdate := func() {
		microservices, err := lister.List(labels.Everything())
		if err != nil {
			log.WithError(err).Error("Failed to list microservices")
			return
		}

		serviceMap := map[string]statusString{}
		jobMap := map[string]statusString{}

		for _, ms := range microservices {
			if ms.Spec.HasService {
				status := serviceStatusString(ms.Status.ServiceStatus)
				if ms.Status.MetaStatus.Phase != "" {
					status = metaStatusString(ms.Status.MetaStatus)
				}
				status.isDev = ms.Spec.DevMode
				status.name = ms.Name
				serviceMap[ms.Name] = status
			}
			if ms.Spec.HasJob {
				status := jobStatusString(ms.Status.JobStatus)
				if ms.Status.MetaStatus.Phase != "" {
					status = metaStatusString(ms.Status.MetaStatus)
				}
				status.name = ms.Name
				jobMap[ms.Name] = status
			}
		}
		serviceChan <- serviceMap
		jobChan <- jobMap
	}

	registerInformerHandler(informer, sendUpdate)
	go informer.Run(nil)

	cache.WaitForCacheSync(nil, informer.HasSynced)
	return serviceChan, jobChan
}

type statusString struct {
	color int
	phase string
	msg   string

	isDev bool
	name  string
}

func (ss statusString) String() string {
	msg := ss.phase
	if ss.msg != "" {
		msg += ": " + ss.msg
	}
	return goterm.Color(msg, ss.color)
}

func serviceStatusString(status kelda.ServiceStatus) statusString {
	ss := statusString{
		phase: string(status.Phase),
		msg:   status.Message,
		color: goterm.BLACK,
	}
	switch status.Phase {
	case kelda.ServiceFailed:
		ss.color = goterm.RED
	case kelda.ServiceStarting:
		ss.color = goterm.YELLOW
	case kelda.ServiceReady:
		ss.color = goterm.GREEN
	case kelda.ServiceNotReady:
		ss.color = goterm.YELLOW
	case kelda.ServiceUnknown:
		ss.color = goterm.BLACK
	}

	return ss
}

func jobStatusString(status kelda.JobStatus) statusString {
	ss := statusString{
		phase: string(status.Phase),
		msg:   status.Message,
		color: goterm.BLACK,
	}
	switch status.Phase {
	case kelda.JobFailed:
		ss.color = goterm.RED
	case kelda.JobStarting:
		ss.color = goterm.YELLOW
	case kelda.JobRunning:
		ss.color = goterm.YELLOW
	case kelda.JobCompleted:
		ss.color = goterm.GREEN
	case kelda.JobUnknown:
		ss.color = goterm.BLACK
	}

	return ss
}

func metaStatusString(status kelda.MetaStatus) statusString {
	ss := statusString{
		phase: string(status.Phase),
		msg:   status.Message,
		color: goterm.BLACK,
	}
	switch status.Phase {
	case kelda.MetaSyncing:
		ss.color = goterm.YELLOW
	case kelda.MetaSynced:
		ss.color = goterm.GREEN
	case kelda.MetaStatusSyncFailed:
		ss.color = goterm.RED
	case kelda.MetaDeployFailed:
		ss.color = goterm.RED
	}

	return ss
}

func sortStatusStrings(statuses []statusString) {
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].name < statuses[j].name
	})
}
