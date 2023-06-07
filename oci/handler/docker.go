// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2022, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type DockerHandler struct {
	client *client.Client
	// cli    *command.DockerCli
}

func NewDockerHandler(ctx context.Context, addr string) (context.Context, *DockerHandler, error) {
	addr = "unix:///var" + addr
	client, err := client.NewClientWithOpts(client.FromEnv, client.WithHost(addr), client.WithVersion("1.42"))
	if err != nil {
		fmt.Printf("Error creating docker client: %s\n", err)
		return nil, nil, err
	}

	return ctx, &DockerHandler{
		client: client,
	}, nil
}

func (handle *DockerHandler) DigestExists(ctx context.Context, digest digest.Digest) (bool, error) {
	// Get the content store of the docker daemon

	if _, _, err := handle.client.ImageInspectWithRaw(ctx, digest.String()); err != nil {
		return false, err
	}

	return true, nil
}

func (handle *DockerHandler) ListManifests(ctx context.Context) ([]ocispec.Manifest, error) {
	all, err := handle.client.ImageList(ctx, types.ImageListOptions{All: true})
	if err != nil {
		return nil, err
	}

	var manifests []ocispec.Manifest
	for _, container := range all {
		// TODO: Check what needs to be filled in here
		manifests = append(manifests, ocispec.Manifest{
			Config: ocispec.Descriptor{
				MediaType: ocispec.MediaTypeImageConfig,
				Digest:    digest.Digest(container.ID),
			},
		})
	}

	return manifests, nil
}

// Set up connection
// Look for the digest
//

// Push the descriptor to the socket
func (handle *DockerHandler) PushDigest(ctx context.Context, name string, desc ocispec.Descriptor, reader io.Reader, progress func(float64)) error {

	return nil
}

// Looks for an image
func (handle *DockerHandler) ResolveImage(ctx context.Context, name string) (ocispec.Image, error) {
	image, imageRaw, err := handle.client.ImageInspectWithRaw(ctx, name)
	if err != nil {
		return ocispec.Image{}, err
	}

	_ = image

	// Almost works
	img := ocispec.Image{}
	json.Unmarshal(imageRaw, &img)

	img.RootFS.DiffIDs = []digest.Digest{}
	for _, layer := range image.RootFS.Layers {
		img.RootFS.DiffIDs = append(img.RootFS.DiffIDs, digest.Digest(layer))
	}

	return img, nil
}

// type ProgressDetail struct {
// 	Current int `json:"current"`
// 	Total   int `json:"total"`
// }

// type ErrorDetail struct {
// 	Message string `json:"message"`
// }

// type PullProgress struct {
// 	ErrorDetail    ErrorDetail    `json:"errorDetail"`
// 	Error          string         `json:"error"`
// 	Status         string         `json:"status"`
// 	Progress       string         `json:"progress"`
// 	ProgressDetail ProgressDetail `json:"progressDetail"`
// }

// If it is not present locally
func (handle *DockerHandler) FetchImage(ctx context.Context, name string, progress func(float64)) error {
	// rc, err := handle.client.ImagePull(ctx, name, types.ImagePullOptions{Platform: "kvm/x86_64"})
	rc, err := handle.client.ImagePull(ctx, name, types.ImagePullOptions{})

	if err != nil {
		fmt.Printf("Error pulling image: %v\n", err)
		return err
	}

	decoder := json.NewDecoder(rc)

	for {
		var currentProgress jsonmessage.JSONMessage

		err := decoder.Decode(&currentProgress)
		fmt.Printf("%+v\n", currentProgress)
		if err != nil {
			if err == io.EOF {
				break
			}
		}

		if currentProgress.Progress != nil && currentProgress.Progress.Total > 0 && currentProgress.Progress.Current > 0 {
			progress(float64(currentProgress.Progress.Current) / float64(currentProgress.Progress.Total))
		}
	}

	rc.Close()

	return err
}

// Unpack the fetched image
func (handle *DockerHandler) UnpackImage(ctx context.Context, name, path string) error {
	return nil
}

func (handle *DockerHandler) PushImage(ctx context.Context, name string, desc *ocispec.Descriptor) error {

	return nil
}
