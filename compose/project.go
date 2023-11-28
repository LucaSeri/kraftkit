// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2023, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package compose provides primitives for running Unikraft applications
// via the Compose specification.
package compose

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/compose-spec/compose-go/loader"
	"github.com/compose-spec/compose-go/types"

	"kraftkit.sh/log"
	"kraftkit.sh/machine/network/bridge"
	mplatform "kraftkit.sh/machine/platform"
	ukarch "kraftkit.sh/unikraft/arch"
)

type Project struct {
	*types.Project `json:"project"` // The underlying compose-go project
}

// DefaultFileNames is a list of default compose file names to look for
var DefaultFileNames = []string{
	"docker-compose.yml",
	"docker-compose.yaml",
	"compose.yml",
	"compose.yaml",
	"Composefile",
}

// NewProjectFromComposeFile loads a compose file and returns a project. If no
// compose file is specified, it will look for one in the current directory.
func NewProjectFromComposeFile(ctx context.Context, composeFile string) (*Project, error) {
	if composeFile == "" {
		for _, file := range DefaultFileNames {
			if _, err := os.Stat(file); err == nil {
				log.G(ctx).Debugf("Found compose file: %s", file)
				composeFile = file
				break
			}
		}
	}

	if composeFile == "" {
		return nil, fmt.Errorf("no compose file found")
	}

	configfile := types.ConfigFile{
		Filename: composeFile,
	}

	config := types.ConfigDetails{
		ConfigFiles: []types.ConfigFile{configfile},
	}

	project, err := loader.Load(config)
	if err != nil {
		return nil, err
	}

	return &Project{project}, err
}

// Validate performs some early checks on the project to ensure it is valid,
// as well as fill in some unspecified fields.
func (project *Project) Validate(ctx context.Context) error {
	// Check that each service has at least an image name or a build context
	for _, service := range project.Services {
		if service.Image == "" && service.Build == nil {
			return fmt.Errorf("service %s has neither an image nor a build context", service.Name)
		}
	}

	// If the project has no name, use the directory name
	if project.Name == "" {
		// Take the last part of the working directory
		parts := strings.Split(project.WorkingDir, "/")
		project.Name = parts[len(parts)-1]
	}

	// Fill in any missing image names and prepend the project name
	for i, service := range project.Services {
		if service.Image == "" {
			project.Services[i].Image = fmt.Sprint(project.Name, "-", service.Name)
		}

		project.Services[i].Name = fmt.Sprint(project.Name, "-", service.Name)
	}

	// Fill in any missing platforms
	for i, service := range project.Services {
		if service.Platform == "" {
			hostPlatform, _, err := mplatform.Detect(ctx)
			if err != nil {
				return err
			}

			hostArch, err := ukarch.HostArchitecture()
			if err != nil {
				return err
			}

			project.Services[i].Platform = fmt.Sprint(hostPlatform, "/", hostArch)

		}
	}

	// Go through the network and fill in the project name where needed
	for i, network := range project.Networks {
		if network.Name[0] == '_' {
			network.Name = project.Name + network.Name
			project.Networks[i] = network
		}
	}

	// Remove the default network
	delete(project.Networks, "default")
	usedAddrs := make(map[string]map[string]bool)

	// Currently we need to specify the network driver and IPAM config
	// manually, so check we have that. This can be improved in the future
	// by defaulting in a smart way.
	for i, network := range project.Networks {
		if network.Driver == "" {
			network.Driver = network.Ipam.Driver
		}
		if network.Driver == "" {
			return fmt.Errorf("network %s has no driver specified", network.Name)
		}
		if network.Ipam.Config == nil || len(network.Ipam.Config) == 0 {
			return fmt.Errorf("network %s has no IPAM config specified", network.Name)
		}

		// Join all the IPAM configs together
		ipamConfig := network.Ipam.Config[0]
		for _, config := range network.Ipam.Config[1:] {
			if config.Subnet != "" {
				ipamConfig.Subnet = config.Subnet
			}
			if config.Gateway != "" {
				ipamConfig.Gateway = config.Gateway
			}
		}

		if ipamConfig.Subnet == "" {
			return fmt.Errorf("network %s has no subnet specified", network.Name)
		}

		// Check that the subnet is of type addr/subnet
		if len(strings.Split(ipamConfig.Subnet, "/")) != 2 {
			return fmt.Errorf("network %s has an invalid subnet specified", network.Name)
		}

		subnetIP, subnetMask, err := net.ParseCIDR(ipamConfig.Subnet)
		if err != nil {
			return fmt.Errorf("failed to parse %s network subnet", network.Name)
		}

		if subnetMask == nil {
			return fmt.Errorf("failed to parse network %s subnet mask", network.Name)
		}

		// Check that the gateway is of type addr
		if ipamConfig.Gateway == "" {
			ipamConfig.Gateway = subnetIP.String()
		} else {
			// Additionally check the gateway is part of the subnet
			gatewayIP := net.ParseIP(ipamConfig.Gateway)
			if gatewayIP == nil {
				return fmt.Errorf("failed to parse %s network gateway", network.Name)
			}

			if !subnetMask.Contains(gatewayIP) {
				return fmt.Errorf("network %s gateway is not within the subnet", network.Name)
			}
		}

		usedAddrs[i] = make(map[string]bool)
		usedAddrs[i][ipamConfig.Gateway] = true
		usedAddrs[i][subnetMask.IP.String()] = true
		network.Ipam.Config[0] = ipamConfig
		project.Networks[i] = network
	}

	// Check the services networks
	for _, service := range project.Services {
		if service.Networks == nil {
			continue
		}
		delete(service.Networks, "default")
		for name, network := range service.Networks {
			if _, ok := project.Networks[name]; !ok {
				return fmt.Errorf("service %s references non-existent network %s", service.Name, name)
			}

			if network == nil {
				service.Networks[name] = &types.ServiceNetworkConfig{}
				network = service.Networks[name]
			}

			// If the network has an ipv4_address specified, check it is
			// valid and not already used
			if network.Ipv4Address != "" {
				ip := net.ParseIP(network.Ipv4Address)
				if ip == nil {
					return fmt.Errorf("service %s has an invalid ipv4_address specified", service.Name)
				}

				if usedAddrs[name][ip.String()] {
					return fmt.Errorf("service %s has an ipv4_address that is already in use", service.Name)
				}

				usedAddrs[name][ip.String()] = true
			}
		}
	}

	// Run through the services again and assign an IP address to each
	for i, service := range project.Services {
		for name, network := range service.Networks {
			if network == nil {
				continue
			}
			if network.Ipv4Address == "" {
				// Start at the network's subnet IP and increment until we find
				// a free one
				_, subnet, err := net.ParseCIDR(project.Networks[name].Ipam.Config[0].Subnet)
				if err != nil {
					return err
				}

				if subnet == nil {
					// This should not be possible
					return fmt.Errorf("failed to parse network %s subnet", name)
				}

				ip := subnet.IP

				for subnet.Contains(ip) && usedAddrs[name][ip.String()] {
					ip = bridge.IncreaseIP(ip)
				}

				if !subnet.Contains(ip) {
					return fmt.Errorf("not enough free IP addresses in network %s", name)
				}

				project.Services[i].Networks[name].Ipv4Address = ip.String()
				usedAddrs[name][ip.String()] = true
			}
		}
	}
	return nil
}
