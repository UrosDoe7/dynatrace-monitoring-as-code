/*
 * @license
 * Copyright 2023 Dynatrace LLC
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

package idutils

import (
	"encoding/base64"
	"fmt"
)

// GenerateExternalID generates the externalID for settings 2.0 objects based on the schema, and ID.
// The result of the function is pure.
// Max length for the external ID is 500
func GenerateExternalID(schema, ID string) string {
	const prefix = "monaco:"
	const format = "%s$%s"
	const externalIDMaxLength = 500

	formattedID := fmt.Sprintf(format, schema, ID)
	encodedID := base64.StdEncoding.EncodeToString([]byte(formattedID))

	encodedIDMaxLength := externalIDMaxLength - len(prefix)
	if len(encodedID) > encodedIDMaxLength {
		encodedID = encodedID[encodedIDMaxLength:]
	}

	externalID := fmt.Sprintf("monaco:%s", encodedID)

	return externalID
}
