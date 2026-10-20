// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package comment

import (
	"context"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/rpc/pb"
	"strings"

	"github.com/Sea-Go/Sea-RideTheWind/service/comment/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/comment/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetCommentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetCommentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCommentLogic {
	return &GetCommentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetCommentLogic) GetComment(req *types.GetCommentReq) (resp *types.GetCommentResp, err error) {
	if strings.TrimSpace(req.TargetType) == "" {
		return nil, status.Error(codes.InvalidArgument, "target_type is required")
	}
	if strings.TrimSpace(req.TargetId) == "" {
		return nil, status.Error(codes.InvalidArgument, "target_id is required")
	}

	// sortType: 0=热度 1=时间（你 proto 定义）
	if req.SortType != 0 && req.SortType != 1 {
		req.SortType = 0
	}

	// 分页兜底
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 50 {
		req.PageSize = 50
	}

	// rootId < 0 没意义，兜底成 0（查根评论）
	if req.RootId < 0 {
		req.RootId = 0
	}

	// 2) 调 RPC
	// 注意：这里的 CommentRpc 字段名请按你的 svcCtx 实际名称修改
	rpcResp, err := l.svcCtx.CommentCli.GetComment(l.ctx, &pb.GetCommentReq{
		TargetType: req.TargetType,
		TargetId:   req.TargetId,
		SortType:   req.SortType,
		RootId:     req.RootId,
		Page:       req.Page,
		PageSize:   req.PageSize,
	})
	if err != nil {
		// 如果你项目里有 errorx，可在这里做统一错误转换
		return nil, err
	}

	// 3) RPC -> API 类型映射
	resp = &types.GetCommentResp{
		Comment: make([]types.CommentItem, 0),
	}

	// 映射 subject
	if rpcResp.GetSubject() != nil {
		resp.Subject = types.SubjectInfo{
			TargetType: rpcResp.GetSubject().GetTargetType(),
			TargetId:   rpcResp.GetSubject().GetTargetId(),
			TotalCount: rpcResp.GetSubject().GetTotalCount(),
			RootCount:  rpcResp.GetSubject().GetRootCount(),
			State:      int32(rpcResp.GetSubject().GetState()),
			Attribute:  rpcResp.GetSubject().GetAttribute(),
			OwnerId:    0,
		}
	}

	// 映射评论列表（包含 children 递归）
	if len(rpcResp.GetComment()) > 0 {
		resp.Comment = make([]types.CommentItem, 0, len(rpcResp.GetComment()))
		for _, item := range rpcResp.GetComment() {
			resp.Comment = append(resp.Comment, convertPbCommentItemToAPI(item))
		}
	}

	return resp, nil
}

func convertPbCommentItemToAPI(in *pb.CommentItem) types.CommentItem {
	if in == nil {
		return types.CommentItem{}
	}

	out := types.CommentItem{
		Id:           in.GetId(),
		UserId:       in.GetUserId(),
		Content:      in.GetContent(),
		RootId:       in.GetRootId(),
		ParentId:     in.GetParentId(),
		LikeCount:    in.GetLikeCount(),
		DislikeCount: in.GetDislikeCount(),
		ReplyCount:   in.GetReplyCount(),
		Attribute:    in.GetAttribute(),
		State:        int32(in.GetState()),
		CreatedAt:    in.GetCreatedAt(),
		Meta:         in.GetMeta(),
		Children:     make([]types.CommentItem, 0),
	}

	if len(in.GetChildren()) > 0 {
		out.Children = make([]types.CommentItem, 0, len(in.GetChildren()))
		for _, child := range in.GetChildren() {
			out.Children = append(out.Children, convertPbCommentItemToAPI(child))
		}
	}

	return out
}
