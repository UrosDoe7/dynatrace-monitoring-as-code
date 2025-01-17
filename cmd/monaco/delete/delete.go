// @license
// Copyright 2021 Dynatrace LLC
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package delete

import (
	"errors"
	"fmt"
	"github.com/dynatrace/dynatrace-configuration-as-code/internal/errutils"
	"github.com/dynatrace/dynatrace-configuration-as-code/internal/log"
	"github.com/dynatrace/dynatrace-configuration-as-code/internal/maps"
	"path/filepath"
	"strings"

	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/api"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/client"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/delete"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/manifest"
	"github.com/spf13/afero"
)

func Delete(fs afero.Fs, deploymentManifestPath string, deletePath string, environmentNames []string, environmentGroup string) error {

	deploymentManifestPath = filepath.Clean(deploymentManifestPath)
	deploymentManifestPath, manifestErr := filepath.Abs(deploymentManifestPath)
	deletePath = filepath.Clean(deletePath)
	deletePath, deleteErr := filepath.Abs(deletePath)
	deleteFileWorkingDir := strings.ReplaceAll(deletePath, "delete.yaml", "")
	deleteFile := "delete.yaml"

	if manifestErr != nil {
		return fmt.Errorf("error while finding absolute path for `%s`: %w", deploymentManifestPath, manifestErr)
	}

	if deleteErr != nil {
		return fmt.Errorf("error while finding absolute path for `%s`: %w", deletePath, deleteErr)
	}

	apis := api.NewApis()

	manifest, manifestLoadError := manifest.LoadManifest(&manifest.ManifestLoaderContext{
		Fs:           fs,
		ManifestPath: deploymentManifestPath,
	})

	if manifestLoadError != nil {
		errutils.PrintErrors(manifestLoadError)
		return errors.New("error while loading manifest")
	}

	entriesToDelete, errs := delete.LoadEntriesToDelete(fs, api.GetApiNames(apis), deleteFileWorkingDir, deleteFile)
	if errs != nil {
		return fmt.Errorf("encountered errors while parsing delete.yaml: %s", errs)
	}

	environments := manifest.Environments
	if environmentGroup != "" {
		environments = environments.FilterByGroup(environmentGroup)

		if len(environments) == 0 {
			return fmt.Errorf("no environments in group %q", environmentGroup)
		} else {
			log.Info("Environments loaded in group %q: %v", environmentGroup, maps.Keys(environments))
		}
	}

	if len(environmentNames) > 0 {
		var err error
		environments, err = manifest.Environments.FilterByNames(environmentNames)
		if err != nil {
			return fmt.Errorf("failed to load environments: %w", err)
		}
	}

	deleteErrors := deleteConfigs(maps.Values(environments), apis, entriesToDelete)

	for _, e := range deleteErrors {
		log.Error("Deletion error: %s", e)
	}
	if len(deleteErrors) > 0 {
		return fmt.Errorf("encountered %v errors during delete", len(deleteErrors))
	}
	return nil
}

func deleteConfigs(environments []manifest.EnvironmentDefinition, apis map[string]api.Api, entriesToDelete map[string][]delete.DeletePointer) (errors []error) {

	for _, env := range environments {
		deleteErrors := deleteConfigForEnvironment(env, apis, entriesToDelete)

		if deleteErrors != nil {
			errors = append(errors, deleteErrors...)
		}
	}

	return errors
}

func deleteConfigForEnvironment(env manifest.EnvironmentDefinition, apis map[string]api.Api, entriesToDelete map[string][]delete.DeletePointer) []error {
	dynatraceClient, err := createClient(env, false)

	if err != nil {
		return []error{
			fmt.Errorf("It was not possible to create a client for env `%s` due to the following error: %w", env.Name, err),
		}
	}

	log.Info("Deleting configs for environment `%s`", env.Name)

	return delete.DeleteConfigs(dynatraceClient, apis, entriesToDelete)
}

func createClient(environment manifest.EnvironmentDefinition, dryRun bool) (client.Client, error) {
	if dryRun {
		return &client.DummyClient{}, nil
	}

	token, err := environment.GetToken()

	if err != nil {
		return nil, err
	}

	url, err := environment.GetUrl()
	if err != nil {
		return nil, err
	}

	return client.NewDynatraceClient(url, token)
}
