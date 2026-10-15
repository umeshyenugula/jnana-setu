// Resource clients for Modules 1, 2, 3 (status), and 6 (test chat).
// All authorization is enforced by the backend; these functions only shape
// requests and parse the documented envelopes.
import type { ApiClient } from "./client";
import type {
  ChatAnswer,
  KnowledgeDocument,
  Membership,
  MemberRole,
  Organization,
  ProcessingJob,
  DocumentVersion,
  Widget,
} from "./types";

export const organizations = {
  list: (c: ApiClient) =>
    c.get<{ organizations: Organization[] }>("/api/v1/organizations"),
  create: (c: ApiClient, name: string, slug: string) =>
    c.postJson<{ organization: Organization; membership: Membership }>("/api/v1/organizations", { name, slug }),
  members: (c: ApiClient, orgId: string) =>
    c.get<{ members: Membership[] }>(`/api/v1/organizations/${orgId}/members`),
  addMember: (c: ApiClient, orgId: string, userId: string, role: MemberRole) =>
    c.postJson<Membership>(`/api/v1/organizations/${orgId}/members`, { userId, role }),
};

const knowledgeBase = (orgId: string) => `/api/v1/organizations/${orgId}/knowledge`;

export const knowledge = {
  list: (c: ApiClient, orgId: string, includeInactive = true) =>
    c.get<{ documents: KnowledgeDocument[] }>(
      `${knowledgeBase(orgId)}?includeInactive=${includeInactive ? "true" : "false"}`,
    ),
  get: (c: ApiClient, orgId: string, docId: string) =>
    c.get<{ document: KnowledgeDocument; currentVersion: DocumentVersion }>(
      `${knowledgeBase(orgId)}/${docId}`,
    ),
  versions: (c: ApiClient, orgId: string, docId: string) =>
    c.get<{ versions: DocumentVersion[] }>(`${knowledgeBase(orgId)}/${docId}/versions`),
  status: (c: ApiClient, orgId: string, docId: string) =>
    c.get<{ documentId: string; isActive: boolean; processingStatus: string; currentVersionId: string | null; deleted: boolean }>(
      `${knowledgeBase(orgId)}/${docId}/status`,
    ),
  createText: (c: ApiClient, orgId: string, title: string, contentText: string) =>
    c.postJson<{ document: KnowledgeDocument }>(`${knowledgeBase(orgId)}/text`, { title, contentText }),
  uploadFile: (c: ApiClient, orgId: string, title: string, file: File) => {
    const form = new FormData();
    form.append("file", file);
    if (title.trim()) form.append("title", title.trim());
    return c.postForm<{ document: KnowledgeDocument }>(`${knowledgeBase(orgId)}/files`, form);
  },
  addTextVersion: (c: ApiClient, orgId: string, docId: string, contentText: string) =>
    c.postJson<{ version: DocumentVersion }>(`${knowledgeBase(orgId)}/${docId}/versions`, { contentText }),
  setActive: (c: ApiClient, orgId: string, docId: string, isActive: boolean) =>
    c.patchJson<{ document: KnowledgeDocument }>(`${knowledgeBase(orgId)}/${docId}`, { isActive }),
  reprocess: (c: ApiClient, orgId: string, docId: string) =>
    c.postJson<unknown>(`${knowledgeBase(orgId)}/${docId}/reprocess`, {}),
  remove: (c: ApiClient, orgId: string, docId: string) =>
    c.del<{ deleted: boolean }>(`${knowledgeBase(orgId)}/${docId}`),
};

export const widgets = {
  list: (c: ApiClient, orgId: string) =>
    c.get<{ widgets: Widget[] }>(`/api/v1/organizations/${orgId}/widgets`),
  create: (c: ApiClient, orgId: string, displayName: string, allowedOrigins: string[]) =>
    c.postJson<Widget>(`/api/v1/organizations/${orgId}/widgets`, { displayName, allowedOrigins }),
  update: (c: ApiClient, orgId: string, widgetId: string, patch: { displayName?: string; allowedOrigins?: string[]; active?: boolean }) =>
    c.patchJson<Widget>(`/api/v1/organizations/${orgId}/widgets/${widgetId}`, patch),
};

export const processing = {
  getJob: (c: ApiClient, jobId: string) =>
    c.get<{ job: ProcessingJob }>(`/api/v1/processing/jobs/${jobId}`),
};

// Test chat reuses the public Module 6 chat endpoint with the widget's public
// ID — exactly what the embedded widget will call.
export const testChat = {
  send: (c: ApiClient, widgetId: string, message: string, sessionId?: string) =>
    c.postJson<ChatAnswer>("/api/v1/chat", {
      widgetId,
      ...(sessionId ? { sessionId } : {}),
      message,
    }),
};
