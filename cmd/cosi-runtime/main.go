// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/cosi-project/runtime/api/v1alpha1"
	runtimeserver "github.com/cosi-project/runtime/pkg/controller/protobuf/server"
	"github.com/cosi-project/runtime/pkg/controller/runtime"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/cosi-project/runtime/pkg/state/protobuf/server"
)

var socketPath string

func main() {
	flag.StringVar(&socketPath, "socket-path", "/var/run/cosi-runtime.sock", "path to the UNIX socket to listen on")
	flag.Parse()

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, os.Interrupt)

		select {
		case <-sigCh:
		case <-ctx.Done():
		}
	}()

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("error setting up listening socket: %w", err)
	}

	inmemState := state.WrapCore(namespaced.NewState(inmem.Build))

	logger := log.New(log.Writer(), "controller-runtime: ", log.Flags())

	controllerRuntime, err := runtime.NewRuntime(inmemState, logger)
	if err != nil {
		return fmt.Errorf("error setting up controller runtime: %w", err)
	}

	grpcRuntime := runtimeserver.NewRuntime(controllerRuntime)

	grpcServer := grpc.NewServer()
	v1alpha1.RegisterStateServer(grpcServer, server.NewState(inmemState))
	v1alpha1.RegisterControllerRuntimeServer(grpcServer, grpcRuntime)
	v1alpha1.RegisterControllerAdapterServer(grpcServer, grpcRuntime)

	log.Printf("starting cosi-runtime service on %q", socketPath)

	var eg errgroup.Group

	eg.Go(func() error {
		return grpcServer.Serve(l)
	})

	<-ctx.Done()

	grpcServer.GracefulStop()

	return eg.Wait()
}
