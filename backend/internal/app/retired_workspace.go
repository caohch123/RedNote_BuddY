package app

import "xhs-workbench/internal/domain"

// Historical records stay on disk, but a removed platform must never recover
// provider work or reach the remaining platform's publishing adapter.
func (a *App) retireWorkspaceJob(j *domain.Job) bool {
	platform, found := a.jobWorkspacePlatform(*j, map[string]bool{"jobs:" + j.ID: true})
	if found && validWorkspacePlatform(platform) {
		return false
	}
	if !completedJob(j.State) && j.State != "submission_unknown" {
		j.State = "cancelled"
		j.Message = "所属平台已移除或来源不可用，此任务不会继续执行或重新提交"
		j.UpdatedAt = now()
		if err := a.Store.Put("jobs", j.ID, j); err != nil {
			j.Error = "历史任务停用状态保存失败"
		}
	}
	return true
}
