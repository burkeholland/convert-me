export type Kind = 'image' | 'audio' | 'video';

export type ItemStatus =
  | 'checking' | 'ready' | 'unsupported' | 'queued' | 'converting'
  | 'done' | 'failed' | 'cancelled' | 'skipped';

export interface Item {
  id: string;
  path: string;
  name: string;
  folder: string;
  size: number;
  kind: Kind | '';
  status: ItemStatus;
  source: string;
  width: number;
  height: number;
  durationMs: number;
  target: string;
  targetLabel: string;
  progress: number;
  outputPath: string;
  outputName: string;
  outputSize: number;
  error: string;
  detail: string;
  warning: string;
  thumb: boolean;
}

export interface FormatOption {
  id: string;
  label: string;
  group: string;
  available: boolean;
  reason: string;
}

export interface KindState {
  kind: Kind;
  label: string;
  count: number;
  target: string;
  options: FormatOption[];
}

export interface FormatRow {
  kind: Kind;
  label: string;
  reads: string[];
  writes: string[];
}

export interface Destination {
  mode: 'source' | 'folder';
  folder: string;
}

export interface Batch {
  state: 'idle' | 'running' | 'cancelling' | 'finished';
  total: number;
  done: number;
  failed: number;
  skipped: number;
  cancelled: number;
  currentId: string;
}

export interface Snapshot {
  version: string;
  engine: string;
  state: 'starting' | 'ready' | 'failed';
  setupError: string;
  items: Item[];
  kinds: KindState[];
  destination: Destination;
  batch: Batch;
  formats: FormatRow[];
  revision: number;
  notice: string;
  noticeError: boolean;
  noticeSeq: number;
}

export interface AddResult {
  added: number;
  skipped: { name: string; reason: string }[];
  message: string;
}

export interface Conflict {
  itemId: string;
  name: string;
  folder: string;
}

export interface StartResult {
  started: boolean;
  conflicts: Conflict[];
}

export interface AppBridge {
  Status(): Promise<Snapshot>;
  AddFiles(paths: string[]): Promise<AddResult>;
  ChooseFiles(): Promise<AddResult>;
  RemoveItem(id: string): Promise<void>;
  ClearItems(): Promise<void>;
  SetTarget(kind: string, target: string): Promise<void>;
  ChooseDestination(): Promise<boolean>;
  UseSourceFolder(): Promise<void>;
  UseChosenFolder(): Promise<void>;
  Start(policy: string): Promise<StartResult>;
  Cancel(): Promise<void>;
  OpenOutputFolder(): Promise<void>;
  RevealItem(id: string): Promise<void>;
  OpenLicenses(): Promise<void>;
  MinimiseWindow?(): Promise<void>;
  ToggleMaximiseWindow?(): Promise<void>;
  CloseWindow?(): Promise<void>;
}

declare global {
  interface Window {
    go?: { main?: { App?: AppBridge } };
    runtime?: {
      OnFileDrop?: (callback: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean) => void;
      OnFileDropOff?: () => void;
    };
  }
}
