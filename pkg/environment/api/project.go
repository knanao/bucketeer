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
	"fmt"
	"regexp"
	"strconv"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bucketeer-io/bucketeer/pkg/environment/command"
	"github.com/bucketeer-io/bucketeer/pkg/environment/domain"
	v2es "github.com/bucketeer-io/bucketeer/pkg/environment/storage/v2"
	"github.com/bucketeer-io/bucketeer/pkg/locale"
	"github.com/bucketeer-io/bucketeer/pkg/log"
	"github.com/bucketeer-io/bucketeer/pkg/storage/v2/mysql"
	accountproto "github.com/bucketeer-io/bucketeer/proto/account"
	environmentproto "github.com/bucketeer-io/bucketeer/proto/environment"
	eventproto "github.com/bucketeer-io/bucketeer/proto/event/domain"
)

var (
	projectIDRegex = regexp.MustCompile("^[a-z0-9-]{1,50}$")

	//nolint:lll
	emailRegex = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
)

func (s *EnvironmentService) GetProject(
	ctx context.Context,
	req *environmentproto.GetProjectRequest,
) (*environmentproto.GetProjectResponse, error) {
	_, err := s.checkAdminRole(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateGetProjectRequest(req); err != nil {
		return nil, err
	}
	project, err := s.getProject(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &environmentproto.GetProjectResponse{
		Project: project.Project,
	}, nil
}

func validateGetProjectRequest(req *environmentproto.GetProjectRequest) error {
	if req.Id == "" {
		return localizedError(statusProjectIDRequired, locale.JaJP)
	}
	return nil
}

func (s *EnvironmentService) getProject(ctx context.Context, id string) (*domain.Project, error) {
	projectStorage := v2es.NewProjectStorage(s.mysqlClient)
	project, err := projectStorage.GetProject(ctx, id)
	if err != nil {
		if err == v2es.ErrProjectNotFound {
			return nil, localizedError(statusProjectNotFound, locale.JaJP)
		}
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return project, nil
}

func (s *EnvironmentService) ListProjects(
	ctx context.Context,
	req *environmentproto.ListProjectsRequest,
) (*environmentproto.ListProjectsResponse, error) {
	_, err := s.checkAdminRole(ctx)
	if err != nil {
		return nil, err
	}
	whereParts := []mysql.WherePart{}
	if req.Disabled != nil {
		whereParts = append(whereParts, mysql.NewFilter("disabled", "=", req.Disabled.Value))
	}
	if req.SearchKeyword != "" {
		whereParts = append(whereParts, mysql.NewSearchQuery([]string{"id", "creator_email"}, req.SearchKeyword))
	}
	orders, err := s.newProjectListOrders(req.OrderBy, req.OrderDirection)
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
	projectStorage := v2es.NewProjectStorage(s.mysqlClient)
	projects, nextCursor, totalCount, err := projectStorage.ListProjects(
		ctx,
		whereParts,
		orders,
		limit,
		offset,
	)
	if err != nil {
		s.logger.Error(
			"Failed to list projects",
			log.FieldsFromImcomingContext(ctx).AddFields(
				zap.Error(err),
			)...,
		)
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return &environmentproto.ListProjectsResponse{
		Projects:   projects,
		Cursor:     strconv.Itoa(nextCursor),
		TotalCount: totalCount,
	}, nil
}

func (s *EnvironmentService) newProjectListOrders(
	orderBy environmentproto.ListProjectsRequest_OrderBy,
	orderDirection environmentproto.ListProjectsRequest_OrderDirection,
) ([]*mysql.Order, error) {
	var column string
	switch orderBy {
	case environmentproto.ListProjectsRequest_DEFAULT,
		environmentproto.ListProjectsRequest_ID:
		column = "id"
	case environmentproto.ListProjectsRequest_CREATED_AT:
		column = "created_at"
	case environmentproto.ListProjectsRequest_UPDATED_AT:
		column = "updated_at"
	default:
		return nil, localizedError(statusInvalidOrderBy, locale.JaJP)
	}
	direction := mysql.OrderDirectionAsc
	if orderDirection == environmentproto.ListProjectsRequest_DESC {
		direction = mysql.OrderDirectionDesc
	}
	return []*mysql.Order{mysql.NewOrder(column, direction)}, nil
}

func (s *EnvironmentService) CreateProject(
	ctx context.Context,
	req *environmentproto.CreateProjectRequest,
) (*environmentproto.CreateProjectResponse, error) {
	editor, err := s.checkAdminRole(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateCreateProjectRequest(req); err != nil {
		return nil, err
	}
	project := domain.NewProject(req.Command.Id, req.Command.Description, editor.Email, false)
	if err := s.createProject(ctx, req.Command, project, editor); err != nil {
		return nil, err
	}
	return &environmentproto.CreateProjectResponse{}, nil
}

func validateCreateProjectRequest(req *environmentproto.CreateProjectRequest) error {
	if req.Command == nil {
		return localizedError(statusNoCommand, locale.JaJP)
	}
	if !projectIDRegex.MatchString(req.Command.Id) {
		return localizedError(statusInvalidProjectID, locale.JaJP)
	}
	return nil
}

func (s *EnvironmentService) createProject(
	ctx context.Context,
	cmd command.Command,
	project *domain.Project,
	editor *eventproto.Editor,
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
		projectStorage := v2es.NewProjectStorage(tx)
		handler := command.NewProjectCommandHandler(editor, project, s.publisher)
		if err := handler.Handle(ctx, cmd); err != nil {
			return err
		}
		return projectStorage.CreateProject(ctx, project)
	})
	if err != nil {
		if err == v2es.ErrProjectAlreadyExists {
			return localizedError(statusProjectAlreadyExists, locale.JaJP)
		}
		s.logger.Error(
			"Failed to create project",
			log.FieldsFromImcomingContext(ctx).AddFields(zap.Error(err))...,
		)
		return localizedError(statusInternal, locale.JaJP)
	}
	return nil
}

func (s *EnvironmentService) CreateTrialProject(
	ctx context.Context,
	req *environmentproto.CreateTrialProjectRequest,
) (*environmentproto.CreateTrialProjectResponse, error) {
	_, err := s.checkAdminRole(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateCreateTrialProjectRequest(req); err != nil {
		return nil, err
	}
	editor := &eventproto.Editor{
		Email:   req.Command.Email,
		Role:    accountproto.Account_UNASSIGNED,
		IsAdmin: false,
	}
	existingProject, err := s.getTrialProjectByEmail(ctx, editor.Email)
	if err != nil && status.Code(err) != codes.NotFound {
		return nil, err
	}
	if existingProject != nil {
		return nil, localizedError(statusProjectAlreadyExists, locale.JaJP)
	}
	project := domain.NewProject(req.Command.Id, "", editor.Email, true)
	if err := s.createProject(ctx, req.Command, project, editor); err != nil {
		return nil, err
	}
	if err := s.createTrialEnvironmentsAndAccounts(ctx, project, editor); err != nil {
		return nil, err
	}
	return &environmentproto.CreateTrialProjectResponse{}, nil
}

func validateCreateTrialProjectRequest(req *environmentproto.CreateTrialProjectRequest) error {
	if req.Command == nil {
		return localizedError(statusNoCommand, locale.JaJP)
	}
	if !projectIDRegex.MatchString(req.Command.Id) {
		return localizedError(statusInvalidProjectID, locale.JaJP)
	}
	if !emailRegex.MatchString(req.Command.Email) {
		return localizedError(statusInvalidProjectCreatorEmail, locale.JaJP)
	}
	return nil
}

func (s *EnvironmentService) getTrialProjectByEmail(
	ctx context.Context,
	email string,
) (*environmentproto.Project, error) {
	projectStorage := v2es.NewProjectStorage(s.mysqlClient)
	project, err := projectStorage.GetTrialProjectByEmail(ctx, email, false, true)
	if err != nil {
		if err == v2es.ErrProjectNotFound {
			return nil, localizedError(statusProjectNotFound, locale.JaJP)
		}
		return nil, localizedError(statusInternal, locale.JaJP)
	}
	return project.Project, nil
}

func (s *EnvironmentService) createTrialEnvironmentsAndAccounts(
	ctx context.Context,
	project *domain.Project,
	editor *eventproto.Editor,
) error {
	getAdminAccountReq := &accountproto.GetAdminAccountRequest{
		Email: editor.Email,
	}
	getAdminAccountRes, err := s.accountClient.GetAdminAccount(ctx, getAdminAccountReq)
	if err != nil && status.Code(err) != codes.NotFound {
		return localizedError(statusInternal, locale.JaJP)
	}
	adminAccountExists := false
	if getAdminAccountRes != nil && getAdminAccountRes.Account != nil {
		adminAccountExists = true
	}
	envIDs := []string{
		fmt.Sprintf("%s-development", project.Id),
		fmt.Sprintf("%s-staging", project.Id),
		fmt.Sprintf("%s-production", project.Id),
	}
	for _, envID := range envIDs {
		createEnvCmd := &environmentproto.CreateEnvironmentCommand{
			Id:          envID,
			ProjectId:   project.Id,
			Description: "",
		}
		env := domain.NewEnvironment(envID, "", project.Id)
		if err := s.createEnvironment(ctx, createEnvCmd, env, editor); err != nil {
			return err
		}
		if !adminAccountExists {
			createAccountReq := &accountproto.CreateAccountRequest{
				Command: &accountproto.CreateAccountCommand{
					Email: editor.Email,
					Role:  accountproto.Account_OWNER,
				},
				EnvironmentNamespace: env.Namespace,
			}
			if _, err := s.accountClient.CreateAccount(ctx, createAccountReq); err != nil {
				return localizedError(statusInternal, locale.JaJP)
			}
		}
	}
	return nil
}

func (s *EnvironmentService) UpdateProject(
	ctx context.Context,
	req *environmentproto.UpdateProjectRequest,
) (*environmentproto.UpdateProjectResponse, error) {
	editor, err := s.checkAdminRole(ctx)
	if err != nil {
		return nil, err
	}
	commands := getUpdateProjectCommands(req)
	if err := validateUpdateProjectRequest(req.Id, commands); err != nil {
		return nil, err
	}
	if err := s.updateProject(ctx, req.Id, editor, commands...); err != nil {
		return nil, err
	}
	return &environmentproto.UpdateProjectResponse{}, nil
}

func getUpdateProjectCommands(req *environmentproto.UpdateProjectRequest) []command.Command {
	commands := make([]command.Command, 0)
	if req.ChangeDescriptionCommand != nil {
		commands = append(commands, req.ChangeDescriptionCommand)
	}
	return commands
}

func validateUpdateProjectRequest(id string, commands []command.Command) error {
	if len(commands) == 0 {
		return localizedError(statusNoCommand, locale.JaJP)
	}
	if id == "" {
		return localizedError(statusProjectIDRequired, locale.JaJP)
	}
	return nil
}

func (s *EnvironmentService) updateProject(
	ctx context.Context,
	id string,
	editor *eventproto.Editor,
	commands ...command.Command,
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
		projectStorage := v2es.NewProjectStorage(tx)
		project, err := projectStorage.GetProject(ctx, id)
		if err != nil {
			return err
		}
		handler := command.NewProjectCommandHandler(editor, project, s.publisher)
		for _, command := range commands {
			if err := handler.Handle(ctx, command); err != nil {
				return err
			}
		}
		return projectStorage.UpdateProject(ctx, project)
	})
	if err != nil {
		if err == v2es.ErrProjectNotFound || err == v2es.ErrProjectUnexpectedAffectedRows {
			return localizedError(statusProjectNotFound, locale.JaJP)
		}
		s.logger.Error(
			"Failed to update project",
			log.FieldsFromImcomingContext(ctx).AddFields(zap.Error(err))...,
		)
		return localizedError(statusInternal, locale.JaJP)
	}
	return nil
}

func (s *EnvironmentService) EnableProject(
	ctx context.Context,
	req *environmentproto.EnableProjectRequest,
) (*environmentproto.EnableProjectResponse, error) {
	editor, err := s.checkAdminRole(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateEnableProjectRequest(req); err != nil {
		return nil, err
	}
	if err := s.updateProject(ctx, req.Id, editor, req.Command); err != nil {
		return nil, err
	}
	return &environmentproto.EnableProjectResponse{}, nil
}

func validateEnableProjectRequest(req *environmentproto.EnableProjectRequest) error {
	if req.Command == nil {
		return localizedError(statusNoCommand, locale.JaJP)
	}
	if req.Id == "" {
		return localizedError(statusProjectIDRequired, locale.JaJP)
	}
	return nil
}

func (s *EnvironmentService) DisableProject(
	ctx context.Context,
	req *environmentproto.DisableProjectRequest,
) (*environmentproto.DisableProjectResponse, error) {
	editor, err := s.checkAdminRole(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateDisableProjectRequest(req); err != nil {
		return nil, err
	}
	if err := s.updateProject(ctx, req.Id, editor, req.Command); err != nil {
		return nil, err
	}
	return &environmentproto.DisableProjectResponse{}, nil
}

func validateDisableProjectRequest(req *environmentproto.DisableProjectRequest) error {
	if req.Command == nil {
		return localizedError(statusNoCommand, locale.JaJP)
	}
	if req.Id == "" {
		return localizedError(statusProjectIDRequired, locale.JaJP)
	}
	return nil
}

func (s *EnvironmentService) ConvertTrialProject(
	ctx context.Context,
	req *environmentproto.ConvertTrialProjectRequest,
) (*environmentproto.ConvertTrialProjectResponse, error) {
	editor, err := s.checkAdminRole(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateConvertTrialProjectRequest(req); err != nil {
		return nil, err
	}
	if err := s.updateProject(ctx, req.Id, editor, req.Command); err != nil {
		return nil, err
	}
	return &environmentproto.ConvertTrialProjectResponse{}, nil
}

func validateConvertTrialProjectRequest(req *environmentproto.ConvertTrialProjectRequest) error {
	if req.Command == nil {
		return localizedError(statusNoCommand, locale.JaJP)
	}
	if req.Id == "" {
		return localizedError(statusProjectIDRequired, locale.JaJP)
	}
	return nil
}
