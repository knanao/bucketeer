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

	"github.com/bucketeer-io/bucketeer/pkg/feature/command"
	"github.com/bucketeer-io/bucketeer/pkg/feature/domain"
	v2fs "github.com/bucketeer-io/bucketeer/pkg/feature/storage/v2"
	"github.com/bucketeer-io/bucketeer/pkg/locale"
	"github.com/bucketeer-io/bucketeer/pkg/log"
	"github.com/bucketeer-io/bucketeer/pkg/storage/v2/mysql"
	accountproto "github.com/bucketeer-io/bucketeer/proto/account"
	eventproto "github.com/bucketeer-io/bucketeer/proto/event/domain"
	featureproto "github.com/bucketeer-io/bucketeer/proto/feature"
)

func (s *FeatureService) CreateSegment(
	ctx context.Context,
	req *featureproto.CreateSegmentRequest,
) (*featureproto.CreateSegmentResponse, error) {
	editor, err := s.checkRole(ctx, accountproto.Account_EDITOR, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	if err = validateCreateSegmentRequest(req.Command); err != nil {
		s.logger.Info(
			"Invalid argument",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, err
	}
	segment, err := domain.NewSegment(req.Command.Name, req.Command.Description)
	if err != nil {
		s.logger.Error(
			"Failed to create segment",
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
		segmentStorage := v2fs.NewSegmentStorage(tx)
		if err := segmentStorage.CreateSegment(ctx, segment, req.EnvironmentNamespace); err != nil {
			s.logger.Error(
				"Failed to store segment",
				log.FieldsFromImcomingContext(ctx).AddFields(
					zap.Error(err),
					zap.String("environmentNamespace", req.EnvironmentNamespace),
				)...,
			)
			return err
		}
		handler := command.NewSegmentCommandHandler(
			editor,
			segment,
			s.domainPublisher,
			req.EnvironmentNamespace,
		)
		if err := handler.Handle(ctx, req.Command); err != nil {
			s.logger.Error(
				"Failed to handle command",
				log.FieldsFromImcomingContext(ctx).AddFields(
					zap.Error(err),
					zap.String("environmentNamespace", req.EnvironmentNamespace),
				)...,
			)
			return err
		}
		return nil
	})
	if err != nil {
		if err == v2fs.ErrSegmentAlreadyExists {
			return nil, localizedError(statusAlreadyExists, locale.JaJP)
		}
		s.logger.Error(
			"Failed to create segment",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &featureproto.CreateSegmentResponse{
		Segment: segment.Segment,
	}, nil
}

func (s *FeatureService) DeleteSegment(
	ctx context.Context,
	req *featureproto.DeleteSegmentRequest,
) (*featureproto.DeleteSegmentResponse, error) {
	editor, err := s.checkRole(ctx, accountproto.Account_EDITOR, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	if err := validateDeleteSegmentRequest(req); err != nil {
		s.logger.Info(
			"Invalid argument",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, err
	}
	if err := s.checkSegmentInUse(ctx, req.Id, req.EnvironmentNamespace); err != nil {
		return nil, err
	}
	if err := s.updateSegment(
		ctx,
		editor,
		[]command.Command{req.Command},
		req.Id,
		req.EnvironmentNamespace,
	); err != nil {
		return nil, err
	}
	return &featureproto.DeleteSegmentResponse{}, nil
}

func (s *FeatureService) checkSegmentInUse(ctx context.Context, segmentID, environmentNamespace string) error {
	features := []*featureproto.Feature{}
	var cursor string
	for {
		f, cursor, _, err := s.listFeatures(
			ctx,
			mysql.QueryNoLimit,
			cursor,
			nil,
			"",
			nil,
			nil,
			"",
			featureproto.ListFeaturesRequest_DEFAULT,
			featureproto.ListFeaturesRequest_ASC,
			environmentNamespace,
		)
		if err != nil {
			s.logger.Error(
				"Failed to list features",
				log.FieldsFromImcomingContext(ctx).AddFields(
					zap.Error(err),
					zap.String("environmentNamespace", environmentNamespace),
				)...,
			)
			return localizedError(statusInternal, locale.JaJP)
		}
		features = append(features, f...)
		size := len(f)
		if cursor == "" || size == 0 || size < listRequestSize {
			break
		}
	}
	if s.containsInRules(segmentID, features) {
		s.logger.Warn(
			"Segment User in use",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.String("segmentId", segmentID),
				zap.String("environmentNamespace", environmentNamespace),
			)...,
		)
		return localizedError(statusSegmentInUse, locale.JaJP)
	}
	return nil
}

func (s *FeatureService) containsInRules(segmentID string, features []*featureproto.Feature) bool {
	for _, f := range features {
		for _, r := range f.Rules {
			for _, c := range r.Clauses {
				if c.Operator == featureproto.Clause_SEGMENT {
					for _, id := range c.Values {
						if segmentID == id {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

func (s *FeatureService) UpdateSegment(
	ctx context.Context,
	req *featureproto.UpdateSegmentRequest,
) (*featureproto.UpdateSegmentResponse, error) {
	editor, err := s.checkRole(ctx, accountproto.Account_EDITOR, req.EnvironmentNamespace)
	if err != nil {
		s.logger.Info(
			"Permission denied",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, err
	}
	commands := make([]command.Command, 0, len(req.Commands))
	for _, c := range req.Commands {
		cmd, err := command.UnmarshalCommand(c)
		if err != nil {
			s.logger.Error(
				"Failed to unmarshal command",
				log.FieldsFromImcomingContext(ctx).AddFields(
					zap.Error(err),
					zap.String("environmentNamespace", req.EnvironmentNamespace),
				)...,
			)
			return nil, localizedError(statusInternal, locale.JaJP)
		}
		commands = append(commands, cmd)
	}
	if err := validateUpdateSegment(req.Id, commands); err != nil {
		s.logger.Info(
			"Invalid argument",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, err
	}
	if err := s.updateSegment(ctx, editor, commands, req.Id, req.EnvironmentNamespace); err != nil {
		return nil, err
	}
	return &featureproto.UpdateSegmentResponse{}, nil
}

func (s *FeatureService) updateSegment(
	ctx context.Context,
	editor *eventproto.Editor,
	commands []command.Command,
	segmentID, environmentNamespace string,
) error {
	tx, err := s.mysqlClient.BeginTx(ctx)
	if err != nil {
		s.logger.Error(
			"Failed to begin transaction",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
			)...,
		)
		return localizedError(statusInternal, locale.JaJP)
	}
	err = s.mysqlClient.RunInTransaction(ctx, tx, func() error {
		segmentStorage := v2fs.NewSegmentStorage(tx)
		segment, err := segmentStorage.GetSegment(ctx, segmentID, environmentNamespace)
		if err != nil {
			s.logger.Error(
				"Failed to get segment",
				log.FieldsFromImcomingContext(ctx).AddFields(
					zap.Error(err),
					zap.String("environmentNamespace", environmentNamespace),
				)...,
			)
			return err
		}
		handler := command.NewSegmentCommandHandler(
			editor,
			segment,
			s.domainPublisher,
			environmentNamespace,
		)
		for _, cmd := range commands {
			if err := handler.Handle(ctx, cmd); err != nil {
				s.logger.Error(
					"Failed to handle command",
					log.FieldsFromImcomingContext(ctx).AddFields(
						zap.Error(err),
						zap.String("environmentNamespace", environmentNamespace),
					)...,
				)
				return err
			}
		}
		return segmentStorage.UpdateSegment(ctx, segment, environmentNamespace)
	})
	if err != nil {
		if err == v2fs.ErrSegmentNotFound || err == v2fs.ErrSegmentUnexpectedAffectedRows {
			return localizedError(statusNotFound, locale.JaJP)
		}
		s.logger.Error(
			"Failed to update segment",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", environmentNamespace),
			)...,
		)
		return localizedError(statusInternal, locale.JaJP)
	}
	return nil
}

func (s *FeatureService) GetSegment(
	ctx context.Context,
	req *featureproto.GetSegmentRequest,
) (*featureproto.GetSegmentResponse, error) {
	_, err := s.checkRole(ctx, accountproto.Account_VIEWER, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	if err := validateGetSegmentRequest(req); err != nil {
		s.logger.Info(
			"Invalid argument",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, err
	}
	segmentStorage := v2fs.NewSegmentStorage(s.mysqlClient)
	segment, err := segmentStorage.GetSegment(ctx, req.Id, req.EnvironmentNamespace)
	if err != nil {
		if err == v2fs.ErrSegmentNotFound {
			return nil, localizedError(statusNotFound, locale.JaJP)
		}
		s.logger.Error(
			"Failed to get segment",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &featureproto.GetSegmentResponse{Segment: segment.Segment}, nil

}

func (s *FeatureService) ListSegments(
	ctx context.Context,
	req *featureproto.ListSegmentsRequest,
) (*featureproto.ListSegmentsResponse, error) {
	_, err := s.checkRole(ctx, accountproto.Account_VIEWER, req.EnvironmentNamespace)
	if err != nil {
		return nil, err
	}
	if err := validateListSegmentsRequest(req); err != nil {
		s.logger.Info(
			"Invalid argument",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, err
	}
	whereParts := []mysql.WherePart{
		mysql.NewFilter("deleted", "=", false),
		mysql.NewFilter("environment_namespace", "=", req.EnvironmentNamespace),
	}
	if req.Status != nil {
		whereParts = append(whereParts, mysql.NewFilter("status", "=", req.Status.Value))
	}
	if req.SearchKeyword != "" {
		whereParts = append(whereParts, mysql.NewSearchQuery([]string{"name", "description"}, req.SearchKeyword))
	}
	orders, err := s.newSegmentListOrders(req.OrderBy, req.OrderDirection)
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
	var isInUseStatus *bool
	if req.IsInUseStatus != nil {
		isInUseStatus = &req.IsInUseStatus.Value
	}
	segmentStorage := v2fs.NewSegmentStorage(s.mysqlClient)
	segments, nextCursor, totalCount, err := segmentStorage.ListSegments(
		ctx,
		whereParts,
		orders,
		limit,
		offset,
		isInUseStatus,
		req.EnvironmentNamespace,
	)
	if err != nil {
		s.logger.Error(
			"Failed to list segments",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
				zap.String("environmentNamespace", req.EnvironmentNamespace),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &featureproto.ListSegmentsResponse{
		Segments:   segments,
		Cursor:     strconv.Itoa(nextCursor),
		TotalCount: totalCount,
	}, nil
}

func (s *FeatureService) newSegmentListOrders(
	orderBy featureproto.ListSegmentsRequest_OrderBy,
	orderDirection featureproto.ListSegmentsRequest_OrderDirection,
) ([]*mysql.Order, error) {
	var column string
	switch orderBy {
	case featureproto.ListSegmentsRequest_DEFAULT,
		featureproto.ListSegmentsRequest_NAME:
		column = "name"
	case featureproto.ListSegmentsRequest_CREATED_AT:
		column = "created_at"
	case featureproto.ListSegmentsRequest_UPDATED_AT:
		column = "updated_at"
	default:
		return nil, localizedError(statusInvalidOrderBy, locale.JaJP)
	}
	direction := mysql.OrderDirectionAsc
	if orderDirection == featureproto.ListSegmentsRequest_DESC {
		direction = mysql.OrderDirectionDesc
	}
	return []*mysql.Order{mysql.NewOrder(column, direction)}, nil
}
