import webapi from "./gocliRequest"
import * as components from "./knowledgeComponents"
export * from "./knowledgeComponents"

/**
 * @description
 * @param params
 */
export function listModules(params: components.ListModulesReqParams) {
	return webapi.get<components.ListModulesRespEnvelope>(`/v1/knowledge/modules`, params)
}

/**
 * @description
 * @param params
 */
export function getModule(params: components.ModulePathParams, module_id: string) {
	return webapi.get<components.ModuleEnvelope>(`/v1/knowledge/modules/${module_id}`, params)
}

/**
 * @description
 * @param params
 */
export function getPublishedRelease(params: components.ModulePathParams, module_id: string) {
	return webapi.get<components.ReleaseEnvelope>(`/v1/knowledge/modules/${module_id}/published`, params)
}

/**
 * @description
 * @param params
 */
export function getHistoricalRelease(params: components.ModuleReleasePathParams, module_id: string, release_id: string) {
	return webapi.get<components.ReleaseEnvelope>(`/v1/knowledge/modules/${module_id}/published-releases/${release_id}`, params)
}

/**
 * @description
 * @param params
 */
export function listPublishedRevisions(params: components.PublishedRevisionsReqParams, module_id: string, release_id: string) {
	return webapi.get<components.ListRevisionsRespEnvelope>(`/v1/knowledge/modules/${module_id}/releases/${release_id}/revisions`, params)
}

/**
 * @description
 * @param params
 */
export function getPublishedRevision(params: components.PublishedRevisionPathParams, module_id: string, release_id: string, revision_id: string) {
	return webapi.get<components.RevisionEnvelope>(`/v1/knowledge/modules/${module_id}/releases/${release_id}/revisions/${revision_id}`, params)
}

/**
 * @description
 * @param req
 */
export function submitGroundingReview(req: components.SubmitGroundingReviewReq) {
	return webapi.post<components.GroundingReviewReceiptEnvelope>(`/v1/knowledge/answer-grounding/reviews`, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function cancelBuild(params: components.CancelBuildReqParams, req: components.CancelBuildReq, build_id: string) {
	return webapi.post<components.BuildEnvelope>(`/v1/knowledge/builds/${build_id}/cancel`, params, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function cancelCompile(params: components.CancelCompileReqParams, req: components.CancelCompileReq, compile_id: string) {
	return webapi.post<components.CompileEnvelope>(`/v1/knowledge/compiles/${compile_id}/cancel`, params, req)
}

/**
 * @description
 * @param req
 */
export function createModule(req: components.CreateModuleReq) {
	return webapi.post<components.ModuleEnvelope>(`/v1/knowledge/modules`, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function activate(params: components.ActivateReqParams, req: components.ActivateReq, module_id: string) {
	return webapi.put<components.ReleaseStateEnvelope>(`/v1/knowledge/modules/${module_id}/activation`, params, req)
}

/**
 * @description
 * @param params
 */
export function listBuilds(params: components.ModulePageReqParams, module_id: string) {
	return webapi.get<components.ListBuildsRespEnvelope>(`/v1/knowledge/modules/${module_id}/builds`, params)
}

/**
 * @description
 * @param params
 */
export function getModuleBuild(params: components.ModuleBuildPathParams, module_id: string, build_id: string) {
	return webapi.get<components.BuildEnvelope>(`/v1/knowledge/modules/${module_id}/builds/${build_id}`, params)
}

/**
 * @description
 * @param params
 */
export function listCompiles(params: components.ModulePageReqParams, module_id: string) {
	return webapi.get<components.ListCompilesRespEnvelope>(`/v1/knowledge/modules/${module_id}/compiles`, params)
}

/**
 * @description
 * @param params
 * @param req
 */
export function createCompile(params: components.CreateCompileReqParams, req: components.CreateCompileReq, module_id: string) {
	return webapi.post<components.CompileEnvelope>(`/v1/knowledge/modules/${module_id}/compiles`, params, req)
}

/**
 * @description
 * @param params
 */
export function getModuleCompile(params: components.ModuleCompilePathParams, module_id: string, compile_id: string) {
	return webapi.get<components.CompileEnvelope>(`/v1/knowledge/modules/${module_id}/compiles/${compile_id}`, params)
}

/**
 * @description
 * @param params
 */
export function listReleases(params: components.ModulePageReqParams, module_id: string) {
	return webapi.get<components.ListReleasesRespEnvelope>(`/v1/knowledge/modules/${module_id}/releases`, params)
}

/**
 * @description
 * @param params
 * @param req
 */
export function createRelease(params: components.CreateReleaseReqParams, req: components.CreateReleaseReq, module_id: string) {
	return webapi.post<components.ReleaseEnvelope>(`/v1/knowledge/modules/${module_id}/releases`, params, req)
}

/**
 * @description
 * @param params
 */
export function getModuleRelease(params: components.ModuleReleasePathParams, module_id: string, release_id: string) {
	return webapi.get<components.ReleaseEnvelope>(`/v1/knowledge/modules/${module_id}/releases/${release_id}`, params)
}

/**
 * @description
 * @param params
 */
export function currentRelease(params: components.ModulePathParams, module_id: string) {
	return webapi.get<components.ReleaseStateEnvelope>(`/v1/knowledge/modules/${module_id}/releases/current`, params)
}

/**
 * @description
 * @param params
 */
export function listRevisions(params: components.ModulePageReqParams, module_id: string) {
	return webapi.get<components.ListRevisionsRespEnvelope>(`/v1/knowledge/modules/${module_id}/revisions`, params)
}

/**
 * @description
 * @param params
 */
export function getModuleRevision(params: components.ModuleRevisionPathParams, module_id: string, revision_id: string) {
	return webapi.get<components.RevisionEnvelope>(`/v1/knowledge/modules/${module_id}/revisions/${revision_id}`, params)
}

/**
 * @description
 * @param params
 * @param req
 */
export function recordSearchJudgment(params: components.RecordSearchJudgmentReqParams, req: components.RecordSearchJudgmentReq, module_id: string) {
	return webapi.post<components.SearchJudgmentReceiptEnvelope>(`/v1/knowledge/modules/${module_id}/search-judgments`, params, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function withdrawSearchJudgment(params: components.WithdrawSearchJudgmentReqParams, req: components.WithdrawSearchJudgmentReq, module_id: string) {
	return webapi.post<components.SearchJudgmentReceiptEnvelope>(`/v1/knowledge/modules/${module_id}/search-judgments/withdrawals`, params, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function createSource(params: components.CreateSourceReqParams, req: components.CreateSourceReq, module_id: string) {
	return webapi.post<components.RevisionEnvelope>(`/v1/knowledge/modules/${module_id}/sources`, params, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function createWiki(params: components.CreateWikiReqParams, req: components.CreateWikiReq, module_id: string, page_id: string) {
	return webapi.post<components.RevisionEnvelope>(`/v1/knowledge/modules/${module_id}/wiki-pages/${page_id}/revisions`, params, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function withdraw(params: components.WithdrawReqParams, req: components.WithdrawReq, module_id: string) {
	return webapi.post<components.ReceiptEnvelope>(`/v1/knowledge/modules/${module_id}/withdrawals`, params, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function createBuild(params: components.CreateBuildReqParams, req: components.CreateBuildReq, release_id: string) {
	return webapi.post<components.BuildEnvelope>(`/v1/knowledge/releases/${release_id}/index-builds`, params, req)
}

/**
 * @description
 * @param req
 */
export function registerReviewerKey(req: components.RegisterReviewerKeyReq) {
	return webapi.post<components.ReviewerKeyRecordEnvelope>(`/v1/knowledge/reviewer-keys`, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function revokeReviewerKey(params: components.RevokeReviewerKeyReqParams, req: components.RevokeReviewerKeyReq, key_id: string) {
	return webapi.post<components.ReviewerKeyRecordEnvelope>(`/v1/knowledge/reviewer-keys/${key_id}/revoke`, params, req)
}

/**
 * @description
 * @param params
 */
export function listDraftModules(params: components.ListModulesReqParams) {
	return webapi.get<components.ListModulesRespEnvelope>(`/v1/knowledge/workbench/modules`, params)
}

/**
 * @description
 * @param params
 */
export function getDraftModule(params: components.ModulePathParams, module_id: string) {
	return webapi.get<components.ModuleEnvelope>(`/v1/knowledge/workbench/modules/${module_id}`, params)
}

/**
 * @description
 * @param params
 */
export function listProductAcceptedAnswers(params: components.ProductAcceptedAnswersReqParams, session_id: string) {
	return webapi.get<components.AcceptedAnswersPageEnvelope>(`/v1/knowledge/answer-sessions/${session_id}/accepted-answers`, params)
}

/**
 * @description
 * @param params
 */
export function getProductAcceptedAnswer(params: components.ProductAcceptedAnswerReqParams, session_id: string, answer_id: string) {
	return webapi.get<components.AcceptedAnswerEnvelope>(`/v1/knowledge/answer-sessions/${session_id}/accepted-answers/${answer_id}`, params)
}

/**
 * @description
 * @param params
 */
export function getProductAnswerCitationStates(params: components.ProductAcceptedAnswerReqParams, session_id: string, answer_id: string) {
	return webapi.get<components.ProductAnswerCitationStatesEnvelope>(`/v1/knowledge/answer-sessions/${session_id}/accepted-answers/${answer_id}/citations`, params)
}

/**
 * @description
 * @param params
 * @param req
 */
export function createProductSearch(params: components.ProductSearchReqParams, req: components.ProductSearchReq, session_id: string) {
	return webapi.post<components.ProductSearchEnvelope>(`/v1/knowledge/answer-sessions/${session_id}/searches`, params, req)
}

/**
 * @description
 * @param params
 */
export function getProductSearch(params: components.ProductSearchPathParams, session_id: string, search_id: string) {
	return webapi.get<components.ProductSearchEnvelope>(`/v1/knowledge/answer-sessions/${session_id}/searches/${search_id}`, params)
}

/**
 * @description
 * @param params
 * @param req
 */
export function createToolParent(params: components.ToolParentReqParams, req: components.ToolParentReq, session_id: string) {
	return webapi.post<components.ToolParentEnvelope>(`/v1/knowledge/answer-sessions/${session_id}/tool-runs`, params, req)
}

/**
 * @description
 * @param params
 */
export function getToolParent(params: components.ToolParentPathParams, session_id: string, operation_id: string) {
	return webapi.get<components.ToolParentEnvelope>(`/v1/knowledge/answer-sessions/${session_id}/tool-runs/${operation_id}`, params)
}

/**
 * @description
 * @param params
 * @param req
 */
export function readToolEvidence(params: components.ToolReadReqParams, req: components.ToolReadReq, session_id: string, operation_id: string) {
	return webapi.post<components.ToolReadEnvelope>(`/v1/knowledge/answer-sessions/${session_id}/tool-runs/${operation_id}/evidence-reads`, params, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function createToolSearch(params: components.ToolSearchReqParams, req: components.ToolSearchReq, session_id: string, operation_id: string) {
	return webapi.post<components.ToolSearchEnvelope>(`/v1/knowledge/answer-sessions/${session_id}/tool-runs/${operation_id}/searches`, params, req)
}

/**
 * @description
 * @param params
 */
export function getToolSearch(params: components.ToolSearchPathParams, session_id: string, operation_id: string, search_id: string) {
	return webapi.get<components.ToolSearchEnvelope>(`/v1/knowledge/answer-sessions/${session_id}/tool-runs/${operation_id}/searches/${search_id}`, params)
}

/**
 * @description
 * @param req
 */
export function commitAcceptedAnswer(req: components.CommitAcceptedAnswerReq) {
	return webapi.post<components.AcceptedAnswerEnvelope>(`/internal/v1/knowledge/accepted-answers`, req)
}

/**
 * @description
 * @param params
 */
export function listAcceptedAnswers(params: components.ListAcceptedAnswersReqParams) {
	return webapi.get<components.AcceptedAnswersPageEnvelope>(`/internal/v1/knowledge/accepted-answers`, params)
}

/**
 * @description
 * @param params
 */
export function getAcceptedAnswer(params: components.GetAcceptedAnswerReqParams, answer_id: string) {
	return webapi.get<components.AcceptedAnswerEnvelope>(`/internal/v1/knowledge/accepted-answers/${answer_id}`, params)
}

/**
 * @description
 * @param params
 */
export function getGroundingReview(params: components.GroundingReviewCasePathParams, case_sha256: string) {
	return webapi.get<components.GroundingReviewReceiptEnvelope>(`/internal/v1/knowledge/answer-grounding/reviews/${case_sha256}`, params)
}

/**
 * @description
 * @param params
 */
export function getBuild(params: components.BuildPathParams, build_id: string) {
	return webapi.get<components.BuildEnvelope>(`/internal/v1/knowledge/builds/${build_id}`, params)
}

/**
 * @description
 * @param params
 * @param req
 */
export function claimBuild(params: components.ClaimBuildReqParams, req: components.ClaimBuildReq, build_id: string) {
	return webapi.post<components.BuildEnvelope>(`/internal/v1/knowledge/builds/${build_id}/claim`, params, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function acceptBuild(params: components.AcceptBuildReqParams, req: components.AcceptBuildReq, build_id: string) {
	return webapi.post<components.BuildEnvelope>(`/internal/v1/knowledge/builds/${build_id}/results`, params, req)
}

/**
 * @description
 * @param params
 */
export function getCompile(params: components.CompilePathParams, compile_id: string) {
	return webapi.get<components.CompileEnvelope>(`/internal/v1/knowledge/compiles/${compile_id}`, params)
}

/**
 * @description
 * @param params
 * @param req
 */
export function claimCompile(params: components.ClaimCompileReqParams, req: components.ClaimCompileReq, compile_id: string) {
	return webapi.post<components.CompileEnvelope>(`/internal/v1/knowledge/compiles/${compile_id}/claim`, params, req)
}

/**
 * @description
 * @param params
 * @param req
 */
export function acceptCompile(params: components.AcceptCompileReqParams, req: components.AcceptCompileReq, compile_id: string) {
	return webapi.post<components.CompileEnvelope>(`/internal/v1/knowledge/compiles/${compile_id}/results`, params, req)
}

/**
 * @description
 * @param params
 */
export function getCurrentSearchSnapshot(params: components.ModulePathParams, module_id: string) {
	return webapi.get<components.SearchSnapshotEnvelope>(`/internal/v1/knowledge/modules/${module_id}/search-snapshot`, params)
}

/**
 * @description
 * @param params
 */
export function getRelease(params: components.ReleasePathParams, release_id: string) {
	return webapi.get<components.ReleaseEnvelope>(`/internal/v1/knowledge/releases/${release_id}`, params)
}

/**
 * @description
 * @param params
 */
export function getReviewerKey(params: components.ReviewerKeyPathParams, key_id: string) {
	return webapi.get<components.ReviewerKeyRecordEnvelope>(`/internal/v1/knowledge/reviewer-keys/${key_id}`, params)
}

/**
 * @description
 * @param params
 */
export function getRevision(params: components.RevisionPathParams, revision_id: string) {
	return webapi.get<components.RevisionEnvelope>(`/internal/v1/knowledge/revisions/${revision_id}`, params)
}

/**
 * @description
 * @param req
 */
export function acceptSearchCitations(req: components.AcceptSearchCitationsReq) {
	return webapi.post<components.SearchCitationReceiptEnvelope>(`/internal/v1/knowledge/search-citations`, req)
}

/**
 * @description
 * @param params
 */
export function getSearchCitations(params: components.SearchCitationPathParams, search_id: string) {
	return webapi.get<components.SearchCitationRecordEnvelope>(`/internal/v1/knowledge/search-citations/${search_id}`, params)
}

/**
 * @description
 * @param params
 */
export function getSearchJudgmentEvent(params: components.SearchJudgmentEventPathParams, event_id: string) {
	return webapi.get<components.SearchJudgmentEventReceiptEnvelope>(`/internal/v1/knowledge/search-judgments/events/${event_id}`, params)
}

/**
 * @description
 * @param req
 */
export function readSearchSource(req: components.ReadSearchSourceReq) {
	return webapi.post<components.CitationChunkEnvelope>(`/internal/v1/knowledge/search-sources/read`, req)
}
