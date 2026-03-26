package logic

import (
	"context"
	"sort"
	"time"

	"sea-try-go/service/common/logger"
	followcommon "sea-try-go/service/follow/common"
	"sea-try-go/service/follow/rpc/internal/metrics"
	"sea-try-go/service/follow/rpc/internal/svc"
	"sea-try-go/service/follow/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
)

type GetRecommendationsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetRecommendationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRecommendationsLogic {
	return &GetRecommendationsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetRecommendationsLogic) GetRecommendations(in *pb.ListReq) (resp *pb.RecommendResp, err error) {
	started := time.Now()
	defer func() {
		metrics.ObserveRPC("GetRecommendations", started, err)
	}()

	userID, offset, limit, err := normalizeListReq(
		l.svcCtx.Config.Recommend.DefaultLimit,
		l.svcCtx.Config.Recommend.MaxLimit,
		in,
	)
	if err != nil {
		logger.LogBusinessErr(l.ctx, followcommon.BizCodeFromError(err), err)
		return nil, err
	}

	if err = ensureUserExists(l.ctx, l.svcCtx, userID); err != nil {
		logger.LogBusinessErr(l.ctx, followcommon.BizCodeFromError(err), err, userLogOption(userID))
		return nil, err
	}

	fanout := clampFanout(l.svcCtx.Config.Recommend.MaxFanout)
	depth := clampRecommendDepth(l.svcCtx.Config.Recommend.MaxDepth)

	directFollows, dbErr := l.svcCtx.FollowModel.ListAllFollowTargets(l.ctx, userID, fanout)
	if dbErr != nil {
		metrics.ObserveDBError("get_recommendations", "list_direct_follows")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorDbSelect, dbErr, userLogOption(userID))
		return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbSelect)
	}
	if len(directFollows) == 0 {
		metrics.ObserveRecommendationSize(0)
		return successRecommendResp(nil), nil
	}

	blockedTargets, dbErr := l.svcCtx.FollowModel.ListAllBlockTargets(l.ctx, userID)
	if dbErr != nil {
		metrics.ObserveDBError("get_recommendations", "list_block_targets")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorDbSelect, dbErr, userLogOption(userID))
		return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbSelect)
	}

	blockerUsers, dbErr := l.svcCtx.FollowModel.ListBlockerUsers(l.ctx, userID)
	if dbErr != nil {
		metrics.ObserveDBError("get_recommendations", "list_blocker_users")
		logger.LogBusinessErr(l.ctx, followcommon.ErrorDbSelect, dbErr, userLogOption(userID))
		return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbSelect)
	}

	directSet := toSet(directFollows)
	blockedSet := toSet(blockedTargets)
	blockerSet := toSet(blockerUsers)

	currentLayer := make([]int64, 0, len(directFollows))
	seenLayer := make(map[int64]struct{}, len(directFollows))
	for _, followID := range directFollows {
		if _, blocked := blockedSet[followID]; blocked {
			continue
		}
		if _, blocked := blockerSet[followID]; blocked {
			continue
		}
		if _, exists := seenLayer[followID]; exists {
			continue
		}
		seenLayer[followID] = struct{}{}
		currentLayer = append(currentLayer, followID)
	}

	expanded := make(map[int64]struct{}, len(currentLayer)+1)
	expanded[userID] = struct{}{}
	for _, followID := range currentLayer {
		expanded[followID] = struct{}{}
	}

	scores := make(map[int64]int32)
	for currentDepth := 2; currentDepth <= depth && len(currentLayer) > 0; currentDepth++ {
		nextLayerSet := make(map[int64]struct{})
		for _, sourceUserID := range currentLayer {
			targets, listErr := l.svcCtx.FollowModel.ListAllFollowTargets(l.ctx, sourceUserID, fanout)
			if listErr != nil {
				metrics.ObserveDBError("get_recommendations", "list_layer_follow_targets")
				logger.LogBusinessErr(l.ctx, followcommon.ErrorDbSelect, listErr, userLogOption(userID))
				return nil, followcommon.GRPCError(codes.Internal, followcommon.ErrorDbSelect)
			}

			for _, candidateID := range targets {
				if candidateID == userID {
					continue
				}
				if _, blocked := blockedSet[candidateID]; blocked {
					continue
				}
				if _, blocked := blockerSet[candidateID]; blocked {
					continue
				}
				if _, followed := directSet[candidateID]; followed {
					continue
				}

				scores[candidateID] += scoreWeight(depth, currentDepth)
				if currentDepth < depth {
					if _, exists := expanded[candidateID]; !exists {
						nextLayerSet[candidateID] = struct{}{}
					}
				}
			}
		}

		currentLayer = mapKeys(nextLayerSet)
		for _, candidateID := range currentLayer {
			expanded[candidateID] = struct{}{}
		}
	}

	candidates := make([]recommendCandidate, 0, len(scores))
	for candidateID, score := range scores {
		if score <= 0 {
			continue
		}
		candidates = append(candidates, recommendCandidate{
			TargetID: candidateID,
			Score:    score,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].TargetID < candidates[j].TargetID
		}
		return candidates[i].Score > candidates[j].Score
	})

	start := int(offset)
	if start >= len(candidates) {
		metrics.ObserveRecommendationSize(0)
		return successRecommendResp(nil), nil
	}

	end := start + int(limit)
	if end > len(candidates) {
		end = len(candidates)
	}

	users := make([]*pb.RecommendResp_RecommendUser, 0, end-start)
	for _, candidate := range candidates[start:end] {
		users = append(users, &pb.RecommendResp_RecommendUser{
			TargetId:    candidate.TargetID,
			MutualScore: candidate.Score,
		})
	}

	metrics.ObserveRecommendationSize(len(users))
	logger.LogInfo(l.ctx, "recommendations generated", userLogOption(userID))
	return successRecommendResp(users), nil
}
