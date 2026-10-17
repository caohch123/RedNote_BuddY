package app

import "xhs-workbench/internal/domain"

// These job names are recognized only to retire persisted work safely. No
// renderer, speech generation or per-scene provider execution remains.
func retiredVideoJob(kind string) bool {
	switch kind {
	case "image_voice", "image_plan", "storyboard", "video", "video_generate", "render":
		return true
	}
	return false
}

func (a *App) retireVideoJob(j *domain.Job) bool {
	if !retiredVideoJob(j.Kind) {
		return false
	}
	if !completedJob(j.State) {
		// Keep uncertain paid submissions visible for the user's billing audit.
		if j.State != "submission_unknown" {
			j.State = "cancelled"
		}
		j.Message = "旧视频制作流程已移除，此任务不会继续执行或重新提交"
		j.UpdatedAt = now()
		if err := a.Store.Put("jobs", j.ID, *j); err != nil {
			j.Error = "旧任务停用状态保存失败"
		}
	}
	return true
}
