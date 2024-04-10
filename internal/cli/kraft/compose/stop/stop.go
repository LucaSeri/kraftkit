// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2024, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.
package stop

import (
	"context"
	"fmt"
	"os"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"kraftkit.sh/cmdfactory"
	"kraftkit.sh/compose"
	"kraftkit.sh/config"
	"kraftkit.sh/log"
	"kraftkit.sh/packmanager"
	"kraftkit.sh/tui/processtree"

	machineapi "kraftkit.sh/api/machine/v1alpha1"
	kernelstop "kraftkit.sh/internal/cli/kraft/stop"
	mplatform "kraftkit.sh/machine/platform"
)

type StopOptions struct {
	composefile string
}

func NewCmd() *cobra.Command {
	cmd, err := cmdfactory.New(&StopOptions{}, cobra.Command{
		Short:   "Stop a compose project",
		Use:     "stop [FLAGS]",
		Args:    cobra.NoArgs,
		Aliases: []string{},
		Example: heredoc.Doc(`
			# Stop a compose project
			$ kraft compose stop 
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

func (opts *StopOptions) Pre(cmd *cobra.Command, _ []string) error {
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

func (opts *StopOptions) Run(ctx context.Context, _ []string) error {
	workdir, err := os.Getwd()
	if err != nil {
		return err
	}

	project, err := compose.NewProjectFromComposeFile(ctx, workdir, opts.composefile)
	if err != nil {
		return err
	}

	if err := project.Validate(ctx); err != nil {
		return err
	}

	machineController, err := mplatform.NewMachineV1alpha1ServiceIterator(ctx)
	if err != nil {
		return err
	}

	machines, err := machineController.List(ctx, &machineapi.MachineList{})
	if err != nil {
		return err
	}

	topLevelRender := log.LoggerTypeFromString(config.G[config.KraftKit](ctx).Log.Type) != log.FANCY
	oldLogType := config.G[config.KraftKit](ctx).Log.Type
	config.G[config.KraftKit](ctx).Log.Type = log.LoggerTypeToString(log.BASIC)
	defer func() {
		config.G[config.KraftKit](ctx).Log.Type = oldLogType
	}()

	processes := make([]*processtree.ProcessTreeItem, 0)
	for _, service := range project.Services {
		for _, machine := range machines.Items {
			if service.Name == machine.Name &&
				(machine.Status.State == machineapi.MachineStateRunning ||
					machine.Status.State == machineapi.MachineStatePaused) {
				processes = append(processes, processtree.NewProcessTreeItem(
					fmt.Sprintf("stopping service %s", service.Name),
					"",
					func(ctx context.Context) error {
						kernelStopOptions := kernelstop.StopOptions{
							Platform: "auto",
						}

						return kernelStopOptions.Run(ctx, []string{machine.Name})
					},
				))
			}
		}
	}

	if len(processes) == 0 {
		return nil
	}

	model, err := processtree.NewProcessTree(ctx,
		[]processtree.ProcessTreeOption{
			processtree.IsParallel(false),
			processtree.WithHideOnSuccess(false),
			processtree.WithRenderer(topLevelRender),
		},
		processes...,
	)
	if err != nil {
		return err
	}

	return model.Start()
}
