package knowledge

import (
	"context"
	"errors"
	"testing"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
)

func TestGroundingReviewAuthorityDefaultsOff(t *testing.T) {
	service := &svc.ServiceContext{}
	if _, err := NewRegisterReviewerKeyLogic(context.Background(), service).
		RegisterReviewerKey(&types.RegisterReviewerKeyReq{}); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("key registration enabled without operator switch: %v", err)
	}
	if _, err := NewRevokeReviewerKeyLogic(context.Background(), service).
		RevokeReviewerKey(&types.RevokeReviewerKeyReq{}); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("key revocation enabled without operator switch: %v", err)
	}
	if _, err := NewSubmitGroundingReviewLogic(context.Background(), service).
		SubmitGroundingReview(&types.SubmitGroundingReviewReq{}); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("signed review enabled without operator switch: %v", err)
	}
}
