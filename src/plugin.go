package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	fog05 "github.com/eclipse-fog05/sdk-go/fog05sdk"
)

func check(e error) {
	if e != nil {
		panic(e)
	}

}

func main() {
	args := os.Args[1:]
	fmt.Println(args)

	data, err := ioutil.ReadFile(args[0])
	check(err)
	pld := fog05.Plugin{}
	json.Unmarshal(data, &pld)

	conf := *pld.Configuration

	fmt.Printf("YLocator is %s\n", conf["ylocator"].(string))

	cdp, err := NewContainerDPlugin(pld.Name, pld.Version, pld.UUID, pld)
	check(err)
	cdp.FOSRuntimePluginAbstract.Start()

}

// package main

// import (
// 	"context"
// 	"fmt"
// 	"log"
// 	"syscall"

// 	// "time"
// 	"bufio"
// 	"os"

// 	"github.com/containerd/containerd"
// 	"github.com/containerd/containerd/cio"
// 	"github.com/containerd/containerd/namespaces"
// 	"github.com/containerd/containerd/oci"
// 	"github.com/opencontainers/runtime-spec/specs-go"
// )

// func main() {
// 	if err := redisExample(); err != nil {
// 		log.Fatal(err)
// 	}
// }

// func redisExample() error {
// 	// create a new client connected to the default socket path for containerd
// 	client, err := containerd.New("/run/containerd/containerd.sock")
// 	if err != nil {
// 		return err
// 	}
// 	defer client.Close()

// 	// create a new context with an "example" namespace
// 	ctx := namespaces.WithNamespace(context.Background(), "example")

// 	// pull the redis image from DockerHub
// 	image, err := client.Pull(ctx, "docker.io/library/redis:alpine", containerd.WithPullUnpack)
// 	if err != nil {
// 		return err
// 	}

// 	// // pull the yaks image from Dockerhub
// 	// image2, err := client.Pull(ctx, "docker.io/fog05/yaks", containerd.WithPullUnpack)
// 	// if err != nil {
// 	// 	return err
// 	// }

// 	netns := specs.LinuxNamespace{
// 		Type: specs.NetworkNamespace,
// 		Path: "/var/run/netns/c1",
// 	}
// 	// if err != nil {
// 	// 	return err
// 	// }

// 	var opts []oci.SpecOpts
// 	var cOpts []containerd.NewContainerOpts
// 	var s specs.Spec
// 	var spec containerd.NewContainerOpts

// 	// setting opts
// 	cOpts = append(cOpts, containerd.WithImage(image))
// 	cOpts = append(cOpts, containerd.WithNewSnapshot("redis-server-snapshot", image))
// 	// cOpts = append(cOpts, containerd.WithRuntime("io.containerd.runc.v1", nil))
// 	opts = append(opts, oci.WithDefaultSpec(), oci.WithDefaultUnixDevices)
// 	opts = append(opts, oci.WithImageConfig(image))
// 	opts = append(opts, oci.WithLinuxNamespace(netns))

// 	spec = containerd.WithSpec(&s, opts...)
// 	cOpts = append(cOpts, spec)

// 	// create a container
// 	container, err := client.NewContainer(
// 		ctx,
// 		"example",
// 		cOpts...,
// 	)
// 	if err != nil {
// 		return err
// 	}
// 	defer container.Delete(ctx, containerd.WithSnapshotCleanup)

// 	// create a task from the container
// 	// task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
// 	task, err := container.NewTask(ctx, cio.LogFile("/tmp/redis.log"))
// 	if err != nil {
// 		return err
// 	}
// 	defer task.Delete(ctx)

// 	// make sure we wait before calling start
// 	exitStatusC, err := task.Wait(ctx)
// 	if err != nil {
// 		fmt.Println(err)
// 	}

// 	// call start on the task to execute the redis server
// 	if err := task.Start(ctx); err != nil {
// 		return err
// 	}

// 	// sleep for a lil bit to see the logs
// 	// time.Sleep(10 * time.Second)
// 	fmt.Print("Press 'Enter' to continue...")
// 	bufio.NewReader(os.Stdin).ReadBytes('\n')

// 	// kill the process and get the exit status
// 	if err := task.Kill(ctx, syscall.SIGTERM); err != nil {
// 		return err
// 	}

// 	// wait for the process to fully exit and print out the exit status

// 	status := <-exitStatusC
// 	code, _, err := status.Result()
// 	if err != nil {
// 		return err
// 	}
// 	fmt.Printf("redis-server exited with status: %d\n", code)

// 	return nil
// }
