// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Devoplus
//
//    Licensed under the Apache License, Version 2.0 (the "License"); you may
//    not use this file except in compliance with the License. You may obtain
//    a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
//    WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
//    License for the specific language governing permissions and limitations
//    under the License.

// Command garm-provider-vultr is a GARM external provider that runs GitHub
// Actions runners on Vultr compute instances.
//
// GARM invokes this binary once per operation, passing the operation in the
// GARM_COMMAND environment variable and, for CreateInstance, the bootstrap
// params on stdin. The result is written to stdout as JSON.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudbase/garm-provider-common/execution"
	commonExecution "github.com/cloudbase/garm-provider-common/execution/common"

	"github.com/devoplus/garm-provider-vultr/provider"
)

var signals = []os.Signal{
	os.Interrupt,
	syscall.SIGTERM,
}

// Version is an alternative place to stamp the build version.
//
// The release scripts set provider.Version directly, but the GARM container
// image builds every provider with -X main.Version. Accepting both keeps the
// version reported to GARM correct whichever build is used.
var Version = ""

func main() {
	if Version != "" {
		provider.Version = Version
	}

	ctx, stop := signal.NotifyContext(context.Background(), signals...)
	defer stop()

	executionEnv, err := execution.GetEnvironment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read the execution environment: %s\n", err)
		os.Exit(1)
	}

	prov, err := provider.New(executionEnv.ProviderConfigFile, executionEnv.ControllerID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to set up the provider: %s\n", err)
		os.Exit(1)
	}

	result, err := executionEnv.Run(ctx, prov)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to run command: %s\n", err)
		// GARM reads a Not Found or a duplicate from the exit code, so the
		// error has to be mapped rather than exiting with a plain 1.
		os.Exit(commonExecution.ResolveErrorToExitCode(err))
	}

	if len(result) > 0 {
		fmt.Fprint(os.Stdout, result)
	}
}
