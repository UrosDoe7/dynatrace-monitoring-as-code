/**
 * @license
 * Copyright 2020 Dynatrace LLC
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package settings

import (
	"encoding/json"
	"github.com/dynatrace/dynatrace-configuration-as-code/internal/idutils"
	"github.com/dynatrace/dynatrace-configuration-as-code/internal/log"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/client"
	config "github.com/dynatrace/dynatrace-configuration-as-code/pkg/config/v2"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/config/v2/coordinate"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/config/v2/parameter"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/config/v2/parameter/value"
	"github.com/dynatrace/dynatrace-configuration-as-code/pkg/config/v2/template"
	v2 "github.com/dynatrace/dynatrace-configuration-as-code/pkg/project/v2"
	"sync"
)

// Downloader is responsible for downloading Settings 2.0 objects
type Downloader struct {
	// client is the settings 2.0 client to be used by the Downloader
	client client.SettingsClient

	// filters specifies which settings 2.0 objects need special treatment under
	// certain conditions and need to be skipped
	filters Filters
}

// WithFilters sets specific settings filters for settings 2.0 object that needs to be filtered following
// to some custom criteria
func WithFilters(filters Filters) func(*Downloader) {
	return func(d *Downloader) {
		d.filters = filters
	}
}

// NewSettingsDownloader creates a new downloader for Settings 2.0 objects
func NewSettingsDownloader(client client.SettingsClient, opts ...func(*Downloader)) *Downloader {
	d := &Downloader{
		client:  client,
		filters: defaultSettingsFilters,
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Download downloads all settings 2.0 objects for the given schema IDs

func Download(client client.SettingsClient, schemaIDs []string, projectName string) v2.ConfigsPerType {
	return NewSettingsDownloader(client).Download(schemaIDs, projectName)
}

// DownloadAll downloads all settings 2.0 objects for a given project
func DownloadAll(client client.SettingsClient, projectName string) v2.ConfigsPerType {
	return NewSettingsDownloader(client).DownloadAll(projectName)
}

// Download downloads all settings 2.0 objects for the given schema IDs and a given project
// The returned value is a map of settings 2.0 objects with the schema ID as keys
func (d *Downloader) Download(schemaIDs []string, projectName string) v2.ConfigsPerType {
	return d.download(schemaIDs, projectName)
}

// DownloadAll downloads all settings 2.0 objects for a given project.
// The returned value is a map of settings 2.0 objects with the schema ID as keys
func (d *Downloader) DownloadAll(projectName string) v2.ConfigsPerType {
	log.Debug("Fetching all schemas to download")

	// get ALL schemas
	schemas, err := d.client.ListSchemas()
	if err != nil {
		log.Error("Failed to fetch all known schemas. Skipping settings download. Reason: %s", err)
		return nil
	}
	// convert to list of IDs
	var ids []string
	for _, i := range schemas {
		ids = append(ids, i.SchemaId)
	}

	return d.download(ids, projectName)
}

func (d *Downloader) download(schemas []string, projectName string) v2.ConfigsPerType {
	results := make(v2.ConfigsPerType, len(schemas))
	downloadMutex := sync.Mutex{}
	wg := sync.WaitGroup{}
	wg.Add(len(schemas))
	for _, schema := range schemas {
		go func(s string) {
			defer wg.Done()
			log.Debug("Downloading all settings for schema %s", s)
			objects, err := d.client.ListSettings(s, client.ListSettingsOptions{})
			if err != nil {
				log.Error("Failed to fetch all settings for schema %s: %v", s, err)
				return
			}
			if len(objects) == 0 {
				return
			}
			configs := d.convertAllObjects(objects, projectName)
			downloadMutex.Lock()
			results[s] = configs
			downloadMutex.Unlock()
		}(schema)
	}
	wg.Wait()

	return results
}

func (d *Downloader) convertAllObjects(objects []client.DownloadSettingsObject, projectName string) []config.Config {
	result := make([]config.Config, 0, len(objects))
	for _, o := range objects {

		// try to unmarshall settings value
		var contentUnmarshalled map[string]interface{}
		if err := json.Unmarshal(o.Value, &contentUnmarshalled); err != nil {
			log.Error("Unable to unmarshal JSON value of settings 2.0 object: %v", err)
			return result
		}
		// skip discarded settings objects
		if shouldDiscard, reason := d.filters.Get(o.SchemaId).ShouldDiscard(contentUnmarshalled); shouldDiscard {
			log.Warn("Downloaded setting of schema %q will be discarded. Reason: %s", o.SchemaId, reason)
			continue
		}

		// indent value payload
		var content string
		if bytes, err := json.MarshalIndent(o.Value, "", "  "); err == nil {
			content = string(bytes)
		} else {
			log.Warn("Failed to indent settings template. Reason: %s", err)
			content = string(o.Value)
		}

		// construct config object with generated config ID
		configId := idutils.GenerateUuidFromName(o.ObjectId)
		c := config.Config{
			Template: template.NewDownloadTemplate(configId, configId, content),
			Coordinate: coordinate.Coordinate{
				Project:  projectName,
				Type:     o.SchemaId,
				ConfigId: configId,
			},
			Type: config.Type{
				SchemaId:      o.SchemaId,
				SchemaVersion: o.SchemaVersion,
			},
			Parameters: map[string]parameter.Parameter{
				config.NameParameter:  &value.ValueParameter{Value: configId},
				config.ScopeParameter: &value.ValueParameter{Value: o.Scope},
			},
			Skip:           false,
			OriginObjectId: o.ObjectId,
		}
		result = append(result, c)
	}
	return result
}
