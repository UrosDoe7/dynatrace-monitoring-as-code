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

//go:build unit

package api

import (
	"testing"

	"github.com/golang/mock/gomock"
)

// CreateAPIMockFactory returns a mock version of the api interface
func CreateAPIMockFactory(t *testing.T) (*MockApi, func()) {
	mockCtrl := gomock.NewController(t)

	return NewMockApi(mockCtrl), mockCtrl.Finish
}

func CreateAPIMockWithId(t *testing.T, id string) (*MockApi, func()) {

	api, finish := CreateAPIMockFactory(t)
	api.EXPECT().GetId().MinTimes(1).Return(id)

	return api, finish
}
