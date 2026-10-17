export interface DeletionResult { cleanup_pending?: boolean }
export function deletionSuccess(result: DeletionResult | void, message: string) {
  return result?.cleanup_pending ? '内容已删除，少量本机文件将在重启后继续清理' : message;
}
