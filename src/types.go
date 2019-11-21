package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/containerd/containerd/cio"

	"github.com/containerd/containerd/oci"

	"github.com/opencontainers/runtime-spec/specs-go"

	"github.com/eclipse-fog05/sdk-go/fog05sdk"
	fog05 "github.com/eclipse-fog05/sdk-go/fog05sdk"

	"github.com/fatih/structs"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	log "github.com/sirupsen/logrus"
)

// ContainerDFDU ...
type ContainerDFDU struct {
	UUID          string   `json:"uuid"`
	Image         string   `json:"image"`
	Namespace     string   `json:"ns"`
	LogFile       string   `json:"log_file"`
	ImageSnapshot string   `json:"snapshot"`
	Interfaces    []string `json:"interfaces"`
}

// ContainerDPluginState ...
type ContainerDPluginState struct {
	BaseDir             string                       `json:"base_dir"`
	ImageDir            string                       `json:"image_dir"`
	LogDir              string                       `json:"log_dir"`
	ImageServer         string                       `json:"image_server"`
	UpdateInterval      int                          `json:"update_interval"`
	ContainerdNamespace string                       `json:"containerd_namespace"`
	CurrentInstances    map[string][]fog05.FDURecord `json:"instances"`
	Images              []string                     `json:"images"`
	Containers          map[string]ContainerDFDU     `json:"container"`
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

	st := ContainerDPluginState{CurrentInstances: map[string][]fog05.FDURecord{}, Containers: map[string]ContainerDFDU{}, Images: []string{}}

	ctd := ContainerDPlugin{ContClient: nil, sigs: make(chan os.Signal, 1), done: make(chan bool, 1), manifest: &manifest, state: st}

	ctd.FOSRuntimePluginAbstract = *rtp
	ctd.FOSRuntimePluginAbstract.FOSRuntimePluginInterface = &ctd

	return &ctd, nil
}

func (ctd *ContainerDPlugin) findContainer(name string) (*containerd.Container, error) {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Looking for container: ", name)
	containers, err := ctd.ContClient.Containers(ctd.containerdCtx)

	var container *containerd.Container = nil

	if err != nil {
		return nil, err
	}

	for _, c := range containers {
		if c.ID() == name {
			container = &c
			break
		}
	}

	if container == nil {
		return nil, &fog05sdk.FError{("Container " + name + "not found"), nil}
	}
	return container, nil

}

// StartRuntime ...
func (ctd *ContainerDPlugin) StartRuntime() error {

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
func (ctd *ContainerDPlugin) StopRuntime() error {
	ctd.FOSRuntimePluginAbstract.Logger.Info("Bye from containerd Plugin")
	ctd.ContClient.Close()
	ctd.FOSRuntimePluginAbstract.FOSPlugin.RemovePluginState()
	ctd.FOSRuntimePluginAbstract.Close()
	return nil

}

// DefineFDU ....
func (ctd *ContainerDPlugin) DefineFDU(record fog05.FDURecord) error {
	lstr := fmt.Sprintf("This is define for %s - %s", record.FDUID, record.UUID)
	ctd.FOSRuntimePluginAbstract.Logger.Info(lstr)
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Defining a container")

	ctd.FOSRuntimePluginAbstract.Logger.Debug("Pulling image: ", record.Image.URI)
	img, err := ctd.ContClient.Pull(ctd.containerdCtx, record.Image.URI, containerd.WithPullUnpack)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.WriteFDUError(record.FDUID, record.UUID, 500, err.Error())
		return err
	}
	ctd.state.Images = append(ctd.state.Images, img.Name())
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Added image: ", img.Name())

	cont := ContainerDFDU{record.UUID, img.Name(), record.UUID, path.Join(ctd.state.BaseDir, ctd.state.LogDir, record.UUID+".log"), img.Name() + "-" + record.UUID, []string{}}
	ctd.state.Containers[record.UUID] = cont

	err = ctd.FOSRuntimePluginAbstract.AddFDURecord(record.UUID, &record)
	ctd.FOSRuntimePluginAbstract.Logger.Info("Defined container: ", record.UUID)
	return err
}

// UndefineFDU ....
func (ctd *ContainerDPlugin) UndefineFDU(instanceid string) error {
	lstr := fmt.Sprintf("This is remove for %s", instanceid)
	ctd.FOSRuntimePluginAbstract.Logger.Info(lstr)
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Undefining a container")

	delete(ctd.state.Containers, instanceid)

	return ctd.FOSRuntimePluginAbstract.RemoveFDURecord(instanceid)
}

// ConfigureFDU ....
func (ctd *ContainerDPlugin) ConfigureFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Info("Configure container: ", instanceid)
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	cont := ctd.state.Containers[instanceid]

	// we should create the interfaces and attach them to this namespace

	ctd.FOSRuntimePluginAbstract.Logger.Debug("Creating container namespace: ", instanceid)
	cmd := fmt.Sprintf("sudo ip netns add %s", instanceid)
	ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)

	for _, cp := range record.ConnectionPoints {
		err = ctd.FOSRuntimePluginAbstract.NM.AddNodePort(cp)
		if err != nil {
			ctd.FOSRuntimePluginAbstract.Logger.Error("Error in creation of connection point")
		}
		res, _ := ctd.FOSRuntimePluginAbstract.NM.GetNodePort(cp.UUID)
		for res == nil {
			res, _ = ctd.FOSRuntimePluginAbstract.NM.GetNodePort(cp.UUID)
		}

	}

	for i, vFace := range *(record.Interfaces) {
		faceName := vFace.VirtualInterfaceName
		intfID := fmt.Sprintf("ctd-%s-%d", instanceid, i)
		ctd.FOSRuntimePluginAbstract.Logger.Debug("Creating virtual interface: ", faceName)
		if vFace.VirtualInterface.InterfaceType == fog05sdk.PHYSICAL || vFace.VirtualInterface.InterfaceType == fog05sdk.BRIDGED {
			if vFace.PhysicalFace != nil {

				switch faceType, _ := ctd.FOSPlugin.OS.GetInterfaceType(*vFace.PhysicalFace); faceType {
				case "ethernet":
					// mac := vFace.MACAddress
					// if mac == nil {
					// 	mac := "00:00:00:00:00:00"
					// }

					// cmd = fmt.Sprintf("sudo ip link set ctd%s%d-i up", instanceid, i)
					// ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
					cmd = fmt.Sprintf("sudo ip link add %s link %s type macvlan mode bridge", intfID, *vFace.PhysicalFace)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
					cmd = fmt.Sprintf("sudo ip link set %s netns %s", intfID, instanceid)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
					cmd = fmt.Sprintf("sudo ip netns exec %s ip link set %s up", instanceid, intfID)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)

				case "wireless":
					cmd = fmt.Sprintf("sudo ip link set %s netns %s", *vFace.PhysicalFace, instanceid)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
					ctd.FOSPlugin.OS.SetInterfaceUnaviable(*vFace.PhysicalFace)
				default:

					res, err := ctd.FOSRuntimePluginAbstract.NM.CreateVirtualInterface(intfID, vFace)
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error in creating virtual interface")
					}

					iFace := (*res)["int_name"]
					eFace := (*res)["ext_name"]

					cmd = fmt.Sprintf("sudo ip link set %s netns %s", iFace, instanceid)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
					cmd = fmt.Sprintf("sudo ip netns exec %s ip link set %s up", instanceid, iFace)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
					cmd = fmt.Sprintf("sudo ip link set %s master %s", eFace, *vFace.PhysicalFace)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
					cmd = fmt.Sprintf("sudo ip netns exec %s ip link set %s up", instanceid, iFace)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
					cmd = fmt.Sprintf("sudo ip link set %s up", eFace)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)

				}
			} else {
				ctd.FOSRuntimePluginAbstract.Logger.Error("Physical Face is none")
			}

		} else {

			res, err := ctd.FOSRuntimePluginAbstract.NM.CreateVirtualInterface(intfID, vFace)
			if err != nil {
				ctd.FOSRuntimePluginAbstract.Logger.Error("Error in creating virtual interface")
			}

			iFace := (*res)["int_name"]
			// eFace := (*res)["ext_name"]

			cmd = fmt.Sprintf("sudo ip link set %s netns %s", iFace, instanceid)
			ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
			cmd = fmt.Sprintf("sudo ip netns exec %s ip link set %s up", instanceid, iFace)
			ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)

		}

		cont.Interfaces = append(cont.Interfaces, intfID)

	}

	ns := specs.LinuxNamespace{
		Type: specs.NetworkNamespace,
		Path: path.Join("/var/run/netns", cont.Namespace)}

	img, err := ctd.ContClient.GetImage(ctd.containerdCtx, cont.Image)
	if err != nil {
		return err
	}

	var opts []oci.SpecOpts
	var cOpts []containerd.NewContainerOpts
	var s specs.Spec
	var spec containerd.NewContainerOpts

	//setting opts
	cOpts = append(cOpts, containerd.WithImage(img))
	cOpts = append(cOpts, containerd.WithNewSnapshot(cont.ImageSnapshot, img))
	opts = append(opts, oci.WithDefaultSpec(), oci.WithDefaultUnixDevices)
	opts = append(opts, oci.WithImageConfig(img))
	opts = append(opts, oci.WithLinuxNamespace(ns))

	spec = containerd.WithSpec(&s, opts...)
	cOpts = append(cOpts, spec)

	ctd.FOSRuntimePluginAbstract.Logger.Debug("Creating container: ", cont.UUID)
	_, err = ctd.ContClient.NewContainer(ctd.containerdCtx, cont.UUID, cOpts...)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot create container: ", err)
		return err
	}

	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, instanceid, fog05.CONFIGURE)
}

// CleanFDU ....
func (ctd *ContainerDPlugin) CleanFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Clean a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	cont := ctd.state.Containers[instanceid]

	container, err := ctd.findContainer(cont.UUID)
	if err != nil {
		return err
	}

	err = (*container).Delete(ctd.containerdCtx, containerd.WithSnapshotCleanup)
	if err != nil {
		return err
	}

	for i, vFace := range *(record.Interfaces) {
		faceName := vFace.VirtualInterfaceName
		intfID := fmt.Sprintf("ctd-%s-%d", instanceid, i)
		ctd.FOSRuntimePluginAbstract.Logger.Debug("Removing interface: ", faceName)
		if vFace.VirtualInterface.InterfaceType == fog05sdk.PHYSICAL || vFace.VirtualInterface.InterfaceType == fog05sdk.BRIDGED {
			if vFace.PhysicalFace != nil {

				switch faceType, _ := ctd.FOSPlugin.OS.GetInterfaceType(*vFace.PhysicalFace); faceType {
				case "ethernet":
					// mac := vFace.MACAddress
					// if mac == nil {
					// 	mac := "00:00:00:00:00:00"
					// }

					cmd := fmt.Sprintf("sudo ip link set %s netns 0", intfID)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
					cmd = fmt.Sprintf("sudo link del %s", intfID)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)

				case "wireless":
					cmd := fmt.Sprintf("sudo ip link set %s netns 0", *vFace.PhysicalFace)
					ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)
					ctd.FOSPlugin.OS.SetInterfaceAvailable(*vFace.PhysicalFace)
				default:

					_, err := ctd.FOSRuntimePluginAbstract.NM.DeleteVirtualInterface(intfID)
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error in deletion of virtual interface")
					}

				}
			} else {
				ctd.FOSRuntimePluginAbstract.Logger.Error("Physical Face is none")
			}

		} else {

			_, err := ctd.FOSRuntimePluginAbstract.NM.DeleteVirtualInterface(intfID)
			if err != nil {
				ctd.FOSRuntimePluginAbstract.Logger.Error("Error in deletion of virtual interface")
			}

		}

	}
	cmd := fmt.Sprintf("sudo ip netns del %s", instanceid)
	ctd.FOSPlugin.OS.ExecuteCommand(cmd, true, true)

	for _, cp := range record.ConnectionPoints {
		err = ctd.FOSRuntimePluginAbstract.NM.RemoveNodePort(cp.UUID)
		if err != nil {
			ctd.FOSRuntimePluginAbstract.Logger.Error("Error in creation of connection point")
		}

	}

	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.DEFINE)
}

// RunFDU ....
func (ctd *ContainerDPlugin) RunFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Start a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}

	cont := ctd.state.Containers[instanceid]

	container, err := ctd.findContainer(cont.UUID)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot find container ", err)
		return err
	}
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Starting container: ", (*container).ID())

	ctd.FOSRuntimePluginAbstract.Logger.Debug("Creating task")
	task, err := (*container).NewTask(ctd.containerdCtx, cio.LogFile(cont.LogFile))
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot create task: ", err)
		return err
	}

	ctd.FOSRuntimePluginAbstract.Logger.Debug("Starting task")
	err = task.Start(ctd.containerdCtx)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot start task: ", err)
		return err
	}

	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.RUN)

}

// StopFDU ....
func (ctd *ContainerDPlugin) StopFDU(instanceid string) error {

	var taskStatus containerd.Status

	ctd.FOSRuntimePluginAbstract.Logger.Debug("Stop a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}

	cont := ctd.state.Containers[instanceid]

	container, err := ctd.findContainer(cont.UUID)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot find container ", err)
		return err
	}

	task, err := (*container).Task(ctd.containerdCtx, nil)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot find task ", err)
		return err
	}
	err = task.Kill(ctd.containerdCtx, syscall.SIGTERM)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot send SIGTERM to task ", err)
		return err
	}
	taskStatus, err = task.Status(ctd.containerdCtx)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot get task status ", err)
		return err
	}

	for true {
		ctd.FOSRuntimePluginAbstract.Logger.Debug("Task status is ", taskStatus)
		if taskStatus.Status == containerd.Stopped {
			break
		} else {
			taskStatus, err = task.Status(ctd.containerdCtx)
			if err != nil {
				ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot get task status ", err)
				return err
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	es, err := task.Delete(ctd.containerdCtx)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot delete task ", err)
		return err
	}
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Task exist status ", *es)

	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.CONFIGURE)
}

// PauseFDU ....
func (ctd *ContainerDPlugin) PauseFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Pause a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.PAUSE)
}

// ResumeFDU ....
func (ctd *ContainerDPlugin) ResumeFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Resume a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.RUN)
}
