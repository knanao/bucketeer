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

	"github.com/bucketeer-io/bucketeer/pkg/locale"
	"github.com/bucketeer-io/bucketeer/pkg/log"
	"github.com/bucketeer-io/bucketeer/pkg/storage/v2/mysql"
	"github.com/bucketeer-io/bucketeer/pkg/user/domain"
	userstorage "github.com/bucketeer-io/bucketeer/pkg/user/storage/v2"
	accountproto "github.com/bucketeer-io/bucketeer/proto/account"
	userproto "github.com/bucketeer-io/bucketeer/proto/user"
)

const maxPageSizePerRequest = 50

func (s *userService) GetUser(ctx context.Context, req *userproto.GetUserRequest) (*userproto.GetUserResponse, error) {
	_, err := s.checkRole(ctx, accountproto.Account_VIEWER, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	if err := s.validateGetUserRequest(req); err != nil {
		return nil, err
	}
	user, err := s.getUser(ctx, req.UserId, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	return &userproto.GetUserResponse{
		User: user.User,
	}, nil
}

func (s *userService) validateGetUserRequest(req *userproto.GetUserRequest) error {
	if req.UserId == "" {
		return localizedError(statusMissingUserID, locale.JaJP)
	}
	return nil
}

func (s *userService) getUser(ctx context.Context, userID, environmentNamespace string) (*domain.User, error) {
	userStorage := userstorage.NewUserStorage(s.storageClient)
	user, err := userStorage.GetUser(ctx, userID, environmentNamespace)
	if err != nil {
		if err == userstorage.ErrUserNotFound {
			return nil, localizedError(statusNotFound, locale.JaJP)
		}
		s.logger.Error(
			"Failed to get user",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("userId", userID),
				zap.String("environmentNamespace", environmentNamespace),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return user, nil
}

func (s *userService) ListUsers(
	ctx context.Context,
	req *userproto.ListUsersRequest,
) (*userproto.ListUsersResponse, error) {
	_, err := s.checkRole(ctx, accountproto.Account_VIEWER, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	whereParts := []mysql.WherePart{
		mysql.NewFilter("environment_namespace", "=", req.EnvironmentNamespace),
	}
	if req.SearchKeyword != "" {
		whereParts = append(whereParts, mysql.NewSearchQuery([]string{"id"}, req.SearchKeyword))
	}
	if req.From != 0 {
		whereParts = append(whereParts, mysql.NewFilter("last_seen", ">=", req.From))
	}
	if req.To != 0 {
		whereParts = append(whereParts, mysql.NewFilter("last_seen", "<=", req.To))
	}
	orders, err := s.newListOrders(req.OrderBy, req.OrderDirection)
	if err != nil {
		s.logger.Error(
			"Invalid argument",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, err
	}
	if req.PageSize == 0 {
		req.PageSize = maxPageSizePerRequest
	}
	limit := int(req.PageSize)
	if req.Cursor == "" {
		req.Cursor = "0"
	}
	offset, err := strconv.Atoi(req.Cursor)
	if err != nil {
		return nil, localizedError(statusInvalidCursor, locale.JaJP)
	}
	storage := userstorage.NewUserStorage(s.storageClient)
	users, nextCursor, err := storage.ListUsers(ctx, whereParts, orders, limit, offset)
	if err != nil {
		s.logger.Error(
			"Failed to list users",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &userproto.ListUsersResponse{
		Users:  users,
		Cursor: strconv.Itoa(nextCursor),
	}, nil
}

func (s *userService) newListOrders(
	orderBy userproto.ListUsersRequest_OrderBy,
	orderDirection userproto.ListUsersRequest_OrderDirection,
) ([]*mysql.Order, error) {
	var column string
	switch orderBy {
	case userproto.ListUsersRequest_DEFAULT,
		userproto.ListUsersRequest_LAST_SEEN:
		column = "last_seen"
	case userproto.ListUsersRequest_CREATED_AT:
		column = "created_at"
	default:
		return nil, localizedError(statusInvalidOrderBy, locale.JaJP)
	}
	direction := mysql.OrderDirectionAsc
	if orderDirection == userproto.ListUsersRequest_DESC {
		direction = mysql.OrderDirectionDesc
	}
	return []*mysql.Order{mysql.NewOrder(column, direction)}, nil
}
