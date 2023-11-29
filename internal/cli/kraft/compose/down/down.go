// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2023, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package down

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"github.com/compose-spec/compose-go/types"

	"github.com/spf13/cobra"
	"kraftkit.sh/cmdfactory"
	"kraftkit.sh/compose"
	netremove "kraftkit.sh/internal/cli/kraft/net/remove"
	"kraftkit.sh/internal/cli/kraft/remove"
	"kraftkit.sh/log"
	"kraftkit.sh/packmanager"

	machineapi "kraftkit.sh/api/machine/v1alpha1"
	networkapi "kraftkit.sh/api/network/v1alpha1"
	"kraftkit.sh/machine/network"
	mplatform "kraftkit.sh/machine/platform"
)

type DownOptions struct {
	composefile string
}

func NewCmd() *cobra.Command {
	cmd, err := cmdfactory.New(&DownOptions{}, cobra.Command{
		Short: "Run a compose project.",
		Use:   "down",
		Long: heredoc.Doc(`
			Stop and remove a compose project	
		`),
		Annotations: map[string]string{
			cmdfactory.AnnotationHelpGroup: "compose",
		},
	})
	if err != nil {
		panic(err)
	}

	return cmd
}

func (opts *DownOptions) Pre(cmd *cobra.Command, _ []string) error {
	ctx, err := packmanager.WithDefaultUmbrellaManagerInContext(cmd.Context())
	if err != nil {
		return err
	}

	cmd.SetContext(ctx)

	if cmd.Flag("file").Changed {
		opts.composefile = cmd.Flag("file").Value.String()
	}

	log.G(cmd.Context()).WithField("composefile", opts.composefile).Debug("using")
	return nil
}

func (opts *DownOptions) Run(ctx context.Context, args []string) error {
	project, err := compose.NewProjectFromComposeFile(ctx, opts.composefile)
	if err != nil {
		return err
	}

	if err := project.Validate(ctx); err != nil {
		return err
	}

	controller, err := mplatform.NewMachineV1alpha1ServiceIterator(ctx)
	if err != nil {
		return err
	}

	machines, err := controller.List(ctx, &machineapi.MachineList{})
	if err != nil {
		return err
	}

	for _, service := range project.Services {
		for _, machine := range machines.Items {
			if service.Name == machine.Name {
				if err := removeService(ctx, service); err != nil {
					return err
				}
			}
		}
	}

	driverNetworks := make(map[string][]string)

	for _, projectNetwork := range project.Networks {
		if _, ok := driverNetworks[projectNetwork.Driver]; !ok {
			strategy, ok := network.Strategies()[projectNetwork.Driver]
			if !ok {
				return fmt.Errorf("unsupported network driver strategy: %s", projectNetwork.Driver)
			}

			controller, err := strategy.NewNetworkV1alpha1(ctx)
			if err != nil {
				return err
			}

			networks, err := controller.List(ctx, &networkapi.NetworkList{})
			if err != nil {
				return err
			}

			driverNetworks[projectNetwork.Driver] = []string{}

			for _, network := range networks.Items {
				driverNetworks[projectNetwork.Driver] = append(driverNetworks[projectNetwork.Driver], network.Name)
			}
		}

		for _, existingNetwork := range driverNetworks[projectNetwork.Driver] {
			if projectNetwork.Name == existingNetwork {
				if err := removeNetwork(ctx, projectNetwork); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func removeService(ctx context.Context, service types.ServiceConfig) error {
	log.G(ctx).Infof("Removing service %s...", service.Name)
	removeOptions := remove.RemoveOptions{Platform: "auto"}

	return removeOptions.Run(ctx, []string{service.Name})
}

func removeNetwork(ctx context.Context, network types.NetworkConfig) error {
	log.G(ctx).Infof("Removing network %s...", network.Name)
	removeOptions := netremove.RemoveOptions{Driver: network.Driver}

	return removeOptions.Run(ctx, []string{network.Name})
}
