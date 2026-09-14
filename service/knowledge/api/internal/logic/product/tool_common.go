package product

import (
	"context"
	"encoding/json"
	"time"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"
	"sea-try-go/service/user/user/identity"
)

func toolSubject(ctx context.Context, service *svc.ServiceContext) (types.AcceptedSubjectRef, error) {
	subject, err := identity.ResolveSubjectRef(ctx, service.UserRpc)
	if err != nil {
		return types.AcceptedSubjectRef{}, err
	}
	return types.AcceptedSubjectRef{AuthorityId: subject.AuthorityID, TenantId: subject.TenantID,
		SubjectId: subject.SubjectID}, nil
}

func toolParentResult(parent model.ToolParent, allowLower bool) types.ToolParentResult {
	return types.ToolParentResult{OperationId: parent.OperationID, ScopeRef: parent.ScopeRef,
		SnapshotRef: parent.SnapshotRef, BudgetRef: parent.BudgetRef, ModuleId: parent.ModuleID,
		DeadlineAtMs: parent.ExpiresAt.UnixMilli(), AllowLowerIntelligence: allowLower,
		Budget: types.ToolBudget{SearchCalls: parent.SearchRemaining, ReadCalls: parent.ReadRemaining,
			QuoteRunes: parent.QuoteRemaining, MaxReadsPerSearch: model.ToolMaxReadsPerSearch,
			MaxQuoteRunesPerSearch: model.ToolMaxRunesPerSearch}}
}

func toolSearchEnvelope(child model.ToolSearch) (*types.ToolSearchEnvelope, error) {
	if child.Status == "complete" {
		var result types.ToolSearchResult
		if err := json.Unmarshal(child.Result, &result); err != nil {
			return nil, model.ErrArtifactUnavailable
		}
		return &types.ToolSearchEnvelope{Code: 200, Msg: "success", Data: result}, nil
	}
	status := "in_flight"
	code := 202
	if child.Status == "failed" {
		status, code = "retryable_failure", 503
	}
	return &types.ToolSearchEnvelope{Code: code, Msg: status,
		Data: types.ToolSearchResult{SearchId: child.SearchID, Status: status,
			Evidence: []types.ToolEvidence{}, Gaps: []string{}, Conflicts: []string{}}}, nil
}

func toolParentLive(parent model.ToolParent) error {
	if time.Now().After(parent.ExpiresAt) {
		return model.ErrUnavailable
	}
	return nil
}

func verifiedToolSearchEnvelope(ctx context.Context, service *svc.ServiceContext,
	parent model.ToolParent, child model.ToolSearch) (*types.ToolSearchEnvelope, error) {
	var result types.ToolSearchResult
	if err := json.Unmarshal(child.Result, &result); err != nil {
		return nil, model.ErrArtifactUnavailable
	}
	if err := service.Store.VerifyToolSearchResult(ctx, parent, child, result,
		service.Config.SearchTools.AllowLowerIntelligence); err != nil {
		return nil, err
	}
	return &types.ToolSearchEnvelope{Code: 200, Msg: "success", Data: result}, nil
}
