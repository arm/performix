// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func (s *ApapServer) ListAndroidPackages(ctx context.Context, in *apapproto.ListAndroidPackagesRequest) (*apapproto.ListAndroidPackagesResponse, error) {
	if in == nil {
		return nil, message.New(message.EngineGrpcserverApiApapListAndroidPackagesRequestMissing)
	}
	tgt, err := TargetFromProto(in.GetTarget())
	if err != nil {
		return nil, message.New(message.EngineGrpcserverApiApapListAndroidPackagesTargetInvalid).WithCause(err)
	}
	if _, ok := tgt.(*target.AndroidTarget); !ok {
		return nil, message.New(message.EngineGrpcserverApiApapListAndroidPackagesTargetNotAndroid).
			WithMetadata(map[string]string{"targetType": fmt.Sprintf("%T", tgt)})
	}

	lock := s.targetAccess.LockWithCancellation(tgt, "list Android packages", ctx.Done())
	if lock == nil {
		return nil, message.New(message.EngineGrpcserverApiApapListAndroidPackagesLockCancelled)
	}
	defer lock.Unlock()

	targetSession, err := s.targetSessions.TargetSession(tgt)
	if err != nil {
		return nil, err
	}
	_, err = targetSession.Connect(ctx)
	if err != nil {
		return nil, err
	}
	agentConn, err := targetSession.TargetAgent(ctx)
	if err != nil {
		return nil, err
	}

	stream, err := agentConn.Client.HoldLock(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, message.New(message.EngineGrpcserverApiApapListAndroidPackagesAgentLockFailed).WithCause(err)
	}
	_, err = stream.Recv()
	if err != nil {
		return nil, err
	}

	packageList, err := agentConn.Client.ListAndroidPackages(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return androidPackageListToApapProto(packageList), nil
}

func androidPackageListToApapProto(packageList *targetagentproto.AndroidPackageList) *apapproto.ListAndroidPackagesResponse {
	if packageList == nil {
		return &apapproto.ListAndroidPackagesResponse{}
	}
	packages := make([]*apapproto.AndroidPackage, 0, len(packageList.Packages))
	for _, pkg := range packageList.Packages {
		packages = append(packages, &apapproto.AndroidPackage{
			Name:       pkg.Name,
			Activities: androidActivitiesToApapProto(pkg.Activities),
		})
	}
	return &apapproto.ListAndroidPackagesResponse{Packages: packages}
}

func androidActivitiesToApapProto(activities []*targetagentproto.AndroidActivity) []*apapproto.AndroidActivity {
	protoActivities := make([]*apapproto.AndroidActivity, 0, len(activities))
	for _, activity := range activities {
		protoActivities = append(protoActivities, &apapproto.AndroidActivity{Name: activity.Name})
	}
	return protoActivities
}
