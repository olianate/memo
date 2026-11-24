export type BlockType =
  | "paragraph"
  | "heading"
  | "bullet"
  | "ordered"
  | "todo"
  | "quote"
  | "code"
  | "divider"
  | "image"
  | "file";

export interface Span {
  text: string;
  bold?: boolean;
  italic?: boolean;
  strike?: boolean;
  code?: boolean;
  link?: string;
}

export interface Block {
  id: string;
  type: BlockType;
  level?: number;
  checked?: boolean;
  language?: string;
  spans?: Span[] | null;
  attachment_id?: string;
  alt?: string;
}

export interface Tag {
  id: string;
  name: string;
  normalized: string;
  count: number;
}

export interface Attachment {
  id: string;
  memo_id: string;
  kind: "image" | "file";
  name: string;
  mime: string;
  bytes: number;
  width?: number;
  height?: number;
  present: boolean;
  has_thumb: boolean;
  notion_file_id?: string;
}

export interface Memo {
  id: string;
  created_at: string;
  updated_at: string;
  revision: number;
  snippet: string;
  deleted: boolean;
  deleted_at?: string;
  keep_local: boolean;
  local_state: "present" | "evicted";
  notion_page_id?: string;
  notion_synced_at?: string;
  last_hydrated_at?: string;
  sync_error?: string;
  blocks: Block[] | null;
  tags: Tag[] | null;
  attachments: Attachment[] | null;
  day: string;
}

export interface Counts {
  notes: number;
  tags: number;
  days: number;
}

export interface Filter {
  deleted?: "active" | "deleted" | "all";
  tag?: string;
  local_only?: boolean;
  synced?: boolean;
  with_attachment?: boolean;
  untagged?: boolean;
  from?: string;
  to?: string;
  limit?: number;
}

export interface SearchResult {
  memo: Memo;
  before: string;
  match: string;
  after: string;
}

export interface Settings {
  theme: "light" | "dark" | "system";
  hotkey: string;
  start_at_login: boolean;
  retention_days: number;
  immediate_eviction: boolean;
  sync_enabled: boolean;
  sync_paused: boolean;
  notion_connected: boolean;
  workspace_name: string;
  workspace_file_limit: number;
  data_dir: string;
  onboarded: boolean;
  hotkey_error?: string;
  mcp_enabled: boolean;
  mcp_port: number;
  mcp_running: boolean;
  mcp_endpoint?: string;
  mcp_error?: string;
  version: string;
}

export interface Workspace {
  name: string;
  id: string;
  file_limit: number;
  bot_type: string;
}

export interface ParentPage {
  id: string;
  title: string;
  url: string;
}

export interface ArchiveDatabase {
  id: string;
  database_id: string;
  title: string;
  url: string;
}

export interface ParentPageList {
  databases: ArchiveDatabase[];
  pages: ParentPage[];
}

export interface ArchiveTarget {
  kind: "database" | "page";
  id: string;
  title: string;
  url: string;
}

export interface ConnectRequest {
  token: string;
  kind: "database" | "page";
  target_id: string;
  database_name: string;
  confirm_schema_changes: boolean;
}

export interface SchemaProperty {
  name: string;
  type: string;
}

export interface SchemaConflict {
  name: string;
  existing_type: string;
  required_type: string;
}

export interface ArchiveDatabasePreview {
  id: string;
  title: string;
  title_property: string;
  missing_properties: SchemaProperty[];
  conflicts: SchemaConflict[];
  has_existing_rows: boolean;
}

export interface ConnectResult {
  workspace: Workspace;
  data_source_id: string;
  database_id: string;
  database_title: string;
  title_property: string;
  created_database: boolean;
}

export interface SyncLog {
  id: number;
  at: string;
  level: string;
  event: string;
  message: string;
}

export interface ErrorReport {
  source: string;
  message: string;
  stack?: string;
  context?: string;
}

export interface SyncStatus {
  state: "idle" | "syncing" | "offline" | "paused" | "error";
  enabled: boolean;
  paused: boolean;
  configured: boolean;
  active: boolean;
  last_success?: string;
  pending: number;
  retrying: number;
  last_error?: string;
  workspace_name?: string;
  data_source_id?: string;
  logs: SyncLog[] | null;
}

export interface BackupResult {
  path: string;
  bytes: number;
  at: string;
}

export interface RestoreProgress {
  total: number;
  completed: number;
  failed: number;
  current_id?: string;
}

export interface UpdateInfo {
  current_version: string;
  latest_version: string;
  available: boolean;
  notes: string;
  published_at: string;
  release_url: string;
  asset_name: string;
  asset_size: number;
  can_install: boolean;
  checked_at: string;
}

export interface UpdateProgress {
  phase: string;
  percent: number;
  message: string;
}

export interface DayCount {
  date: string;
  count: number;
}

export interface Composition {
  type: string;
  count: number;
}

export interface Bucket {
  label: string;
  count: number;
}

export interface CoOccurrence {
  a: string;
  b: string;
  count: number;
}

export interface HourCount {
  hour: number;
  count: number;
}

export interface Dashboard {
  month: string;
  totals: {
    notes: number;
    words: number;
    tags: number;
    attachments: number;
    active_days: number;
  };
  daily: DayCount[];
  year: DayCount[];
  composition: Composition[];
  word_buckets: Bucket[];
  top_tags: Tag[];
  co_occurrence: CoOccurrence[];
  hours: HourCount[];
  todos: { total: number; done: number };
  attachments: { images: number; files: number; bytes: number };
  health: { local_only: number; synced: number; evicted: number; pending: number };
}

export type ViewId =
  | "all"
  | "local"
  | "synced"
  | "attachments"
  | "untagged"
  | "stats"
  | "settings"
  | "trash"
  | "tag";
