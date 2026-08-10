import type { ComputedRef, InjectionKey } from "vue";

export interface WorkspaceContext {
  cwd: ComputedRef<string>;
  openFile: (path: string) => void;
}

export const WorkspaceContextKey: InjectionKey<WorkspaceContext> = Symbol("shelley-workspace");
