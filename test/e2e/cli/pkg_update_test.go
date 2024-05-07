// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2023, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cli_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2" //nolint:stylecheck
	. "github.com/onsi/gomega"    //nolint:stylecheck

	"sigs.k8s.io/kustomize/kyaml/yaml"

	fcmd "kraftkit.sh/test/e2e/framework/cmd"
	fcfg "kraftkit.sh/test/e2e/framework/config"
	. "kraftkit.sh/test/e2e/framework/matchers" //nolint:stylecheck
)

var _ = Describe("kraft pkg", func() {
	var cmd *fcmd.Cmd

	var stdout *fcmd.IOStream
	var stderr *fcmd.IOStream

	var cfg *fcfg.Config

	var manifestsPath string

	BeforeEach(func() {
		stdout = fcmd.NewIOStream()
		stderr = fcmd.NewIOStream()

		cfg = fcfg.NewTempConfig()

		cmd = fcmd.NewKraft(stdout, stderr, cfg.Path())
		cmd.Args = append(cmd.Args, "pkg", "update", "--log-level", "info", "--log-type", "json")

		manifestsPath = yaml.GetValue(cfg.Read("paths", "manifests"))
		Expect(manifestsPath).To(SatisfyAny(
			Not(BeAnExistingFile()),
			BeAnEmptyDirectory(),
		), "manifests directory should either be empty or not yet created")
	})

	Context("implicitly using the default manager type (manifest)", func() {
		When("invoked without flags or positional arguments", func() {
			It("should retrieve the list of components, libraries and packages", func() {
				err := cmd.Run()
				if err != nil {
					fmt.Print(cmd.DumpError(stdout, stderr, err))
				}
				Expect(err).ToNot(HaveOccurred())

				Expect(stderr.String()).To(BeEmpty())
				Expect(stdout.String()).To(MatchRegexp(`{"level":"info","msg":"updating manifest index"}`))
				Expect(stdout.String()).To(MatchRegexp(`{"level":"info","msg":"updating oci index"}`))

				Expect(manifestsPath).To(ContainFiles("index.yaml", "unikraft.yaml"))
				Expect(manifestsPath).To(ContainDirectories("libs"))
			})
		})

		When("invoked with the --help flag", func() {
			BeforeEach(func() {
				cmd.Args = append(cmd.Args, "--help")
			})

			It("should print the command's help", func() {
				err := cmd.Run()
				if err != nil {
					fmt.Print(cmd.DumpError(stdout, stderr, err))
				}
				Expect(err).ToNot(HaveOccurred())

				Expect(stderr.String()).To(BeEmpty())
				Expect(stdout.String()).To(MatchRegexp(`^Retrieve new lists of Unikraft components, libraries and packages.\n`))
			})
		})
	})
})
