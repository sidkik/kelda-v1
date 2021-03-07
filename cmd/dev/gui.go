package dev

import (
	"fmt"
	"sort"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/buger/goterm"
	"github.com/jroimartin/gocui"
	"github.com/sirupsen/logrus"

	"github.com/kelda-inc/kelda/cmd/util"
	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
	keldaClientset "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned"
	"github.com/kelda-inc/kelda/pkg/errors"
)

const (
	clusterWidgetName   = "cluster"
	devStatusWidgetName = "dev"
	tunnelsWidgetName   = "tunnels"
	servicesWidgetName  = "services"
	jobsWidgetName      = "jobs"
)

type keldaGUI interface {
	// Run implements the main GUI loop.
	Run(keldaClientset.Interface, string) error

	// GetLogger returns a logrus Logger that can be used to display messages
	// on the user's screen.
	GetLogger() *logrus.Logger
}

// keldaGUIImpl contains the GUI implementation for normal user usage.
type keldaGUIImpl struct {
	logger    *logrus.Logger
	loggerOut chanWriter
}

func newKeldaGUI() keldaGUI {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		ForceColors:     true,
		FullTimestamp:   true,
		TimestampFormat: time.Kitchen,
	})

	// Allow 256 `Write`s without a corresponding `Read`. We give a generous
	// buffer here because if the channel becomes full, calls to write log
	// messages will block until there's space in the channel (which means that
	// any work in the same thread can't proceed until the log message is
	// written to the UI).
	loggerOut := chanWriter(make(chan []byte, 256))
	logger.SetOutput(loggerOut)

	return &keldaGUIImpl{logger, loggerOut}
}

func (keldaGUI *keldaGUIImpl) GetLogger() *logrus.Logger {
	return keldaGUI.logger
}

func (keldaGUI *keldaGUIImpl) Run(keldaClient keldaClientset.Interface, namespace string) error {
	gui, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		return err
	}
	defer gui.Close()

	cluster := &clusterWidget{namespace}

	// Stream the logrus output to the status view.
	dev := &devStatusWidget{height: 5}
	go func() {
		defer util.HandlePanic()
		copyToView(gui, devStatusWidgetName, keldaGUI.loggerOut)
	}()

	tunnelsChan := newTunnelWatcher(keldaClient, namespace)
	tunnels := &tunnelsWidget{}
	go func() {
		defer util.HandlePanic()
		tunnels.syncUpdates(gui, tunnelsChan)
	}()

	servicesChan, jobsChan := newMicroserviceWatcher(keldaClient, namespace)
	services := &servicesWidget{}
	go func() {
		defer util.HandlePanic()
		services.syncUpdates(gui, servicesChan)
	}()

	jobs := &jobsWidget{}
	go func() {
		defer util.HandlePanic()
		jobs.syncUpdates(gui, jobsChan)
	}()

	gui.SetManager(cluster, tunnels, jobs, services, dev)
	ctrlCHandler := func(_ *gocui.Gui, _ *gocui.View) error {
		return gocui.ErrQuit
	}
	if err := gui.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, ctrlCHandler); err != nil {
		return errors.WithContext(err, "bind GUI Ctrl-C")
	}

	if err := gui.MainLoop(); err != nil && err != gocui.ErrQuit {
		return err
	}
	return nil
}

// clusterWidget displays the development namespace at the top of the GUI.
type clusterWidget struct {
	namespace string
}

func (w *clusterWidget) Layout(g *gocui.Gui) error {
	maxWidth, _ := g.Size()
	height := 1

	v, err := g.SetView(clusterWidgetName, 0, 0, maxWidth-1, height+1)
	if err != nil && err != gocui.ErrUnknownView {
		return err
	}

	v.Title = "Cluster"
	v.Wrap = true
	fmt.Fprintf(v, "Namespace: %s\n", w.namespace)

	return nil
}

// tunnelsWidget displays the tunnels. It's placed under the cluster overview.
type tunnelsWidget struct {
	tunnels map[kelda.TunnelSpec]kelda.TunnelStatus
	lock    sync.Mutex
}

// syncUpdates redraws the UI whenever there's new tunnel status in the
// `updates` channel.
func (w *tunnelsWidget) syncUpdates(g *gocui.Gui,
	updates chan map[kelda.TunnelSpec]kelda.TunnelStatus) {

	for {
		update := <-updates

		w.lock.Lock()
		w.tunnels = update
		w.lock.Unlock()

		g.Update(w.Layout)
	}
}

func (w *tunnelsWidget) Layout(g *gocui.Gui) error {
	w.lock.Lock()
	defer w.lock.Unlock()

	height := len(w.tunnels)
	x1, y1, x2, y2, err := relativeTo(g, clusterWidgetName, height)
	if err != nil {
		return err
	}

	v, err := g.SetView(tunnelsWidgetName, x1, y1, x2, y2)
	if err != nil && err != gocui.ErrUnknownView {
		return err
	}
	v.Title = "Tunnels"
	v.Wrap = true
	v.Clear()

	out := tabwriter.NewWriter(v, 0, 10, 5, ' ', 0)
	defer out.Flush()

	// Sort the tunnels so that the output is consistent.
	var names []string
	nameToStatus := map[string]string{}
	for tunnel, status := range w.tunnels {
		name := fmt.Sprintf("localhost:%d -> %s:%d",
			tunnel.LocalPort, tunnel.Service, tunnel.RemotePort)
		nameToStatus[name] = w.statusString(status)
		names = append(names, name)
	}

	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(out, "%s\t%s\n", name, nameToStatus[name])
	}

	return nil
}

func (w *tunnelsWidget) statusString(status kelda.TunnelStatus) string {
	switch status.Phase {
	case kelda.TunnelUp:
		return goterm.Color("Up", goterm.GREEN)
	case kelda.TunnelStarting:
		return goterm.Color("Starting", goterm.YELLOW)
	case kelda.TunnelCrashed:
		if status.Message != "" {
			return goterm.Color(status.Message, goterm.RED)
		}
		return goterm.Color("Down", goterm.RED)
	default:
		return string(status.Phase)
	}
}

// devStatusWidget is an empty view that streams Kelda logs. It's placed under
// the services view.
type devStatusWidget struct {
	height int
}

func (w *devStatusWidget) Layout(g *gocui.Gui) error {
	x1, y1, x2, y2, err := relativeTo(g, servicesWidgetName, w.height)
	if err != nil {
		return err
	}

	v, err := g.SetView(devStatusWidgetName, x1, y1, x2, y2)
	if err != nil && err != gocui.ErrUnknownView {
		return err
	}

	v.Title = "Status"
	v.Wrap = true
	v.Autoscroll = true

	return nil
}

// servicesWidget displays the services. It's placed under the
// jobs view.
type servicesWidget struct {
	services map[string]statusString
	lock     sync.Mutex
}

// syncUpdates redraws the UI whenever there's new service status in the
// `updates` channel.
func (w *servicesWidget) syncUpdates(g *gocui.Gui,
	updates chan map[string]statusString) {
	for {
		update := <-updates

		w.lock.Lock()
		w.services = update
		w.lock.Unlock()

		g.Update(w.Layout)
	}
}

func (w *servicesWidget) Layout(g *gocui.Gui) error {
	w.lock.Lock()
	defer w.lock.Unlock()

	var devServices, regularServices []statusString
	for _, svc := range w.services {
		if svc.isDev {
			devServices = append(devServices, svc)
		} else {
			regularServices = append(regularServices, svc)
		}
	}

	height := len(w.services)
	if len(devServices) != 0 {
		// We need an extra line for the newline after the dev service.
		height++
	}

	x1, y1, x2, y2, err := relativeTo(g, jobsWidgetName, height)
	if err != nil {
		return err
	}

	v, err := g.SetView(servicesWidgetName, x1, y1, x2, y2)
	if err != nil && err != gocui.ErrUnknownView {
		return err
	}

	v.Title = "Services"
	v.Wrap = true
	v.Clear()

	out := tabwriter.NewWriter(v, 0, 10, 5, ' ', 0)
	defer out.Flush()

	// Print dev services.
	sortStatusStrings(devServices)
	for _, svc := range devServices {
		fmt.Fprintf(out, "%s (dev)\t%s\n", svc.name, svc)
	}

	if len(devServices) != 0 {
		fmt.Fprintf(out, "\t\n")
	}

	// Print other services.
	sortStatusStrings(regularServices)
	for _, svc := range regularServices {
		fmt.Fprintf(out, "%s\t%s\n", svc.name, svc)
	}

	return nil
}

// jobsWidget displays the jobs. It's placed under the tunnels view.
type jobsWidget struct {
	jobs map[string]statusString
	lock sync.Mutex
}

// syncUpdates redraws the UI whenever there's new tunnel status in the
// `updates` channel.
func (w *jobsWidget) syncUpdates(g *gocui.Gui,
	updates chan map[string]statusString) {
	for {
		update := <-updates

		w.lock.Lock()
		w.jobs = update
		w.lock.Unlock()

		g.Update(w.Layout)
	}
}

func (w *jobsWidget) Layout(g *gocui.Gui) error {
	w.lock.Lock()
	defer w.lock.Unlock()

	height := len(w.jobs)
	x1, y1, x2, y2, err := relativeTo(g, tunnelsWidgetName, height)
	if err != nil {
		return err
	}

	v, err := g.SetView(jobsWidgetName, x1, y1, x2, y2)
	if err != nil && err != gocui.ErrUnknownView {
		return err
	}

	v.Title = "Jobs"
	v.Wrap = true
	v.Clear()

	out := tabwriter.NewWriter(v, 0, 10, 5, ' ', 0)
	defer out.Flush()
	var names []string
	for name := range w.jobs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(out, "%s\t%s\n", name, w.jobs[name])
	}

	return nil
}

func relativeTo(g *gocui.Gui, view string, height int) (int, int, int, int, error) {
	maxWidth, _ := g.Size()

	_, _, _, origin, err := g.ViewPosition(view)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	top := origin + 1
	return 0, top, maxWidth - 1, top + height + 1, nil
}

// copyToView writes the messages in `stream` into the desired `view` in `gui`.
// It guarantees writes occur in the order of messages in `stream`.
func copyToView(gui *gocui.Gui, view string, stream chanWriter) {
	for b := range stream {
		b := b
		done := make(chan struct{})
		gui.Update(func(gui *gocui.Gui) error {
			defer close(done)
			v, err := gui.View(view)
			if err != nil {
				return err
			}

			if _, err := v.Write(b); err != nil {
				return err
			}
			return nil
		})
		<-done
	}
}
