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

package deploy

import (
	"fmt"
	"github.com/dynatrace/dynatrace-configuration-as-code/internal/idutils"
	"github.com/dynatrace/dynatrace-configuration-as-code/internal/log"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/api"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/client"
	config "github.com/dynatrace/dynatrace-configuration-as-code/pkg/config/v2"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/config/v2/parameter"
)

// DeployConfigsOptions defines additional options used by DeployConfigs
type DeployConfigsOptions struct {
	ContinueOnErr bool
	DryRun        bool
}

// DeployConfigs deploys the given configs with the given apis via the given client
// NOTE: the given configs need to be sorted, otherwise deployment will
// probably fail, as references cannot be resolved
func DeployConfigs(client client.Client, apis api.ApiMap,
	sortedConfigs []config.Config, opts DeployConfigsOptions) []error {

	entityMap := NewEntityMap(apis)
	var errors []error

	for _, c := range sortedConfigs {
		c := c // to avoid implicit memory aliasing (gosec G601)

		if c.Skip {
			log.Info("\tSkipping deployment of config %s", c.Coordinate)

			entityMap.PutResolved(c.Coordinate, parameter.ResolvedEntity{
				EntityName: c.Coordinate.ConfigId,
				Coordinate: c.Coordinate,
				Properties: parameter.Properties{},
				Skip:       true,
			})
			continue
		}

		logAction, logVerb := getWordsForLogging(opts.DryRun)
		log.Info("\t%s config %s", logAction, c.Coordinate)

		var entity parameter.ResolvedEntity
		var deploymentErrors []error

		switch {
		case c.Type.IsEntities():
			log.Debug("Entities are not deployable, skipping entity type: %s", c.Type.EntitiesType)
		case c.Type.IsSettings():
			entity, deploymentErrors = deploySetting(client, entityMap, &c)
		default:
			entity, deploymentErrors = deployConfig(client, apis, entityMap, &c)
		}

		if deploymentErrors != nil {
			for _, err := range deploymentErrors {
				errors = append(errors, fmt.Errorf("failed to %s config %s: %w", logVerb, c.Coordinate, err))
			}

			if !opts.ContinueOnErr && !opts.DryRun {
				return errors
			}
		}
		entityMap.PutResolved(entity.Coordinate, entity)
	}

	return errors
}

// getWordsForLogging returns fitting action and verb words to clearly tell a user if configuration is
// deployed or validated when logging based on the dry-run boolean
func getWordsForLogging(isDryRun bool) (action, verb string) {
	if isDryRun {
		return "Validating", "validate"
	}
	return "Deploying", "deploy"
}

func deployConfig(client client.ConfigClient, apis api.ApiMap, entityMap *EntityMap, conf *config.Config) (parameter.ResolvedEntity, []error) {

	apiToDeploy := apis[conf.Coordinate.Type]
	if apiToDeploy == nil {
		return parameter.ResolvedEntity{}, []error{fmt.Errorf("unknown api `%s`. this is most likely a bug", conf.Type.Api)}
	}

	properties, errors := resolveProperties(conf, entityMap.Resolved())
	if len(errors) > 0 {
		return parameter.ResolvedEntity{}, errors
	}

	configName, err := extractConfigName(conf, properties)
	if err != nil {
		errors = append(errors, err)
	} else {
		if entityMap.Known(apiToDeploy.GetId(), configName) && !apiToDeploy.IsNonUniqueNameApi() {
			errors = append(errors, newConfigDeployErr(conf, fmt.Sprintf("duplicated config name `%s`", configName)))
		}
	}
	if len(errors) > 0 {
		return parameter.ResolvedEntity{}, errors
	}

	renderedConfig, err := conf.Render(properties)
	if err != nil {
		return parameter.ResolvedEntity{}, []error{err}
	}

	if apiToDeploy.DeprecatedBy() != "" {
		log.Warn("API for \"%s\" is deprecated! Please consider migrating to \"%s\"!", apiToDeploy.GetId(), apiToDeploy.DeprecatedBy())
	}

	var entity api.DynatraceEntity
	if apiToDeploy.IsNonUniqueNameApi() {
		entity, err = upsertNonUniqueNameConfig(client, apiToDeploy, conf, configName, renderedConfig)
	} else {
		entity, err = client.UpsertConfigByName(apiToDeploy, configName, []byte(renderedConfig))
	}

	if err != nil {
		return parameter.ResolvedEntity{}, []error{newConfigDeployErr(conf, err.Error())}
	}

	properties[config.IdParameter] = entity.Id
	properties[config.NameParameter] = entity.Name

	return parameter.ResolvedEntity{
		EntityName: entity.Name,
		Coordinate: conf.Coordinate,
		Properties: properties,
		Skip:       false,
	}, nil
}

func upsertNonUniqueNameConfig(client client.ConfigClient, apiToDeploy api.Api, conf *config.Config, configName string, renderedConfig string) (api.DynatraceEntity, error) {
	configId := conf.Coordinate.ConfigId
	projectId := conf.Coordinate.Project

	entityUuid := configId

	isUuidOrMeId := idutils.IsUuid(entityUuid) || idutils.IsMeId(entityUuid)
	if !isUuidOrMeId {
		entityUuid = idutils.GenerateUuidFromConfigId(projectId, configId)
	}

	return client.UpsertConfigByNonUniqueNameAndId(apiToDeploy, entityUuid, configName, []byte(renderedConfig))
}

func deploySetting(settingsClient client.SettingsClient, entityMap *EntityMap, c *config.Config) (parameter.ResolvedEntity, []error) {
	properties, errors := resolveProperties(c, entityMap.Resolved())
	if len(errors) > 0 {
		return parameter.ResolvedEntity{}, errors
	}

	scope, err := extractScope(properties)
	if err != nil {
		return parameter.ResolvedEntity{}, []error{err}
	}

	renderedConfig, err := c.Render(properties)
	if err != nil {
		return parameter.ResolvedEntity{}, []error{err}
	}

	entity, err := settingsClient.UpsertSettings(client.SettingsObject{
		Id:             c.Coordinate.ConfigId,
		SchemaId:       c.Type.SchemaId,
		SchemaVersion:  c.Type.SchemaVersion,
		Scope:          scope,
		Content:        []byte(renderedConfig),
		OriginObjectId: c.OriginObjectId,
	})
	if err != nil {
		return parameter.ResolvedEntity{}, []error{newConfigDeployErr(c, err.Error())}
	}

	properties[config.IdParameter] = entity.Id
	properties[config.NameParameter] = entity.Name

	return parameter.ResolvedEntity{
		EntityName: entity.Name,
		Coordinate: c.Coordinate,
		Properties: properties,
		Skip:       false,
	}, nil

}

func extractScope(properties parameter.Properties) (string, error) {
	scope, ok := properties[config.ScopeParameter]
	if !ok {
		return "", fmt.Errorf("property '%s' not found, this is most likely a bug", config.ScopeParameter)
	}

	if scope == "" {
		return "", fmt.Errorf("resolved scope is empty")
	}

	return fmt.Sprint(scope), nil
}
