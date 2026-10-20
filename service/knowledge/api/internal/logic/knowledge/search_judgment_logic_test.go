package knowledge

import (
	"context"
	"errors"
	"testing"

	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/model"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/svc"
	"github.com/Sea-Go/Sea-RideTheWind/service/knowledge/api/internal/types"
)

func TestHumanJudgmentCaptureDefaultsOff(t *testing.T) {
	ctx := &svc.ServiceContext{}
	_, err := NewRecordSearchJudgmentLogic(context.Background(), ctx).
		RecordSearchJudgment(&types.RecordSearchJudgmentReq{})
	if !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("recording enabled without operator switch: %v", err)
	}
	_, err = NewWithdrawSearchJudgmentLogic(context.Background(), ctx).
		WithdrawSearchJudgment(&types.WithdrawSearchJudgmentReq{})
	if !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("withdrawal enabled without operator switch: %v", err)
	}
}
