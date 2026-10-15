// Shared Module 0 envelope shapes. Mirrors the Go api package.
export interface ApiMeta {
  correlationId: string;
  timestamp: string;
  version: string;
  path: string;
}

export interface ApiSuccess<T> {
  success: true;
  data: T;
  meta: ApiMeta;
}

export interface ApiFailure {
  success: false;
  error: { code: string; message: string };
  meta: ApiMeta;
}

export type ApiEnvelope<T> = ApiSuccess<T> | ApiFailure;

// Module 1 shapes.
export interface Organization {
  id: string;
  name: string;
  slug: string;
  creatorUserId: string;
  status: "active" | "suspended";
  createdAt: string;
  updatedAt: string;
}

export type MemberRole = "owner" | "admin" | "member" | "viewer";

export interface Membership {
  id: string;
  organizationId: string;
  userId: string;
  role: MemberRole;
  createdAt: string;
  updatedAt: string;
}

export interface Widget {
  id: string;
  organizationId: string;
  publicWidgetId: string;
  displayName: string;
  allowedOrigins: string[];
  active: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface WidgetPublicConfig {
  publicWidgetId: string;
  displayName: string;
  allowedOrigins: string[];
  active: boolean;
}

// Module 2 shapes.
export type ProcessingStatus = "pending" | "processing" | "ready" | "failed";
export type SourceType = "text" | "file";

export interface KnowledgeDocument {
  id: string;
  organizationId: string;
  creatorUserId: string;
  title: string;
  sourceType: SourceType;
  currentVersionId: string | null;
  isActive: boolean;
  processingStatus: ProcessingStatus;
  deletedAt?: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface DocumentVersion {
  id: string;
  documentId: string;
  versionNumber: number;
  contentType: string;
  originalFilename?: string | null;
  storagePath: string;
  contentSize: number;
  checksumSha256?: string | null;
  creatorUserId: string;
  createdAt: string;
}

export interface ProcessingReference {
  id: string;
  organizationId: string;
  documentId: string;
  versionId: string;
  operation: string;
  idempotencyKey: string;
  status: string;
  attempts: number;
  createdAt: string;
  updatedAt: string;
}

// Module 3 processing-job shapes (status surface).
export type JobStatus = "queued" | "processing" | "completed" | "failed" | "cancelled";

export interface ProcessingJob {
  id: string;
  organizationId: string;
  documentId: string;
  versionId: string;
  operation: string;
  idempotencyKey: string;
  status: JobStatus;
  attempts: number;
  maxAttempts: number;
  errorCode?: string | null;
  errorMessage?: string | null;
  createdAt: string;
  updatedAt: string;
}

// Module 6 chat shapes (used by the dashboard test-chat panel).
export type ChatStatus = "answered" | "insufficient_evidence" | "temporarily_unavailable";

export interface ChatSource {
  documentId: string;
  versionId: string;
  chunkId: string;
  rank: number;
  excerpt: string;
  sourceMeta?: Record<string, unknown>;
}

export interface ChatAnswer {
  sessionId: string;
  status: ChatStatus;
  answer: string;
  sources: ChatSource[];
}
