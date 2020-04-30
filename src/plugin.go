/*
* Copyright (c) 2014,2020 ADLINK Technology Inc.
* See the NOTICE file(s) distributed with this work for additional
* information regarding copyright ownership.
* This program and the accompanying materials are made available under the
* terms of the Eclipse Public License 2.0 which is available at
* http://www.eclipse.org/legal/epl-2.0, or the Apache License, Version 2.0
* which is available at https://www.apache.org/licenses/LICENSE-2.0.
* SPDX-License-Identifier: EPL-2.0 OR Apache-2.0
* Contributors: Gabriele Baldoni, ADLINK Technology Inc.
* containerd plugin

 */

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
