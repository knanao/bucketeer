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
	"strconv"

	"go.uber.org/zap"

	"github.com/bucketeer-io/bucketeer/pkg/account/command"
	"github.com/bucketeer-io/bucketeer/pkg/account/domain"
	v2as "github.com/bucketeer-io/bucketeer/pkg/account/storage/v2"
	"github.com/bucketeer-io/bucketeer/pkg/locale"
	"github.com/bucketeer-io/bucketeer/pkg/log"
	"github.com/bucketeer-io/bucketeer/pkg/storage/v2/mysql"
	proto "github.com/bucketeer-io/bucketeer/proto/account"
	eventproto "github.com/bucketeer-io/bucketeer/proto/event/domain"
)

func (s *AccountService) CreateAPIKey(
	ctx context.Context,
	req *proto.CreateAPIKeyRequest,
) (*proto.CreateAPIKeyResponse, error) {
	editor, err := s.checkRole(ctx, proto.Account_OWNER, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	if err := validateCreateAPIKeyRequest(req); err != nil {
		return nil, err
	}
	key, err := domain.NewAPIKey(req.Command.Name, req.Command.Role)
	if err != nil {
		s.logger.Error(
			"Failed to create a new api key",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	tx, err := s.mysqlClient.BeginTx(ctx)
	if err != nil {
		s.logger.Error(
			"Failed to begin transaction",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	err = s.mysqlClient.RunInTransaction(ctx, tx, func() error {
		apiKeyStorage := v2as.NewAPIKeyStorage(tx)
		handler := command.NewAPIKeyCommandHandler(editor, key, s.publisher, req.EnvironmentNamespace)
		if err := handler.Handle(ctx, req.Command); err != nil {
			return err
		}
		return apiKeyStorage.CreateAPIKey(ctx, key, req.EnvironmentNamespace)
	})
	if err != nil {
		if err == v2as.ErrAPIKeyAlreadyExists {
			return nil, localizedError(statusAlreadyExists, locale.JaJP)
		}
		s.logger.Error(
			"Failed to create api key",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &proto.CreateAPIKeyResponse{
		ApiKey: key.APIKey,
	}, nil
}

func (s *AccountService) ChangeAPIKeyName(
	ctx context.Context,
	req *proto.ChangeAPIKeyNameRequest,
) (*proto.ChangeAPIKeyNameResponse, error) {
	editor, err := s.checkRole(ctx, proto.Account_OWNER, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	if err := validateChangeAPIKeyNameRequest(req); err != nil {
		s.logger.Error(
			"Failed to change api key name",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, err
	}
	if err := s.updateAPIKeyMySQL(ctx, editor, req.Id, req.EnvironmentNamespace, req.Command); err != nil {
		if err == v2as.ErrAPIKeyNotFound || err == v2as.ErrAPIKeyUnexpectedAffectedRows {
			return nil, localizedError(statusNotFound, locale.JaJP)
		}
		s.logger.Error(
			"Failed to change api key name",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
				zap.String("id", req.Id),
				zap.String("name", req.Command.Name),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &proto.ChangeAPIKeyNameResponse{}, nil
}

func (s *AccountService) EnableAPIKey(
	ctx context.Context,
	req *proto.EnableAPIKeyRequest,
) (*proto.EnableAPIKeyResponse, error) {
	editor, err := s.checkRole(ctx, proto.Account_OWNER, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	if err := validateEnableAPIKeyRequest(req); err != nil {
		s.logger.Error(
			"Failed to enable api key",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, err
	}
	if err := s.updateAPIKeyMySQL(ctx, editor, req.Id, req.EnvironmentNamespace, req.Command); err != nil {
		if err == v2as.ErrAPIKeyNotFound || err == v2as.ErrAPIKeyUnexpectedAffectedRows {
			return nil, localizedError(statusNotFound, locale.JaJP)
		}
		s.logger.Error(
			"Failed to enable api key",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
				zap.String("id", req.Id),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &proto.EnableAPIKeyResponse{}, nil
}

func (s *AccountService) DisableAPIKey(
	ctx context.Context,
	req *proto.DisableAPIKeyRequest,
) (*proto.DisableAPIKeyResponse, error) {
	editor, err := s.checkRole(ctx, proto.Account_OWNER, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	if err := validateDisableAPIKeyRequest(req); err != nil {
		s.logger.Error(
			"Failed to disable api key",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, err
	}
	if err := s.updateAPIKeyMySQL(ctx, editor, req.Id, req.EnvironmentNamespace, req.Command); err != nil {
		if err == v2as.ErrAPIKeyNotFound || err == v2as.ErrAPIKeyUnexpectedAffectedRows {
			return nil, localizedError(statusNotFound, locale.JaJP)
		}
		s.logger.Error(
			"Failed to disable api key",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
				zap.String("id", req.Id),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &proto.DisableAPIKeyResponse{}, nil
}

func (s *AccountService) updateAPIKeyMySQL(
	ctx context.Context,
	editor *eventproto.Editor,
	id, environmentNamespace string,
	cmd command.Command,
) error {
	tx, err := s.mysqlClient.BeginTx(ctx)
	if err != nil {
		s.logger.Error(
			"Failed to begin transaction",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
			)...,
		)
		return err
	}
	return s.mysqlClient.RunInTransaction(ctx, tx, func() error {
		apiKeyStorage := v2as.NewAPIKeyStorage(tx)
		apiKey, err := apiKeyStorage.GetAPIKey(ctx, id, environmentNamespace)
		if err != nil {
			return err
		}
		handler := command.NewAPIKeyCommandHandler(editor, apiKey, s.publisher, environmentNamespace)
		if err := handler.Handle(ctx, cmd); err != nil {
			return err
		}
		return apiKeyStorage.UpdateAPIKey(ctx, apiKey, environmentNamespace)
	})
}

func (s *AccountService) GetAPIKey(ctx context.Context, req *proto.GetAPIKeyRequest) (*proto.GetAPIKeyResponse, error) {
	_, err := s.checkRole(ctx, proto.Account_VIEWER, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	if req.Id == "" {
		return nil, localizedError(statusMissingAPIKeyID, locale.JaJP)
	}
	apiKeyStorage := v2as.NewAPIKeyStorage(s.mysqlClient)
	apiKey, err := apiKeyStorage.GetAPIKey(ctx, req.Id, req.EnvironmentNamespace)
	if err != nil {
		if err == v2as.ErrAPIKeyNotFound {
			return nil, localizedError(statusNotFound, locale.JaJP)
		}
		s.logger.Error(
			"Failed to get api key",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
				zap.String("id", req.Id),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &proto.GetAPIKeyResponse{ApiKey: apiKey.APIKey}, nil
}

func (s *AccountService) ListAPIKeys(
	ctx context.Context,
	req *proto.ListAPIKeysRequest,
) (*proto.ListAPIKeysResponse, error) {
	_, err := s.checkRole(ctx, proto.Account_VIEWER, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	whereParts := []mysql.WherePart{
		mysql.NewFilter("environment_namespace", "=", req.EnvironmentNamespace),
	}
	if req.Disabled != nil {
		whereParts = append(whereParts, mysql.NewFilter("disabled", "=", req.Disabled.Value))
	}
	if req.SearchKeyword != "" {
		whereParts = append(whereParts, mysql.NewSearchQuery([]string{"name"}, req.SearchKeyword))
	}
	orders, err := s.newAPIKeyListOrders(req.OrderBy, req.OrderDirection)
	if err != nil {
		s.logger.Error(
			"Invalid argument",
			log.FieldsFromImcomingContext(ctx).AddFields(zap.Error(err))...,
		)
		return nil, err
	}
	limit := int(req.PageSize)
	cursor := req.Cursor
	if cursor == "" {
		cursor = "0"
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil {
		return nil, localizedError(statusInvalidCursor, locale.JaJP)
	}
	apiKeyStorage := v2as.NewAPIKeyStorage(s.mysqlClient)
	apiKeys, nextCursor, totalCount, err := apiKeyStorage.ListAPIKeys(
		ctx,
		whereParts,
		orders,
		limit,
		offset,
	)
	if err != nil {
		s.logger.Error(
			"Failed to list api keys",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &proto.ListAPIKeysResponse{
		ApiKeys:    apiKeys,
		Cursor:     strconv.Itoa(nextCursor),
		TotalCount: totalCount,
	}, nil
}

func (s *AccountService) newAPIKeyListOrders(
	orderBy proto.ListAPIKeysRequest_OrderBy,
	orderDirection proto.ListAPIKeysRequest_OrderDirection,
) ([]*mysql.Order, error) {
	var column string
	switch orderBy {
	case proto.ListAPIKeysRequest_DEFAULT,
		proto.ListAPIKeysRequest_NAME:
		column = "name"
	case proto.ListAPIKeysRequest_CREATED_AT:
		column = "created_at"
	case proto.ListAPIKeysRequest_UPDATED_AT:
		column = "updated_at"
	default:
		return nil, localizedError(statusInvalidOrderBy, locale.JaJP)
	}
	direction := mysql.OrderDirectionAsc
	if orderDirection == proto.ListAPIKeysRequest_DESC {
		direction = mysql.OrderDirectionDesc
	}
	return []*mysql.Order{mysql.NewOrder(column, direction)}, nil
}

func (s *AccountService) GetAPIKeyBySearchingAllEnvironments(
	ctx context.Context,
	req *proto.GetAPIKeyBySearchingAllEnvironmentsRequest,
) (*proto.GetAPIKeyBySearchingAllEnvironmentsResponse, error) {
	_, err := s.checkAdminRole(ctx)
	if err != nil {
		return nil, err
	}
	if req.Id == "" {
		return nil, localizedError(statusMissingAPIKeyID, locale.JaJP)
	}
	projects, err := s.listProjects(ctx)
	if err != nil {
		s.logger.Error(
			"Failed to get project list",
			log.FieldsFromImcomingContext(ctx).AddFields(zap.Error(err))...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	if len(projects) == 0 {
		s.logger.Error(
			"Could not find any projects",
			log.FieldsFromImcomingContext(ctx).AddFields(zap.Error(err))...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	environments, err := s.listEnvironments(ctx)
	if err != nil {
		s.logger.Error(
			"Failed to get environment list",
			log.FieldsFromImcomingContext(ctx).AddFields(zap.Error(err))...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	if len(environments) == 0 {
		s.logger.Error(
			"Could not find any environments",
			log.FieldsFromImcomingContext(ctx).AddFields(zap.Error(err))...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	projectSet := s.makeProjectSet(projects)
	apiKeyStorage := v2as.NewAPIKeyStorage(s.mysqlClient)
	for _, e := range environments {
		if p, ok := projectSet[e.ProjectId]; !ok || p.Disabled {
			continue
		}
		apiKey, err := apiKeyStorage.GetAPIKey(ctx, req.Id, e.Namespace)
		if err != nil {
			if err == v2as.ErrAPIKeyNotFound {
				continue
			}
			s.logger.Error(
				"Failed to get api key",
				log.FieldsFromImcomingContext(ctx).AddFields(
					zap.Error(err),
					zap.String("environmentNamespace", e.Namespace),
					zap.String("id", req.Id),
				)...,
			)
			return nil, localizedError(statusInternal, locale.JaJP)
		}
		return &proto.GetAPIKeyBySearchingAllEnvironmentsResponse{
			EnvironmentApiKey: &proto.EnvironmentAPIKey{EnvironmentNamespace: e.Namespace, ApiKey: apiKey.APIKey},
		}, nil
	}
	return nil, localizedError(statusNotFound, locale.JaJP)
}
