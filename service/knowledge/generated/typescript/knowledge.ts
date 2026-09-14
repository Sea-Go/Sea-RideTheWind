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
 * @param req
 */
export function createCompile(params: components.CreateCompileReqParams, req: components.CreateCompileReq, module_id: string) {
	return webapi.post<components.CompileEnvelope>(`/v1/knowledge/modules/${module_id}/compiles`, params, req)
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
export function currentRelease(params: components.ModulePathParams, module_id: string) {
	return webapi.get<components.ReleaseStateEnvelope>(`/v1/knowledge/modules/${module_id}/releases/current`, params)
}

/**
 * @description
 * @param params
 */
export function listRevisions(params: components.ModulePathParams, module_id: string) {
	return webapi.get<components.ListRevisionsRespEnvelope>(`/v1/knowledge/modules/${module_id}/revisions`, params)
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
 * @param params
 */
export function listDraftModules(params: components.ListModulesReqParams) {
	return webapi.get<components.ListModulesRespEnvelope>(`/v1/knowledge/workbench/modules`, params)
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
export function getRelease(params: components.ReleasePathParams, release_id: string) {
	return webapi.get<components.ReleaseEnvelope>(`/internal/v1/knowledge/releases/${release_id}`, params)
}

/**
 * @description
 * @param params
 */
export function getRevision(params: components.RevisionPathParams, revision_id: string) {
	return webapi.get<components.RevisionEnvelope>(`/internal/v1/knowledge/revisions/${revision_id}`, params)
}
