import type {
  ListModulesReqParams,
  ModulePageReqParams,
  PublishedRevisionsReqParams,
  ListModulesResp,
  ListRevisionsResp,
  ListReleasesResp,
  ListBuildsResp,
  ListCompilesResp,
  Revision,
} from "../generated/typescript/knowledgeComponents"

// These are valid wire requests/responses captured in the HTTP contract tests.
// The generated consumer must accept the same omitted parameters/terminal fields.
export const defaultModulePage: ListModulesReqParams = {}
export const defaultResourcePage: ModulePageReqParams = {}
export const defaultPublishedPage: PublishedRevisionsReqParams = {}
export const terminalModules: ListModulesResp = { items: [] }
export const terminalRevisions: ListRevisionsResp = { items: [] }
export const terminalReleases: ListReleasesResp = { items: [] }
export const terminalBuilds: ListBuildsResp = { items: [] }
export const terminalCompiles: ListCompilesResp = { items: [] }

type Optional<T, K extends keyof T> = {} extends Pick<T, K> ? true : false
export const metadataMayOmitBody: Optional<Revision, "content"> = true
