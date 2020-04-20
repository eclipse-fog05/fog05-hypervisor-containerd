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

	cdp, err := NewContainerdPlugin(pld.Name, pld.Version, pld.UUID, pld)
	check(err)
	cdp.FOSRuntimePluginAbstract.Start()

}
