package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"

	fog05 "github.com/eclipse-fog05/sdk-go/fog05sdk"

	"github.com/fatih/structs"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	log "github.com/sirupsen/logrus"
)

type ContainerDPluginState struct {
	BaseDir             string                       `json:"base_dir"`
	ImageDir            string                       `json:"image_dir"`
	LogDir              string                       `json:"log_dir"`
	ImageServer         string                       `json:"image_server"`
	UpdateInterval      int                          `json:"update_interval"`
	ContainerdNamespace string                       `json:"containerd_namespace"`
	CurrentInstances    map[string][]fog05.FDURecord `json:"instances"`
}

// ContainerDPlugin ...
type ContainerDPlugin struct {
	fog05.FOSRuntimePluginAbstract

	ContClient    *containerd.Client
	sigs          chan os.Signal
	done          chan bool
	state         ContainerDPluginState
	containerdCtx context.Context
	manifest      *fog05.Plugin
}

// NewContainerDPlugin ...
func NewContainerDPlugin(name string, version int, plid string, manifest fog05.Plugin) (*ContainerDPlugin, error) {

	rtp, err := fog05.NewFOSRuntimePluginAbstract(name, version, plid, manifest)
	if err != nil {
		return nil, err
	}

	st := ContainerDPluginState{CurrentInstances: map[string][]fog05.FDURecord{}}

	ctd := ContainerDPlugin{ContClient: nil, sigs: make(chan os.Signal, 1), done: make(chan bool, 1), manifest: &manifest, state: st}

	ctd.FOSRuntimePluginAbstract = *rtp
	ctd.FOSRuntimePluginAbstract.FOSRuntimePluginInterface = ctd

	return &ctd, nil
}

// StartRuntime ...
func (ctd ContainerDPlugin) StartRuntime() error {

	ctd.FOSRuntimePluginAbstract.Logger.SetReportCaller(true)
	ctd.FOSRuntimePluginAbstract.Logger.SetLevel(log.TraceLevel)
	ctd.FOSRuntimePluginAbstract.Logger.Info("Connecting to containerd ... ")
	cclient, err := containerd.New("/run/containerd/containerd.sock")
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot connect to containerd!!!")
		ctd.FOSRuntimePluginAbstract.Logger.Error(err)
		return err
	}
	ctd.ContClient = cclient

	ctd.FOSRuntimePluginAbstract.Logger.Info("Hello from containerd Plugin - PID: ", ctd.FOSRuntimePluginAbstract.Pid)

	signal.Notify(ctd.sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ctd.sigs
		ctd.done <- true
	}()

	s := "running"
	ctd.manifest.Status = &s

	ctd.FOSRuntimePluginAbstract.RegisterPlugin(ctd.manifest)

	nodeConf, err := ctd.FOSRuntimePluginAbstract.FOSPlugin.GetNodeConfiguration()
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot get node configuration!!!")
		ctd.FOSRuntimePluginAbstract.Logger.Error(err)
		return err
	}
	ctd.state.BaseDir = path.Join(nodeConf.Agent.Path, "containerd")
	ctd.state.ImageDir = "images"
	ctd.state.LogDir = "logs"
	ctd.state.ImageServer = ""
	ctd.state.ContainerdNamespace = "fos"

	ctd.containerdCtx = namespaces.WithNamespace(context.Background(), ctd.state.ContainerdNamespace)

	ctd.FOSRuntimePluginAbstract.FOSPlugin.SavePluginState(structs.Map(ctd.state))

	<-ctd.done
	return ctd.StopRuntime()
}

// StopRuntime ....
func (ctd ContainerDPlugin) StopRuntime() error {
	ctd.FOSRuntimePluginAbstract.Logger.Info("Bye from containerd Plugin")
	ctd.ContClient.Close()
	ctd.FOSRuntimePluginAbstract.FOSPlugin.RemovePluginState()
	ctd.FOSRuntimePluginAbstract.Close()
	return nil

}

// DefineFDU ....
func (ctd ContainerDPlugin) DefineFDU(record fog05.FDURecord) error {
	lstr := fmt.Sprintf("This is define for %s - %s", record.FDUID, record.UUID)
	ctd.FOSRuntimePluginAbstract.Logger.Info(lstr)
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Defining a container")

	_, err := ctd.ContClient.Pull(ctd.containerdCtx, record.Image.URI, containerd.WithPullUnpack)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.WriteFDUError(record.FDUID, record.UUID, 500, err.Error())
		return err
	}

	return ctd.FOSRuntimePluginAbstract.AddFDURecord(record.UUID, &record)
}

// UndefineFDU ....
func (ctd ContainerDPlugin) UndefineFDU(instanceid string) error {
	lstr := fmt.Sprintf("This is remove for %s", instanceid)
	ctd.FOSRuntimePluginAbstract.Logger.Info(lstr)
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Undefining a container")
	return ctd.FOSRuntimePluginAbstract.RemoveFDURecord(instanceid)
}

// ConfigureFDU ....
func (ctd ContainerDPlugin) ConfigureFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Configure a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, instanceid, fog05.CONFIGURE)
}

// CleanFDU ....
func (ctd ContainerDPlugin) CleanFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Clean a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.DEFINE)
}

// StartFDU ....
func (ctd ContainerDPlugin) StartFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Start a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.RUN)

}

// StopFDU ....
func (ctd ContainerDPlugin) StopFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Stop a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.STOP)
}

// PauseFDU ....
func (ctd ContainerDPlugin) PauseFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Pause a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.PAUSE)
}

// ResumeFDU ....
func (ctd ContainerDPlugin) ResumeFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Resume a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.RUN)
}
