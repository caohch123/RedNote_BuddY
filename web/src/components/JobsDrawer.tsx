import { useRef, useState } from 'react';
import { App, Button, Drawer, Popconfirm, Progress, Space, Typography } from 'antd';
import { DeleteOutlined, ReloadOutlined } from '@ant-design/icons';
import { api, errorText } from '../api';
import type { Job } from '../types';
import { formatDate } from '../utils';
import { Failure, Nothing, Status } from './Common';
import { useWorkspace } from '../workspace';
import { platformNames, workspacePath } from '../platform';
import { notifyJobsCleared } from '../jobEvents';

const kinds: Record<string, string> = { collect: '笔记采集', recollect: '重新采集', extract: '文字提取', ocr: '图片 OCR',
  asr: '视频转写', rewrite: 'AI 仿写', image_analyze: '图片特征分析', image_compose: '图片提示词编排', image_rewrite: '参考图仿写', video_script: '视频素材分析与脚本编排', video_rewrite: '视频仿写生成', publish: '作品发布', publish_prepare: '发布准备' };
const retiredKinds = ['image_voice', 'image_plan', 'storyboard', 'video', 'video_generate', 'render'];
const imageSubmissionUnknown = (job: Job) => job.kind === 'image_rewrite' && (job.state === 'submission_unknown' || job.phase === 'submission_unknown' || !!job.error?.includes('提交结果'));

export default function JobsDrawer({ open, onClose, jobs, error, refresh }: {
  open: boolean; onClose: () => void; jobs: Job[]; error: string; refresh: () => void;
}) {
  const { message } = App.useApp();
  const { platform, active } = useWorkspace();
  const [retrying, setRetrying] = useState('');
  const [clearing, setClearing] = useState(false);
  const clearInFlight = useRef(false);
  async function clearAll() {
    if (clearInFlight.current) return;
    clearInFlight.current = true;
    setClearing(true);
    try {
      const result = await api<{ deleted: number; ids: string[]; stopping: number; remote_unstopped: number }>(workspacePath('/jobs', platform), 'DELETE');
      notifyJobsCleared(platform, result.ids || []);
      message.success(result.deleted ? `已清空 ${result.deleted} 条任务${result.stopping ? '，正在中断剩余处理' : ''}` : '没有可清理的任务');
      if (result.remote_unstopped) message.info('本地处理已中断，已提交的云端生成请到对应服务控制台核对');
      refresh();
    } catch (e) { message.error(errorText(e)); } finally { clearInFlight.current = false; setClearing(false); }
  }
  async function retry(job: Job) {
    if (imageSubmissionUnknown(job) || retiredKinds.includes(job.kind)) return;
    setRetrying(job.id);
    try { await api(workspacePath(`/jobs/${job.id}/retry`, platform), 'POST', {});
      message.success('任务已加入队列'); refresh();
    } catch (e) { message.error(errorText(e)); } finally { setRetrying(''); }
  }
  function onRetry(job: Job) {
    if (imageSubmissionUnknown(job) || retiredKinds.includes(job.kind)) return;
    void retry(job);
  }
  return <>
    <Drawer rootClassName={active ? undefined : 'inactive-workspace-portal'} title={`${platformNames[platform]}后台任务`} open={active && open} onClose={onClose} size={520}
      extra={<Space><Popconfirm title="清空全部任务？" description="清除当前工作区的任务记录，并中断排队及正在进行的处理。" okText="清空" cancelText="取消" onConfirm={() => void clearAll()}>
        <Button aria-label="清空全部任务" icon={<DeleteOutlined />} loading={clearing} disabled={!jobs.length}>清空</Button>
      </Popconfirm><Button icon={<ReloadOutlined />} onClick={refresh}>刷新</Button></Space>}>
      <Typography.Paragraph type="secondary">任务进度每 2 秒更新。关闭页面后，服务会继续处理已提交的任务。</Typography.Paragraph>
      <Failure error={error} retry={refresh} />
      {!jobs.length && <Nothing description="暂无后台任务" />}
      <div className="job-list">{jobs.map(job => <div className="job-card" key={job.id}>
        <div className="row-between"><strong>{retiredKinds.includes(job.kind) ? '已停用的旧视频任务' : kinds[job.kind] || job.kind}</strong><Status value={job.state} /></div>
        <Typography.Text type="secondary">{job.phase || '等待处理'} · {formatDate(job.created_at)}</Typography.Text>
        <Progress percent={job.progress || 0} status={job.state === 'failed' ? 'exception' : job.state === 'succeeded' ? 'success' : 'active'} size="small" />
        {job.message && <p>{job.message}</p>}
        {job.error && !imageSubmissionUnknown(job) && <Typography.Paragraph type="danger" className="preserve">{job.error}</Typography.Paragraph>}
        {imageSubmissionUnknown(job) && <Typography.Paragraph type="secondary">图片生成提交结果待核实，请先到所配置的图片供应商控制台核对生成记录和费用。此任务不会自动重复提交；核实后可将已生成的图片保存到本机，再上传到当前工作区使用。</Typography.Paragraph>}
        {['publish', 'publish_prepare'].includes(job.kind) && ['failed', 'blocked', 'waiting_config', 'paused', 'submission_unknown', 'verification_required'].includes(job.state) && <Typography.Paragraph type="secondary">
          {job.state === 'submission_unknown' ? '请先在目标平台创作中心核对结果，再到发布管理记录；此任务不会自动重复提交。' : '请到发布管理检查账号和素材，重新核对并确认发布。'}
        </Typography.Paragraph>}
        {job.kind === 'video_rewrite' && ['failed', 'blocked', 'waiting_config', 'paused', 'submission_unknown'].includes(job.state) && <Typography.Paragraph type="secondary">请返回视频仿写工作台核对本次任务并恢复查询或下载。重新生成需要再次确认。</Typography.Paragraph>}
        {job.kind === 'video_script' && ['failed', 'waiting_config'].includes(job.state) && <Typography.Paragraph type="secondary">请返回视频仿写工作台，检查素材与配置后重新生成脚本。</Typography.Paragraph>}
        {!['publish', 'publish_prepare', 'video_rewrite', 'video_script', ...retiredKinds].includes(job.kind) && !imageSubmissionUnknown(job) && ['failed', 'blocked', 'waiting_config', 'paused', 'submission_unknown'].includes(job.state) && <Space>
          <Button onClick={() => onRetry(job)} loading={retrying === job.id} size="small">重试此任务</Button>
        </Space>}
        <span className="task-id">任务 {job.id}</span>
      </div>)}</div>
    </Drawer>

  </>;
}
