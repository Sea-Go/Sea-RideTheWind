package logic

import (
	"context"
	"sort"
	"strconv"

	"sea-try-go/service/common/logger"
	followcommon "sea-try-go/service/follow/common"
	"sea-try-go/service/follow/rpc/internal/svc"
	"sea-try-go/service/follow/rpc/pb"
	"sea-try-go/service/user/user/rpc/userservice"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type recommendCandidate struct {
	TargetID int64
	Score    int32
}

func userLogOption(userID int64) logger.LogOption {
	return logger.WithUserID(strconv.FormatInt(userID, 10))
}

func validateRelationReq(in *pb.RelationReq) error {
	if in == nil || in.GetUserId() <= 0 || in.GetTargetId() <= 0 {
		return followcommon.GRPCError(codes.InvalidArgument, followcommon.ErrorInvalidParam)
	}
	if in.GetUserId() == in.GetTargetId() {
		return followcommon.GRPCError(codes.InvalidArgument, followcommon.ErrorSelfRelation)
	}
	return nil
}

func normalizeListReq(defaultLimit, maxLimit int32, in *pb.ListReq) (int64, int32, int32, error) {
	if in == nil || in.GetUserId() <= 0 {
		return 0, 0, 0, followcommon.GRPCError(codes.InvalidArgument, followcommon.ErrorInvalidParam)
	}

	offset := in.GetOffset()
	if offset < 0 {
		offset = 0
	}

	limit := in.GetLimit()
	if limit <= 0 {
		limit = defaultLimit
	}
	if maxLimit > 0 && limit > maxLimit {
		limit = maxLimit
	}

	return in.GetUserId(), offset, limit, nil
}

func successBaseResp() *pb.BaseResp {
	return &pb.BaseResp{
		Code: int32(followcommon.Success),
		Msg:  followcommon.GetErrMsg(followcommon.Success),
	}
}

func successUserListResp(userIDs []int64) *pb.UserListResp {
	return &pb.UserListResp{
		Code:    int32(followcommon.Success),
		Msg:     followcommon.GetErrMsg(followcommon.Success),
		UserIds: userIDs,
	}
}

func successRecommendResp(users []*pb.RecommendResp_RecommendUser) *pb.RecommendResp {
	return &pb.RecommendResp{
		Code:  int32(followcommon.Success),
		Msg:   followcommon.GetErrMsg(followcommon.Success),
		Users: users,
	}
}

func ensureUserExists(ctx context.Context, svcCtx *svc.ServiceContext, userID int64) error {
	resp, err := svcCtx.UserRpc.GetUser(ctx, &userservice.GetUserReq{Uid: userID})
	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			return followcommon.GRPCError(codes.NotFound, followcommon.ErrorUserNotFound)
		}
		return followcommon.GRPCError(codes.Internal, followcommon.ErrorServerCommon)
	}
	if resp == nil || !resp.Found || resp.User == nil {
		return followcommon.GRPCError(codes.NotFound, followcommon.ErrorUserNotFound)
	}
	return nil
}

func toSet(userIDs []int64) map[int64]struct{} {
	result := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		result[userID] = struct{}{}
	}
	return result
}

func mapKeys(values map[int64]struct{}) []int64 {
	result := make([]int64, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i] < result[j]
	})
	return result
}

func scoreWeight(maxDepth, depth int) int32 {
	if maxDepth < 2 {
		maxDepth = 3
	}
	if depth < 2 {
		depth = 2
	}

	weight := (maxDepth - depth + 1) * 10
	if weight < 10 {
		weight = 10
	}
	return int32(weight)
}

func clampRecommendDepth(depth int) int {
	if depth < 2 {
		return 3
	}
	if depth > 5 {
		return 5
	}
	return depth
}

func clampFanout(fanout int) int {
	if fanout <= 0 {
		return 50
	}
	if fanout > 200 {
		return 200
	}
	return fanout
}
