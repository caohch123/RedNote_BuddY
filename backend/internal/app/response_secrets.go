package app

import "xhs-workbench/internal/domain"

func (a *App) publicJob(job domain.Job) domain.Job {
	job.Message = portalJobMessage(a, job, job.Message)
	job.Error = portalJobMessage(a, job, job.Error)
	if job.InputScene != nil {
		scene := *job.InputScene
		scene.Error = portalJobMessage(a, job, scene.Error)
		job.InputScene = &scene
	}
	job.Payload = nil
	job.ProviderSnapshot = nil
	job.ProviderSnapshotHistory = nil
	return job
}

func publicVideoProject(project domain.VideoProject) domain.VideoProject {
	if project.VideoRewrite == nil {
		return project
	}
	state := *project.VideoRewrite
	state.Submissions = append([]domain.VideoSubmission{}, state.Submissions...)
	for i := range state.Submissions {
		state.Submissions[i].ProviderSnapshot = nil
	}
	project.VideoRewrite = &state
	return project
}
