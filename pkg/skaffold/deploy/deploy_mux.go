/*
Copyright 2019 The Skaffold Authors

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

package deploy

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/build"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubernetes/manifest"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/util"
)

// DeployerMux forwards all method calls to the deployers it contains.
// When encountering an error, it aborts and returns the error. Otherwise,
// it collects the results and returns it in bulk.
type DeployerMux []Deployer

// Channel-friendly structure for Deploy's return signature
type ret struct {
	ns []string
	e  error
}

func (m DeployerMux) Deploy(ctx context.Context, w io.Writer, as []build.Artifact) ([]string, error) {
	retChan := make(chan ret)
	seenNamespaces := util.NewStringSet()

	for _, deployer := range m {
		go wrapDeploy(ctx, w, as, deployer, retChan)
	}
	for range m {
		ret := <-retChan
		namespaces, err := ret.ns, ret.e
		if err != nil {
			return nil, err
		}
		seenNamespaces.Insert(namespaces...)
	}

	return seenNamespaces.ToList(), nil
}

func (m DeployerMux) Dependencies() ([]string, error) {
	deps := util.NewStringSet()
	for _, deployer := range m {
		result, err := deployer.Dependencies()
		if err != nil {
			return nil, err
		}
		deps.Insert(result...)
	}
	return deps.ToList(), nil
}

func (m DeployerMux) Cleanup(ctx context.Context, w io.Writer) error {
	errChan := make(chan error)

	for _, deployer := range m {
		go wrapCleanup(ctx, w, deployer, errChan)
	}
	for range m {
		if err := <-errChan; err != nil {
			return err
		}
	}
	return nil
}

func (m DeployerMux) Render(ctx context.Context, w io.Writer, as []build.Artifact, offline bool, filepath string) error {
	resources, buf := []string{}, &bytes.Buffer{}
	for _, deployer := range m {
		buf.Reset()
		if err := deployer.Render(ctx, buf, as, offline, "" /* never write to files */); err != nil {
			return err
		}
		resources = append(resources, buf.String())
	}

	allResources := strings.Join(resources, "\n---\n")
	return manifest.Write(allResources, filepath, w)
}

func wrapDeploy(
	ctx context.Context,
	w io.Writer,
	as []build.Artifact,
	d Deployer,
	q chan<- ret) {
	ns, e := d.Deploy(ctx, w, as)
	q <- ret{ns, e}
}

func wrapCleanup(
	ctx context.Context,
	w io.Writer,
	d Deployer,
	err chan<- error) {
	err <- d.Cleanup(ctx, w)
}
