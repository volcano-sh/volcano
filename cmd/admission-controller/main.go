/*
Copyright 2018 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"volcano.sh/volcano/cmd/admission-controller/app"
	appConf "volcano.sh/volcano/cmd/admission-controller/app/configure"
	admissioncontroller "volcano.sh/volcano/pkg/admission-controller"
)

func serveJobs(w http.ResponseWriter, r *http.Request) {
	app.Serve(w, r, admissioncontroller.AdmitJobs)
}

func serveMutateJobs(w http.ResponseWriter, r *http.Request) {
	app.Serve(w, r, admissioncontroller.MutateJobs)
}

func main() {
	config := appConf.NewConfig()
	config.AddFlags()
	flag.Parse()

	http.HandleFunc(admissioncontroller.AdmitJobPath, serveJobs)
	http.HandleFunc(admissioncontroller.MutateJobPath, serveMutateJobs)

	if err := config.CheckPortOrDie(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	addr := ":" + strconv.Itoa(config.Port)

	clientset := app.GetClient(config)
	server := &http.Server{
		Addr:      addr,
		TLSConfig: app.ConfigTLS(config, clientset),
	}
	server.ListenAndServeTLS("", "")
}
