// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2023, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package up

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/MakeNowJust/heredoc"
	"github.com/compose-spec/compose-go/types"
	"github.com/spf13/cobra"

	"kraftkit.sh/cmdfactory"
	"kraftkit.sh/compose"
	"kraftkit.sh/internal/cli/kraft/build"
	"kraftkit.sh/internal/cli/kraft/pkg"
	"kraftkit.sh/internal/cli/kraft/pkg/pull"
	"kraftkit.sh/internal/cli/kraft/run"
	"kraftkit.sh/log"
	"kraftkit.sh/packmanager"
	"kraftkit.sh/unikraft"

	machineapi "kraftkit.sh/api/machine/v1alpha1"
	mplatform "kraftkit.sh/machine/platform"
)

type UpOptions struct {
	composefile string
}

func NewCmd() *cobra.Command {
	cmd, err := cmdfactory.New(&UpOptions{}, cobra.Command{
		Short: "Run a compose project.",
		Use:   "up",
		Long: heredoc.Doc(`
			Run a compose project	
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

func (opts *UpOptions) Pre(cmd *cobra.Command, _ []string) error {
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

func (opts *UpOptions) Run(ctx context.Context, args []string) error {
	project, err := compose.NewProjectFromComposeFile(ctx, opts.composefile)
	if err != nil {
		return err
	}

	if err := project.Validate(ctx); err != nil {
		return err
	}

	// Check that none of the services are already running
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
				return fmt.Errorf("service %s already running or exited", service.Name)
			}
		}
	}

	for _, service := range project.Services {
		if err := ensureServiceIsPackaged(ctx, service); err != nil {
			return err
		}
	}

	longestName := 0
	for _, service := range project.Services {
		if len(service.Name) > longestName {
			longestName = len(service.Name)
		}
	}

	var wg sync.WaitGroup

	for i := range project.Services {
		wg.Add(1)

		go func(service types.ServiceConfig) {
			defer wg.Done()

			if err := runService(ctx, service, longestName); err != nil {
				log.G(ctx).WithError(err).Errorf("failed to run service %s", service.Name)
			}
		}(project.Services[i])
	}

	wg.Wait()

	return nil
}

func platArchFromService(service types.ServiceConfig) (string, string, error) {
	// The service platform should be in the form <platform>/<arch>

	parts := strings.SplitN(service.Platform, "/", 2)

	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid platform: %s for service %s", service.Platform, service.Name)
	}

	// Check that the platform is supported
	if _, ok := mplatform.PlatformsByName()[parts[0]]; !ok {
		return "", "", fmt.Errorf("unsupported platform: %s for service %s", parts[0], service.Name)
	}

	if parts[1] != "x86_64" && parts[1] != "amd64" && parts[1] != "arm32" && parts[1] != "arm64" {
		return "", "", fmt.Errorf("unsupported architecture: %s for service %s", parts[1], service.Name)
	}

	return parts[0], parts[1], nil
}

func ensureServiceIsPackaged(ctx context.Context, service types.ServiceConfig) error {
	plat, arch, err := platArchFromService(service)
	if err != nil {
		return err
	}

	parts := strings.SplitN(service.Image, ":", 2)
	imageName := parts[0]
	imageVersion := "latest"
	if len(parts) == 2 {
		imageVersion = parts[1]
	}

	service.Image = imageName + ":" + imageVersion

	log.G(ctx).Debugf("Searching for service %s locally...", service.Name)
	// Check whether the image is already in the local catalog
	packages, err := packmanager.G(ctx).Catalog(ctx,
		packmanager.WithTypes(unikraft.ComponentTypeApp),
		packmanager.WithName(imageName),
		packmanager.WithVersion(imageVersion),
		packmanager.WithPlatform(plat),
		packmanager.WithArchitecture(arch),
		packmanager.WithUpdate(false))
	if err != nil {
		return err
	}

	// If we have it locally, we are done
	if len(packages) != 0 {
		log.G(ctx).Debugf("Found service %s locally", service.Name)
		return nil
	}

	log.G(ctx).Debugf("Searching for service %s remotely...", service.Name)
	// Check whether the image is in the remote catalog
	packages, err = packmanager.G(ctx).Catalog(ctx,
		packmanager.WithTypes(unikraft.ComponentTypeApp),
		packmanager.WithName(imageName),
		packmanager.WithVersion(imageVersion),
		packmanager.WithPlatform(plat),
		packmanager.WithArchitecture(arch),
		packmanager.WithUpdate(true))

	if err != nil {
		return err
	}

	// If we have it remotely, we are done
	if len(packages) != 0 {
		log.G(ctx).Infof("Found service %s remotely, pulling...", service.Name)
		// We need to pull it locally
		pullOptions := pull.PullOptions{Platform: plat, Architecture: arch}
		return pullOptions.Run(ctx, []string{service.Image})
	}

	// Otherwise, we need to build and package it
	if err := buildService(ctx, service); err != nil {
		return err
	}

	return pkgService(ctx, service)
}

func buildService(ctx context.Context, service types.ServiceConfig) error {
	if service.Build == nil {
		return fmt.Errorf("service %s has no build context", service.Name)
	}

	plat, arch, err := platArchFromService(service)
	if err != nil {
		return err
	}

	log.G(ctx).Infof("Building service %s...", service.Name)

	buildOptions := build.BuildOptions{Platform: plat, Architecture: arch}

	return buildOptions.Run(ctx, []string{service.Build.Context})
}

func pkgService(ctx context.Context, service types.ServiceConfig) error {
	plat, arch, err := platArchFromService(service)
	if err != nil {
		return err
	}

	log.G(ctx).Infof("Packaging service %s...", service.Name)

	pkgOptions := pkg.PkgOptions{Platform: plat, Architecture: arch, Name: service.Image, Strategy: packmanager.StrategyOverwrite, Format: "oci"}

	return pkgOptions.Run(ctx, []string{service.Build.Context})
}

func runService(ctx context.Context, service types.ServiceConfig, prefixLength int) error {
	// The service should be packaged at this point
	plat, arch, err := platArchFromService(service)
	if err != nil {
		return err
	}

	log.G(ctx).Infof("Running service %s...", service.Name)

	prefix := service.Name + strings.Repeat(" ", prefixLength-len(service.Name))

	runOptions := run.RunOptions{RunAs: "oci", Platform: plat, Architecture: arch, Name: service.Name, Prefix: prefix}

	return runOptions.Run(ctx, []string{service.Image})
}
