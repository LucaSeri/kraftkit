// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2024, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package reset

import (
	"context"
	"fmt"
	"os"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	kraftcloud "sdk.kraft.cloud"
	kraftcloudautoscale "sdk.kraft.cloud/services/autoscale"

	"kraftkit.sh/cmdfactory"
	"kraftkit.sh/config"
	"kraftkit.sh/internal/cli/kraft/cloud/utils"
	"kraftkit.sh/log"
)

type ResetOptions struct {
	Auth   *config.AuthConfig                   `noattribute:"true"`
	Client kraftcloudautoscale.AutoscaleService `noattribute:"true"`
	Metro  string                               `noattribute:"true"`
}

func NewCmd() *cobra.Command {
	cmd, err := cmdfactory.New(&ResetOptions{}, cobra.Command{
		Short:   "Reset autoscale configuration of a service group",
		Use:     "reset [FLAGS] UUID|NAME",
		Aliases: []string{"rs", "delconfig", "deinit", "rmconfig"},
		Long:    "Reset autoscale configuration of a service group.",
		Example: heredoc.Doc(`
			# Reset an autoscale configuration by UUID
			$ kraft cloud scale reset fd1684ea-7970-4994-92d6-61dcc7905f2b

			# Reset an autoscale configuration by name
			$ kraft cloud scale reset my-instance-431342
		`),
		Annotations: map[string]string{
			cmdfactory.AnnotationHelpGroup: "kraftcloud-scale",
		},
	})
	if err != nil {
		panic(err)
	}

	return cmd
}

func (opts *ResetOptions) Pre(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("specify a service group name or UUID")
	}

	opts.Metro = cmd.Flag("metro").Value.String()
	if opts.Metro == "" {
		opts.Metro = os.Getenv("KRAFTCLOUD_METRO")
	}
	if opts.Metro == "" {
		return fmt.Errorf("kraftcloud metro is unset")
	}

	log.G(cmd.Context()).WithField("metro", opts.Metro).Debug("using")

	return nil
}

func (opts *ResetOptions) Run(ctx context.Context, args []string) error {
	var err error

	if opts.Auth == nil {
		opts.Auth, err = config.GetKraftCloudAuthConfigFromContext(ctx)
		if err != nil {
			return fmt.Errorf("could not retrieve credentials: %w", err)
		}
	}

	if opts.Client == nil {
		opts.Client = kraftcloud.NewAutoscaleClient(
			kraftcloud.WithToken(config.GetKraftCloudTokenAuthConfig(*opts.Auth)),
		)
	}

	if utils.IsUUID(args[0]) {
		err = opts.Client.WithMetro(opts.Metro).DeleteConfigurationByUUID(ctx, args[0])
	} else {
		err = opts.Client.WithMetro(opts.Metro).DeleteConfigurationByName(ctx, args[0])
	}
	if err != nil {
		return fmt.Errorf("could not reset configuration: %w", err)
	}

	return err
}