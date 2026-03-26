package imagesecurityservicelogic

import (
	"context"
	"strconv"
	"time"

	"sea-try-go/service/security/rpc/internal/svc"
	pb "sea-try-go/service/security/rpc/pb/sea-try-go/service/security/rpc/pb"
)

type DetectImageAdBatchLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDetectImageAdBatchLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DetectImageAdBatchLogic {
	return &DetectImageAdBatchLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DetectImageAdBatchLogic) DetectImageAdBatch(in *pb.DetectImageAdBatchRequest) (*pb.DetectImageAdBatchResponse, error) {
	started := time.Now()
	if in == nil {
		return &pb.DetectImageAdBatchResponse{
			Success:      false,
			ErrorMessage: "request is nil",
		}, nil
	}

	results := make([]*pb.SingleImageResponse, 0, len(in.GetImages()))
	var successCount int32
	var failedCount int32
	var adCount int32

	for index, image := range in.GetImages() {
		itemStarted := time.Now()
		if image == nil {
			failedCount++
			results = append(results, &pb.SingleImageResponse{
				Success:          false,
				ErrorMessage:     "image item is nil",
				ImageId:          buildBatchImageID(in.GetBatchId(), index),
				ProcessingTimeMs: int32(time.Since(itemStarted).Milliseconds()),
			})
			continue
		}

		resp, err := NewDetectImageAdLogic(l.ctx, l.svcCtx).DetectImageAd(&pb.DetectImageAdRequest{
			ImageUrl:    image.GetImageUrl(),
			ImageBase64: image.GetImageBase64(),
			Options:     in.GetOptions(),
		})
		if err != nil {
			failedCount++
			results = append(results, &pb.SingleImageResponse{
				Success:          false,
				ErrorMessage:     err.Error(),
				ImageId:          buildBatchImageID(in.GetBatchId(), index),
				ProcessingTimeMs: int32(time.Since(itemStarted).Milliseconds()),
			})
			continue
		}

		item := &pb.SingleImageResponse{
			IsAd:             resp.GetIsAd(),
			AdConfidence:     resp.GetAdConfidence(),
			ExtractedText:    resp.GetExtractedText(),
			Success:          resp.GetSuccess(),
			ErrorMessage:     resp.GetErrorMessage(),
			ImageId:          buildBatchImageID(in.GetBatchId(), index),
			ProcessingTimeMs: int32(time.Since(itemStarted).Milliseconds()),
			ImageSizeBytes:   int32(len(image.GetImageBase64()) + len(image.GetImageUrl())),
		}
		if item.Success {
			successCount++
			if item.IsAd {
				adCount++
			}
		} else {
			failedCount++
		}
		results = append(results, item)
	}

	adRate := float32(0)
	if successCount > 0 {
		adRate = float32(adCount) / float32(successCount)
	}

	return &pb.DetectImageAdBatchResponse{
		Results:               results,
		Success:               failedCount == 0,
		ErrorMessage:          batchErrorMessage(failedCount),
		BatchId:               in.GetBatchId(),
		TotalProcessingTimeMs: int32(time.Since(started).Milliseconds()),
		SuccessfulCount:       successCount,
		FailedCount:           failedCount,
		AdDetectionRate:       adRate,
	}, nil
}

func buildBatchImageID(batchID string, index int) string {
	if batchID == "" {
		return "image-" + strconv.Itoa(index+1)
	}
	return batchID + "-" + strconv.Itoa(index+1)
}

func batchErrorMessage(failedCount int32) string {
	if failedCount == 0 {
		return ""
	}
	return strconv.Itoa(int(failedCount)) + " image(s) failed"
}
