// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package worker

import (
	"context"

	"sea-try-go/service/knowledge/api/internal/model"
	"sea-try-go/service/knowledge/api/internal/svc"
	"sea-try-go/service/knowledge/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ReadWikiQualitySourceVersionCandidateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewReadWikiQualitySourceVersionCandidateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReadWikiQualitySourceVersionCandidateLogic {
	return &ReadWikiQualitySourceVersionCandidateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ReadWikiQualitySourceVersionCandidateLogic) ReadWikiQualitySourceVersionCandidate(req *types.WikiQualitySourceVersionReq) (resp *types.WikiQualitySourceVersionEnvelope, err error) {
	candidate, err := l.svcCtx.Store.ReadWikiQualitySourceVersionCandidate(l.ctx,
		model.WikiQualitySnapshotTarget{ModuleID: req.ModuleId, PageID: req.PageId,
			FactSetRevisionID: req.FactSetRevisionId, WikiRevisionID: req.WikiRevisionId,
			SourceScopeRevision: req.SourceScopeRevision}, req.SourceVersion)
	if err != nil {
		return nil, err
	}
	return &types.WikiQualitySourceVersionEnvelope{Code: 200, Msg: "success",
		Data: sourceVersionCandidateWire(candidate)}, nil
}

func sourceVersionCandidateWire(candidate model.WikiQualitySourceVersionCandidate) types.WikiQualitySourceVersionCandidate {
	result := types.WikiQualitySourceVersionCandidate{
		SchemaVersion: candidate.SchemaVersion, SourceVersion: candidate.SourceVersion,
		EventSequenceAtRead: candidate.EventSequenceAtRead, EventCount: candidate.EventCount,
		Catalog: types.WikiQualitySourceVersionCatalog{
			FactSetRevisionId:   candidate.Catalog.FactSetRevisionID,
			WikiRevisionId:      candidate.Catalog.WikiRevisionID,
			SourceScopeRevision: candidate.Catalog.SourceScopeRevision,
			EventId:             candidate.Catalog.EventID,
			EventRawSha256:      candidate.Catalog.EventRawSHA256,
			EventJcsSha256:      candidate.Catalog.EventJCSSHA256,
			FactSetJcsSha256:    candidate.Catalog.FactSetJCSSHA256},
		ModuleLifecycleAtRead:         candidate.ModuleLifecycleAtRead,
		WikiWithdrawnAtRead:           candidate.WikiWithdrawnAtRead,
		WikiAndSourcesAvailableAtRead: candidate.WikiAndSourcesAvailableAtRead,
		AllRequiredHaveHead:           candidate.AllRequiredHaveHead,
		Sources:                       make([]types.WikiQualitySourceVersionSource, 0, len(candidate.Sources)),
		RequiredHeads:                 make([]types.WikiQualitySourceVersionRequiredHead, 0, len(candidate.RequiredHeads)),
		Pages:                         make([]types.WikiQualitySourceVersionPage, 0, len(candidate.Pages))}
	for _, source := range candidate.Sources {
		result.Sources = append(result.Sources, types.WikiQualitySourceVersionSource{
			RevisionId: source.RevisionID, ContentSha256: source.ContentSHA256,
			Withdrawn: source.Withdrawn})
	}
	for _, head := range candidate.RequiredHeads {
		result.RequiredHeads = append(result.RequiredHeads,
			types.WikiQualitySourceVersionRequiredHead{
				FactId: head.FactID, SourceRevisionId: head.SourceRevisionID,
				SourceContentSha256: head.SourceContentSHA256, Present: head.Present,
				JudgeRevisionId: head.JudgeRevisionID, EventId: head.EventID,
				EventRawSha256:        head.EventRawSHA256,
				EventJcsSha256:        head.EventJCSSHA256,
				SourceWithdrawnAtRead: head.SourceWithdrawnAtRead})
	}
	for _, page := range candidate.Pages {
		wire := types.WikiQualitySourceVersionPage{FromVersion: page.FromVersion,
			ToVersion: page.ToVersion,
			Events:    make([]types.WikiQualitySourceVersionEvent, 0, len(page.Events))}
		for _, event := range page.Events {
			wire.Events = append(wire.Events, types.WikiQualitySourceVersionEvent{
				AggregateVersion: event.AggregateVersion, EventId: event.EventID,
				EventType: event.EventType, EventJcsSha256: event.EventJCSSHA256,
				JcsSource: event.JCSSource, DeliveredAt: event.DeliveredAt,
				TargetKind: event.TargetKind, TargetId: event.TargetID})
		}
		result.Pages = append(result.Pages, wire)
	}
	return result
}
