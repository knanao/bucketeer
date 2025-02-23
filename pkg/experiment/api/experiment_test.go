// Copyright 2022 The Bucketeer Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package api

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	v2es "github.com/bucketeer-io/bucketeer/pkg/experiment/storage/v2"
	storagetesting "github.com/bucketeer-io/bucketeer/pkg/storage/testing"
	"github.com/bucketeer-io/bucketeer/pkg/storage/v2/mysql"
	mysqlmock "github.com/bucketeer-io/bucketeer/pkg/storage/v2/mysql/mock"
	experimentproto "github.com/bucketeer-io/bucketeer/proto/experiment"
)

func TestGetExperimentMySQL(t *testing.T) {
	t.Parallel()
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	patterns := []struct {
		setup                func(*experimentService)
		id                   string
		environmentNamespace string
		expectedErr          error
	}{
		{
			setup:                nil,
			id:                   "",
			environmentNamespace: "ns0",
			expectedErr:          errExperimentIDRequiredJaJP,
		},
		{
			setup: func(s *experimentService) {
				row := mysqlmock.NewMockRow(mockController)
				row.EXPECT().Scan(gomock.Any()).Return(mysql.ErrNoRows)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().QueryRowContext(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(row)
			},
			id:                   "id-0",
			environmentNamespace: "ns0",
			expectedErr:          errNotFoundJaJP,
		},
		{
			setup: func(s *experimentService) {
				row := mysqlmock.NewMockRow(mockController)
				row.EXPECT().Scan(gomock.Any()).Return(nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().QueryRowContext(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(row)
			},
			id:                   "id-1",
			environmentNamespace: "ns0",
			expectedErr:          nil,
		},
	}
	for _, p := range patterns {
		service := createExperimentService(mockController, nil)
		if p.setup != nil {
			p.setup(service)
		}
		req := &experimentproto.GetExperimentRequest{Id: p.id, EnvironmentNamespace: p.environmentNamespace}
		_, err := service.GetExperiment(createContextWithTokenRoleUnassigned(), req)
		assert.Equal(t, p.expectedErr, err)
	}
}

func TestListExperimentMySQL(t *testing.T) {
	t.Parallel()
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	patterns := []struct {
		setup       func(*experimentService)
		req         *experimentproto.ListExperimentsRequest
		expectedErr error
	}{
		{
			setup: func(s *experimentService) {
				rows := mysqlmock.NewMockRows(mockController)
				rows.EXPECT().Close().Return(nil)
				rows.EXPECT().Next().Return(false)
				rows.EXPECT().Err().Return(nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().QueryContext(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(rows, nil)
				row := mysqlmock.NewMockRow(mockController)
				row.EXPECT().Scan(gomock.Any()).Return(nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().QueryRowContext(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(row)
			},
			req:         &experimentproto.ListExperimentsRequest{FeatureId: "id-0", EnvironmentNamespace: "ns0"},
			expectedErr: nil,
		},
	}
	for _, p := range patterns {
		service := createExperimentService(mockController, nil)
		if p.setup != nil {
			p.setup(service)
		}
		_, err := service.ListExperiments(createContextWithTokenRoleUnassigned(), p.req)
		assert.Equal(t, p.expectedErr, err)
	}
}

func TestCreateExperimentMySQL(t *testing.T) {
	t.Parallel()
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	patterns := []struct {
		setup       func(s *experimentService)
		input       *experimentproto.CreateExperimentRequest
		expectedErr error
	}{
		{
			setup: func(s *experimentService) {
				row := mysqlmock.NewMockRow(mockController)
				row.EXPECT().Scan(gomock.Any()).Return(nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().QueryRowContext(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(row)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(nil)
			},
			input: &experimentproto.CreateExperimentRequest{
				Command: &experimentproto.CreateExperimentCommand{
					FeatureId: "fid",
					GoalIds:   []string{"goalId"},
					StartAt:   1,
					StopAt:    10,
				},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: nil,
		},
	}
	for _, p := range patterns {
		ctx := createContextWithToken()
		service := createExperimentService(mockController, nil)
		if p.setup != nil {
			p.setup(service)
		}
		_, err := service.CreateExperiment(ctx, p.input)
		assert.Equal(t, p.expectedErr, err)
	}
}

func TestValidateCreateExperimentRequest(t *testing.T) {
	t.Parallel()
	patterns := []struct {
		in       *experimentproto.CreateExperimentRequest
		expected error
	}{
		{
			in: &experimentproto.CreateExperimentRequest{
				Command: &experimentproto.CreateExperimentCommand{
					FeatureId: "fid",
					GoalIds:   []string{"gid"},
					StartAt:   1,
					StopAt:    10,
				},
				EnvironmentNamespace: "ns0",
			},
			expected: nil,
		},
		{
			in: &experimentproto.CreateExperimentRequest{
				Command: &experimentproto.CreateExperimentCommand{
					FeatureId: "",
					GoalIds:   []string{"gid"},
				},
				EnvironmentNamespace: "ns0",
			},
			expected: errFeatureIDRequiredJaJP,
		},
		{
			in: &experimentproto.CreateExperimentRequest{
				Command: &experimentproto.CreateExperimentCommand{
					FeatureId: "fid",
					GoalIds:   nil,
				},
				EnvironmentNamespace: "ns0",
			},
			expected: errGoalIDRequiredJaJP,
		},
		{
			in: &experimentproto.CreateExperimentRequest{
				Command: &experimentproto.CreateExperimentCommand{
					FeatureId: "fid",
					GoalIds:   []string{""},
				},
				EnvironmentNamespace: "ns0",
			},
			expected: errGoalIDRequiredJaJP,
		},
		{
			in: &experimentproto.CreateExperimentRequest{
				Command: &experimentproto.CreateExperimentCommand{
					FeatureId: "fid",
					GoalIds:   []string{"gid", ""},
				},
				EnvironmentNamespace: "ns0",
			},
			expected: errGoalIDRequiredJaJP,
		},
		{
			in: &experimentproto.CreateExperimentRequest{
				Command: &experimentproto.CreateExperimentCommand{
					FeatureId: "fid",
					GoalIds:   []string{"gid0", "gid1"},
					StartAt:   1,
					StopAt:    30*24*60*60 + 2,
				},
				EnvironmentNamespace: "ns0",
			},
			expected: errPeriodTooLongJaJP,
		},
		{
			in: &experimentproto.CreateExperimentRequest{
				Command: &experimentproto.CreateExperimentCommand{
					FeatureId: "fid",
					GoalIds:   []string{"gid0", "gid1"},
					StartAt:   1,
					StopAt:    10,
				},
				EnvironmentNamespace: "ns0",
			},
			expected: nil,
		},
	}
	for _, p := range patterns {
		err := validateCreateExperimentRequest(p.in)
		assert.Equal(t, p.expected, err)
	}
}

func TestUpdateExperimentMySQL(t *testing.T) {
	t.Parallel()
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	patterns := []struct {
		setup       func(*experimentService)
		req         *experimentproto.UpdateExperimentRequest
		expectedErr error
	}{
		{
			setup: nil,
			req: &experimentproto.UpdateExperimentRequest{
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errExperimentIDRequiredJaJP,
		},
		{
			setup: nil,
			req: &experimentproto.UpdateExperimentRequest{
				Id: "id-1",
				ChangeExperimentPeriodCommand: &experimentproto.ChangeExperimentPeriodCommand{
					StartAt: time.Now().Unix(),
					StopAt:  time.Now().AddDate(0, 0, 31).Unix(),
				},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errPeriodTooLongJaJP,
		},
		{
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(v2es.ErrExperimentNotFound)
			},
			req: &experimentproto.UpdateExperimentRequest{
				Id:                   "id-0",
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNotFoundJaJP,
		},
		{
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(nil)
			},
			req: &experimentproto.UpdateExperimentRequest{
				Id:                   "id-1",
				ChangeNameCommand:    &experimentproto.ChangeExperimentNameCommand{Name: "test-name"},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: nil,
		},
	}
	for _, p := range patterns {
		ctx := createContextWithToken()
		service := createExperimentService(mockController, nil)
		if p.setup != nil {
			p.setup(service)
		}
		_, err := service.UpdateExperiment(ctx, p.req)
		assert.Equal(t, p.expectedErr, err)
	}
}

func TestStartExperimentMySQL(t *testing.T) {
	t.Parallel()
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	patterns := map[string]struct {
		setup       func(*experimentService)
		req         *experimentproto.StartExperimentRequest
		expectedErr error
	}{
		"error id required": {
			setup: nil,
			req: &experimentproto.StartExperimentRequest{
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errExperimentIDRequiredJaJP,
		},
		"error no command": {
			setup: nil,
			req: &experimentproto.StartExperimentRequest{
				Id:                   "eid",
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNoCommandJaJP,
		},
		"error not found": {
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(v2es.ErrExperimentNotFound)
			},
			req: &experimentproto.StartExperimentRequest{
				Id:                   "noop",
				Command:              &experimentproto.StartExperimentCommand{},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNotFoundJaJP,
		},
		"success": {
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(nil)
			},
			req: &experimentproto.StartExperimentRequest{
				Id:                   "eid",
				Command:              &experimentproto.StartExperimentCommand{},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: nil,
		},
	}
	for msg, p := range patterns {
		t.Run(msg, func(t *testing.T) {
			ctx := createContextWithToken()
			service := createExperimentService(mockController, nil)
			if p.setup != nil {
				p.setup(service)
			}
			_, err := service.StartExperiment(ctx, p.req)
			assert.Equal(t, p.expectedErr, err)
		})
	}
}

func TestFinishExperimentMySQL(t *testing.T) {
	t.Parallel()
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	patterns := map[string]struct {
		setup       func(*experimentService)
		req         *experimentproto.FinishExperimentRequest
		expectedErr error
	}{
		"error id required": {
			setup: nil,
			req: &experimentproto.FinishExperimentRequest{
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errExperimentIDRequiredJaJP,
		},
		"error no command": {
			setup: nil,
			req: &experimentproto.FinishExperimentRequest{
				Id:                   "eid",
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNoCommandJaJP,
		},
		"error not found": {
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(v2es.ErrExperimentNotFound)
			},
			req: &experimentproto.FinishExperimentRequest{
				Id:                   "noop",
				Command:              &experimentproto.FinishExperimentCommand{},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNotFoundJaJP,
		},
		"success": {
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(nil)
			},
			req: &experimentproto.FinishExperimentRequest{
				Id:                   "eid",
				Command:              &experimentproto.FinishExperimentCommand{},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: nil,
		},
	}
	for msg, p := range patterns {
		t.Run(msg, func(t *testing.T) {
			ctx := createContextWithToken()
			service := createExperimentService(mockController, nil)
			if p.setup != nil {
				p.setup(service)
			}
			_, err := service.FinishExperiment(ctx, p.req)
			assert.Equal(t, p.expectedErr, err)
		})
	}
}

func TestStopExperimentMySQL(t *testing.T) {
	t.Parallel()
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	patterns := []struct {
		setup       func(*experimentService)
		req         *experimentproto.StopExperimentRequest
		expectedErr error
	}{
		{
			setup: nil,
			req: &experimentproto.StopExperimentRequest{
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errExperimentIDRequiredJaJP,
		},
		{
			setup: nil,
			req: &experimentproto.StopExperimentRequest{
				Id:                   "id-0",
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNoCommandJaJP,
		},
		{
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(v2es.ErrExperimentNotFound)
			},
			req: &experimentproto.StopExperimentRequest{
				Id:                   "id-0",
				Command:              &experimentproto.StopExperimentCommand{},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNotFoundJaJP,
		},
		{
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(nil)
			},
			req: &experimentproto.StopExperimentRequest{
				Id:                   "id-1",
				Command:              &experimentproto.StopExperimentCommand{},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: nil,
		},
	}
	for _, p := range patterns {
		ctx := createContextWithToken()
		service := createExperimentService(mockController, nil)
		if p.setup != nil {
			p.setup(service)
		}
		_, err := service.StopExperiment(ctx, p.req)
		assert.Equal(t, p.expectedErr, err)
	}
}

func TestArchiveExperimentMySQL(t *testing.T) {
	t.Parallel()
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	patterns := []struct {
		setup       func(*experimentService)
		req         *experimentproto.ArchiveExperimentRequest
		expectedErr error
	}{
		{
			setup: nil,
			req: &experimentproto.ArchiveExperimentRequest{
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errExperimentIDRequiredJaJP,
		},
		{
			setup: nil,
			req: &experimentproto.ArchiveExperimentRequest{
				Id:                   "id-0",
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNoCommandJaJP,
		},
		{
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(v2es.ErrExperimentNotFound)
			},
			req: &experimentproto.ArchiveExperimentRequest{
				Id:                   "id-0",
				Command:              &experimentproto.ArchiveExperimentCommand{},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNotFoundJaJP,
		},
		{
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(nil)
			},
			req: &experimentproto.ArchiveExperimentRequest{
				Id:                   "id-1",
				Command:              &experimentproto.ArchiveExperimentCommand{},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: nil,
		},
	}
	for _, p := range patterns {
		ctx := createContextWithToken()
		service := createExperimentService(mockController, nil)
		if p.setup != nil {
			p.setup(service)
		}
		_, err := service.ArchiveExperiment(ctx, p.req)
		assert.Equal(t, p.expectedErr, err)
	}
}

func TestDeleteExperimentMySQL(t *testing.T) {
	t.Parallel()
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	patterns := []struct {
		setup       func(*experimentService)
		req         *experimentproto.DeleteExperimentRequest
		expectedErr error
	}{
		{
			setup: nil,
			req: &experimentproto.DeleteExperimentRequest{
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errExperimentIDRequiredJaJP,
		},
		{
			setup: nil,
			req: &experimentproto.DeleteExperimentRequest{
				Id:                   "id-0",
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNoCommandJaJP,
		},
		{
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(v2es.ErrExperimentNotFound)
			},
			req: &experimentproto.DeleteExperimentRequest{
				Id:                   "id-0",
				Command:              &experimentproto.DeleteExperimentCommand{},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: errNotFoundJaJP,
		},
		{
			setup: func(s *experimentService) {
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().BeginTx(gomock.Any()).Return(nil, nil)
				s.mysqlClient.(*mysqlmock.MockClient).EXPECT().RunInTransaction(
					gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(nil)
			},
			req: &experimentproto.DeleteExperimentRequest{
				Id:                   "id-1",
				Command:              &experimentproto.DeleteExperimentCommand{},
				EnvironmentNamespace: "ns0",
			},
			expectedErr: nil,
		},
	}
	for _, p := range patterns {
		ctx := createContextWithToken()
		service := createExperimentService(mockController, nil)
		if p.setup != nil {
			p.setup(service)
		}
		_, err := service.DeleteExperiment(ctx, p.req)
		assert.Equal(t, p.expectedErr, err)
	}
}

func TestExperimentPermissionDenied(t *testing.T) {
	t.Parallel()
	mockController := gomock.NewController(t)
	defer mockController.Finish()
	ctx := createContextWithTokenRoleUnassigned()
	s := storagetesting.NewInMemoryStorage()
	service := createExperimentService(mockController, s)
	patterns := map[string]struct {
		action   func(context.Context, *experimentService) error
		expected error
	}{
		"CreateExperiment": {
			action: func(ctx context.Context, es *experimentService) error {
				_, err := es.CreateExperiment(ctx, &experimentproto.CreateExperimentRequest{})
				return err
			},
			expected: errPermissionDeniedJaJP,
		},
		"UpdateExperiment": {
			action: func(ctx context.Context, es *experimentService) error {
				_, err := es.UpdateExperiment(ctx, &experimentproto.UpdateExperimentRequest{})
				return err
			},
			expected: errPermissionDeniedJaJP,
		},
		"StopExperiment": {
			action: func(ctx context.Context, es *experimentService) error {
				_, err := es.StopExperiment(ctx, &experimentproto.StopExperimentRequest{})
				return err
			},
			expected: errPermissionDeniedJaJP,
		},
		"DeleteExperiment": {
			action: func(ctx context.Context, es *experimentService) error {
				_, err := es.DeleteExperiment(ctx, &experimentproto.DeleteExperimentRequest{})
				return err
			},
			expected: errPermissionDeniedJaJP,
		},
	}
	for msg, p := range patterns {
		actual := p.action(ctx, service)
		assert.Equal(t, p.expected, actual, "%s", msg)
	}
}
