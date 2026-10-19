// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package article

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/api/internal/types"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/common/errmsg"
	"github.com/Sea-Go/Sea-RideTheWind/service/article/rpc/articleservice"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/logger"

	"github.com/zeromicro/go-zero/core/logx"
)

type UploadImageLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUploadImageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UploadImageLogic {
	return &UploadImageLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UploadImageLogic) UploadImage(file multipart.File, header *multipart.FileHeader) (resp *types.UploadImageResp, err error) {
	content, err := io.ReadAll(file)
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorMinioUpload, fmt.Errorf("read uploaded file failed: %w", err))
		return nil, err
	}

	rpcResp, err := l.svcCtx.ArticleRpc.UploadFile(l.ctx, &articleservice.UploadFileRequest{
		Content:  content,
		FileName: header.Filename,
	})
	if err != nil {
		logger.LogBusinessErr(l.ctx, errmsg.ErrorMinioUpload, fmt.Errorf("rpc call UploadFile failed: %w", err))
		return nil, err
	}

	return &types.UploadImageResp{
		ImageUrl: rpcResp.FileUrl,
	}, nil
}
