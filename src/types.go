package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"strings"
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

const containerdSocket = "/run/containerd/containerd.sock"

// ContainerdFDUIntrefaceInfo ...
type ContainerdFDUIntrefaceInfo struct {
	Internal *fog05.InterfaceInfo `json:"internal,omitempty"`
	External *fog05.InterfaceInfo `json:"external,omitempty"`
}

// ContainerdFDU ...
type ContainerdFDU struct {
	UUID             string                        `json:"uuid"`
	Image            string                        `json:"image"`
	Namespace        string                        `json:"ns"`
	LogFile          string                        `json:"log_file"`
	ImageSnapshot    string                        `json:"snapshot"`
	Interfaces       []ContainerdFDUIntrefaceInfo  `json:"interfaces"`
	ConnectionPoints []fog05.ConnectionPointRecord `json:"connection_points"`
}

// ContainerdPluginState ...
type ContainerdPluginState struct {
	BaseDir             string                       `json:"base_dir"`
	ImageDir            string                       `json:"image_dir"`
	LogDir              string                       `json:"log_dir"`
	ImageServer         string                       `json:"image_server"`
	UpdateInterval      int                          `json:"update_interval"`
	ContainerdNamespace string                       `json:"containerd_namespace"`
	CurrentInstances    map[string][]fog05.FDURecord `json:"instances"`
	Images              []string                     `json:"images"`
	Containers          map[string]ContainerdFDU     `json:"container"`
}

// ContainerdPlugin ...
type ContainerdPlugin struct {
	fog05.FOSRuntimePluginAbstract

	ContClient    *containerd.Client
	sigs          chan os.Signal
	done          chan bool
	state         ContainerdPluginState
	containerdCtx context.Context
	manifest      *fog05.Plugin
}

// NewContainerdPlugin ...
func NewContainerdPlugin(name string, version int, plid string, manifest fog05.Plugin) (*ContainerdPlugin, error) {

	rtp, err := fog05.NewFOSRuntimePluginAbstract(name, version, plid, manifest)
	if err != nil {
		return nil, err
	}

	st := ContainerdPluginState{CurrentInstances: map[string][]fog05.FDURecord{}, Containers: map[string]ContainerdFDU{}, Images: []string{}}

	ctd := ContainerdPlugin{ContClient: nil, sigs: make(chan os.Signal, 1), done: make(chan bool, 1), manifest: &manifest, state: st}

	ctd.FOSRuntimePluginAbstract = *rtp
	ctd.FOSRuntimePluginAbstract.FOSRuntimePluginInterface = &ctd

	return &ctd, nil
}

func (ctd *ContainerdPlugin) findContainer(name string) (*containerd.Container, error) {
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

func (ctd *ContainerdPlugin) getShortFDUID(FDUID string) string {
	s := strings.Split(FDUID, "-")

	shortID := fmt.Sprintf("%s%s%s%s%s", string(s[0][0]), string(s[1][0]), string(s[2][0]), string(s[3][0]), string(s[4][0]))

	return shortID

}

// StartRuntime ...
func (ctd *ContainerdPlugin) StartRuntime() error {

	ctd.FOSRuntimePluginAbstract.Logger.SetReportCaller(true)
	ctd.FOSRuntimePluginAbstract.Logger.SetLevel(log.TraceLevel)
	ctd.FOSRuntimePluginAbstract.Logger.Info("Connecting to containerd ... ")

	var sock string
	sockFile, ok := (*ctd.manifest.Configuration)["containerd_socket"]
	if ok {
		sock = sockFile.(string)
	} else {
		sock = containerdSocket
	}

	cclient, err := containerd.New(sock)
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
func (ctd *ContainerdPlugin) StopRuntime() error {
	ctd.FOSRuntimePluginAbstract.Logger.Info("Removing running containers...")
	ctd.clearRuntime()
	ctd.FOSRuntimePluginAbstract.Logger.Info("Bye from containerd Plugin")
	ctd.ContClient.Close()
	ctd.FOSRuntimePluginAbstract.FOSPlugin.RemovePluginState()
	ctd.FOSRuntimePluginAbstract.Close()
	return nil

}

// DefineFDU ....
func (ctd *ContainerdPlugin) DefineFDU(record fog05.FDURecord) error {
	lstr := fmt.Sprintf("This is define for %s - %s", record.FDUID, record.UUID)
	ctd.FOSRuntimePluginAbstract.Logger.Info(lstr)
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Defining a container")

	if strings.HasPrefix(record.Image.URI, "file://") {

		f, _ := os.Open(strings.TrimPrefix(record.Image.URI, "file://"))
		imgs, err := ctd.ContClient.Import(ctd.containerdCtx, f)
		if err != nil {
			ctd.FOSRuntimePluginAbstract.WriteFDUError(record.FDUID, record.UUID, 500, err.Error())
			return err
		}
		f.Close()
		img := imgs[0]
		ctd.state.Images = append(ctd.state.Images, img.Name)
		ctd.FOSRuntimePluginAbstract.Logger.Debug("Added image: ", img.Name)

		cont := ContainerdFDU{record.UUID, img.Name, record.UUID, path.Join(ctd.state.BaseDir, ctd.state.LogDir, record.UUID+".log"), img.Name + "-" + record.UUID, []ContainerdFDUIntrefaceInfo{}, []fog05.ConnectionPointRecord{}}
		ctd.state.Containers[record.UUID] = cont

	} else {

		var img containerd.Image

		img, err := ctd.ContClient.GetImage(ctd.containerdCtx, record.Image.URI)
		if err != nil {
			ctd.FOSRuntimePluginAbstract.Logger.Debug("Image not present pulling image: ", record.Image.URI)
			img, err = ctd.ContClient.Pull(ctd.containerdCtx, record.Image.URI, containerd.WithPullUnpack)
			if err != nil {
				ctd.FOSRuntimePluginAbstract.WriteFDUError(record.FDUID, record.UUID, 500, err.Error())
				return err
			}
		} else {
			ctd.FOSRuntimePluginAbstract.Logger.Debug("Image Already present: ", record.Image.URI)
		}

		ctd.state.Images = append(ctd.state.Images, img.Name())
		ctd.FOSRuntimePluginAbstract.Logger.Debug("Added image: ", img.Name())

		cont := ContainerdFDU{record.UUID, img.Name(), record.UUID, path.Join(ctd.state.BaseDir, ctd.state.LogDir, record.UUID+".log"), img.Name() + "-" + record.UUID, []ContainerdFDUIntrefaceInfo{}, []fog05.ConnectionPointRecord{}}
		ctd.state.Containers[record.UUID] = cont
	}

	err := ctd.FOSRuntimePluginAbstract.AddFDURecord(record.UUID, &record)
	ctd.FOSRuntimePluginAbstract.Logger.Info("Defined container: ", record.UUID)
	return err
}

// UndefineFDU ....
func (ctd *ContainerdPlugin) UndefineFDU(instanceid string) error {
	lstr := fmt.Sprintf("This is remove for %s", instanceid)
	ctd.FOSRuntimePluginAbstract.Logger.Info(lstr)
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Undefining a container")

	delete(ctd.state.Containers, instanceid)

	return ctd.FOSRuntimePluginAbstract.RemoveFDURecord(instanceid)
}

// ConfigureFDU ....
func (ctd *ContainerdPlugin) ConfigureFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Info("Configure container: ", instanceid)
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	cont := ctd.state.Containers[instanceid]

	// we should create the interfaces and attach them to this namespace

	ctd.FOSRuntimePluginAbstract.Logger.Debug("Creating container namespace: ", instanceid)
	nsName, err := ctd.FOSPlugin.NM.CreateNetworkNamespace()
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Error in creation of network namespace")
		return err
	}

	cont.Namespace = nsName

	for _, cp := range record.ConnectionPoints {
		err = ctd.FOSRuntimePluginAbstract.NM.AddNodePort(cp)
		if err != nil {
			ctd.FOSRuntimePluginAbstract.Logger.Error("Error in creation of connection point")
		}
		res, _ := ctd.FOSRuntimePluginAbstract.NM.GetNodePort(cp.UUID)
		for res == nil {
			res, _ = ctd.FOSRuntimePluginAbstract.NM.GetNodePort(cp.UUID)
			cont.ConnectionPoints = append(cont.ConnectionPoints, *res)
		}

	}

	for _, vFace := range *(record.Interfaces) {
		faceName := vFace.VirtualInterfaceName

		ctd.FOSRuntimePluginAbstract.Logger.Debug("Creating virtual interface: ", faceName)
		ctd.FOSRuntimePluginAbstract.Logger.Debug("Creating virtual interface: ", vFace)
		mac := vFace.MACAddress
		if vFace.VirtualInterface.InterfaceType == fog05sdk.PHYSICAL || vFace.VirtualInterface.InterfaceType == fog05sdk.BRIDGED {
			if vFace.PhysicalFace != nil {

				switch faceType, _ := ctd.FOSRuntimePluginAbstract.OS.GetInterfaceType(*vFace.PhysicalFace); faceType {
				case "ethernet":

					macvlanTempName, err := ctd.FOSRuntimePluginAbstract.NM.CreateMACVLANInterface(*vFace.PhysicalFace)
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error in creation of network interface %s %s", faceName, err)
						continue
					}

					_, err = ctd.FOSRuntimePluginAbstract.NM.MoveInterfaceInNamespace(macvlanTempName, cont.Namespace)
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error in moving interface %s to namespace %s: %s", macvlanTempName, cont.Namespace, err)
						continue
					}

					//setting mac address
					if mac != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Debug("Assiging MAC address to virtual interface: ", mac)
						_, err = ctd.FOSRuntimePluginAbstract.NM.AssignMACAddressToInterfaceInNamespace(macvlanTempName, cont.Namespace, *mac)
						if err != nil {
							ctd.FOSRuntimePluginAbstract.Logger.Error("Error on assign mac address: ", err)
							continue
						}
					}

					_, err = ctd.FOSRuntimePluginAbstract.NM.RenameVirtualInterfaceInNamespace(macvlanTempName, faceName, cont.Namespace)
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error on rename interface: ", err)
						continue
					}

					intfInfo, err := ctd.FOSRuntimePluginAbstract.NM.AssignAddressToInterfaceInNamespace(faceName, cont.Namespace, "")

					intFDUInfo := ContainerdFDUIntrefaceInfo{Internal: &intfInfo.Internal, External: nil}

					cont.Interfaces = append(cont.Interfaces, intFDUInfo)

				case "wireless":

					intfInfo, err := ctd.FOSRuntimePluginAbstract.NM.MoveInterfaceInNamespace(*vFace.PhysicalFace, cont.Namespace)
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error on moving interface to namespace: ", err)
						continue
					}
					_, err = ctd.FOSRuntimePluginAbstract.NM.RenameVirtualInterfaceInNamespace(*vFace.PhysicalFace, faceName, cont.Namespace)
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error on rename interface: ", err)
						continue
					}
					intFDUInfo := ContainerdFDUIntrefaceInfo{Internal: intfInfo, External: nil}
					cont.Interfaces = append(cont.Interfaces, intFDUInfo)

				default:

					intfInfo, err := ctd.FOSPlugin.NM.CreateVirtualInterfaceInNamespace(faceName, cont.Namespace)
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error on interface creation: ", err)
						continue
					}

					if mac != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Debug("Assiging MAC address to virtual interface: ", mac)
						_, err = ctd.FOSRuntimePluginAbstract.NM.AssignMACAddressToInterfaceInNamespace(faceName, cont.Namespace, *mac)
						if err != nil {
							ctd.FOSRuntimePluginAbstract.Logger.Error("Error on assign mac address: ", err)
							continue
						}
					}

					_, err = ctd.FOSRuntimePluginAbstract.NM.AttachInterfaceToBridge(intfInfo.External.Name, *vFace.PhysicalFace)
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error on attaching to bridge: ", err)
						continue
					}

					intfInfo, err = ctd.FOSRuntimePluginAbstract.NM.AssignAddressToInterfaceInNamespace(faceName, cont.Namespace, "")
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error on assigning address: ", err)
						continue
					}

					intFDUInfo := ContainerdFDUIntrefaceInfo{Internal: &intfInfo.Internal, External: &intfInfo.External}
					cont.Interfaces = append(cont.Interfaces, intFDUInfo)

				}
			} else {
				ctd.FOSRuntimePluginAbstract.Logger.Error("Physical Face is none")
			}

		} else {

			intfInfo, err := ctd.FOSRuntimePluginAbstract.NM.CreateVirtualInterfaceInNamespace(faceName, cont.Namespace)
			if err != nil {
				ctd.FOSRuntimePluginAbstract.Logger.Error("Error on interface creation: ", err)
				continue
			}

			if mac != nil {
				ctd.FOSRuntimePluginAbstract.Logger.Debug("Assiging MAC address to virtual interface: ", mac)
				_, err = ctd.FOSRuntimePluginAbstract.NM.AssignMACAddressToInterfaceInNamespace(faceName, cont.Namespace, *mac)
				if err != nil {
					ctd.FOSRuntimePluginAbstract.Logger.Error("Error on assign mac address: ", err)
					continue
				}
			}

			cpid := vFace.CPID
			if cpid != nil {
				cp := ctd.findConnectionPoint(cont.ConnectionPoints, *cpid)
				if cp != nil {
					_, err = ctd.FOSRuntimePluginAbstract.NM.AttachInterfaceToBridge(intfInfo.External.Name, *cp.BrName)
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error on attaching to bridge: ", err)
						continue
					}

					_, err = ctd.FOSRuntimePluginAbstract.NM.AssignAddressToInterfaceInNamespace(faceName, cont.Namespace, "")
					if err != nil {
						ctd.FOSRuntimePluginAbstract.Logger.Error("Error on assigning address: ", err)
						continue
					}
				} else {
					ctd.FOSRuntimePluginAbstract.Logger.Error("Unable to find a ConnectionPoint for ", faceName)
				}

			} else {
				ctd.FOSRuntimePluginAbstract.Logger.Error("Interface is not connected to anything", faceName)
			}
			intFDUInfo := ContainerdFDUIntrefaceInfo{Internal: &intfInfo.Internal, External: &intfInfo.External}
			cont.Interfaces = append(cont.Interfaces, intFDUInfo)
		}

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
func (ctd *ContainerdPlugin) CleanFDU(instanceid string) error {
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

	for _, vFace := range cont.Interfaces {
		if vFace.External != nil {
			_, err = ctd.FOSPlugin.NM.DetachInterfaceFromBridge(vFace.External.Name)
			if err != nil {
				ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot detach interface: ", err)
			}

		}
		_, err := ctd.FOSPlugin.NM.DeleteVirtualInterfaceFromNamespace(vFace.Internal.Name, cont.Namespace)
		if err != nil {
			ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot delete interface: ", err)
		}
	}

	for _, cp := range record.ConnectionPoints {
		err = ctd.FOSRuntimePluginAbstract.NM.RemoveNodePort(cp.UUID)
		if err != nil {
			ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot delete connection point: ", err)
		}

	}

	_, err = ctd.FOSPlugin.NM.DeleteNetworkNamespace(cont.Namespace)
	if err != nil {
		ctd.FOSRuntimePluginAbstract.Logger.Error("Cannot remove network namespace:", err)
	}

	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.DEFINE)
}

// RunFDU ....
func (ctd *ContainerdPlugin) RunFDU(instanceid string) error {
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
func (ctd *ContainerdPlugin) StopFDU(instanceid string) error {

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
func (ctd *ContainerdPlugin) PauseFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Pause a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.PAUSE)
}

// ResumeFDU ....
func (ctd *ContainerdPlugin) ResumeFDU(instanceid string) error {
	ctd.FOSRuntimePluginAbstract.Logger.Debug("Resume a container")
	record, err := ctd.FOSRuntimePluginAbstract.GetFDURecord(instanceid)
	if err != nil {
		return err
	}
	return ctd.FOSRuntimePluginAbstract.UpdateFDUStatus(record.FDUID, record.UUID, fog05.RUN)
}

func (ctd *ContainerdPlugin) clearRuntime() {
	for _, instances := range ctd.state.CurrentInstances {
		for _, instance := range instances {
			ctd.forceFDUTermination(instance)
		}
	}
}

func (ctd *ContainerdPlugin) forceFDUTermination(instance fog05.FDURecord) {
	switch instance.Status {
	case fog05.PAUSE:
		ctd.ResumeFDU(instance.UUID)
		ctd.StopFDU(instance.UUID)
		ctd.CleanFDU(instance.UUID)
		ctd.UndefineFDU(instance.UUID)
	case fog05.RUN:
		ctd.StopFDU(instance.UUID)
		ctd.CleanFDU(instance.UUID)
		ctd.UndefineFDU(instance.UUID)
	case fog05.CONFIGURE:
		ctd.CleanFDU(instance.UUID)
		ctd.UndefineFDU(instance.UUID)
	case fog05.DEFINE:
		ctd.UndefineFDU(instance.UUID)
	}
}

func (ctd *ContainerdPlugin) findConnectionPoint(cps []fog05.ConnectionPointRecord, id string) *fog05.ConnectionPointRecord {
	for _, v := range cps {
		if v.CPID == id {
			return &v
		}
	}
	return nil
}
