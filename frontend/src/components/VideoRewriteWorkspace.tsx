import { useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Card, Checkbox, Collapse, Empty, Input, InputNumber, Modal, Select, Space, Spin, Switch, Tag, Typography, Upload } from 'antd';
import { DownloadOutlined, PlusOutlined, ReloadOutlined, ThunderboltOutlined, UploadOutlined } from '@ant-design/icons';
import { api, assetURL, errorText, upload } from '../api';
import { useResource } from '../hooks';
import { workspacePath } from '../platform';
import type { Asset, AsyncResult, Job, Rewrite, Version, VideoCapabilities, VideoProject, VideoRewriteResult } from '../types';
import { useWorkspace } from '../workspace';
import { formatDate } from '../utils';
import { replaceVideoMaterial, sortVideoRewriteProjects, videoForm, videoInputSignature, videoPurposes, videoSnapshotLabels, videoTiming, type VideoRewriteForm } from '../videoRewrite';
import { Field, Status } from './Common';
import WorkspaceActionDock from './WorkspaceActionDock';
import GenerationSparkle from './GenerationSparkle';
import ParticleGenerateButton from './ParticleGenerateButton';
import { jobWasCleared, useJobsCleared } from '../jobEvents';
import ReferenceSlotCard from './ReferenceSlotCard';

type Props = { rewrite: Rewrite; version?: Version; assets: Asset[]; projects: VideoProject[]; jobs: Job[];
  visible: boolean; mediaStatus: string; onUpdated: () => void; onApplied: (version: Version, sources: string[], assetID: string) => void };
type Slot = 'primary_asset_id' | 'reference_video_id' | 'reference_image_id';
const slots: { key: Slot; title: string; image?: boolean }[] = [
  { key: 'primary_asset_id', title: '被仿写视频' }, { key: 'reference_video_id', title: '参考视频' }, { key: 'reference_image_id', title: '参考图片', image: true },
];
const signature = (value: unknown) => JSON.stringify(value);
const activeStates = ['queued', 'running'];

export default function VideoRewriteWorkspace(props: Props) {
  const { active, platform } = useWorkspace(); const { message } = App.useApp();
  const [local, setLocal] = useState<VideoProject[]>([]); const [selected, setSelected] = useState('');
  const [initializing, setInitializing] = useState(false); const [initialError, setInitialError] = useState('');
  const [retry, setRetry] = useState(0); const [creating, setCreating] = useState(false);
  const attempt = useRef(''); const inFlight = useRef(false); const mounted = useRef(true);
  const saveBeforeLeave = useRef<(() => Promise<VideoProject>) | undefined>(undefined);
  const callbacks = useRef(props); callbacks.current = props;
  const server = props.projects.filter(project => project.workflow === 'video_rewrite_v2');
  const sourceAssets = props.assets.filter(asset => asset.owner_id === props.rewrite.note_id && (asset.kind === 'video' || asset.mime?.startsWith('video/')));
  const choices = sortVideoRewriteProjects([...server.map(project => { const newer = local.find(item => item.id === project.id); return newer && newer.revision >= project.revision ? newer : project; }), ...local.filter(project => !server.some(item => item.id === project.id))], sourceAssets);
  const project = choices.find(item => item.id === selected) || choices[0];
  const sourceKey = sourceAssets.map(asset => `${asset.id}:${asset.status}`).join(',');
  function saved(project: VideoProject) { setLocal(previous => [...previous.filter(item => item.id !== project.id), project]); }
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => { if (choices.length && !choices.some(item => item.id === selected)) setSelected(choices[0].id); }, [choices, selected]);
  async function create() {
    setCreating(true);
    try { await saveBeforeLeave.current?.(); const next = await api<VideoProject>(workspacePath('/video-projects', platform), 'POST', { workflow: 'video_rewrite_v2', rewrite_id: props.rewrite.id, version_id: props.version?.id || '' }); saved(next); setSelected(next.id); props.onUpdated(); }
    catch (e) { message.error(errorText(e)); } finally { setCreating(false); }
  }
  useEffect(() => {
    if (!active || !props.visible || inFlight.current) return;
    const key = `${props.rewrite.id}:${sourceKey}:${props.mediaStatus}:${retry}`; if (attempt.current === key) return;
    attempt.current = key; inFlight.current = true; setInitializing(true); setInitialError('');
    void api<VideoProject[]>(workspacePath(`/rewrites/${props.rewrite.id}/videos/initialize`, platform), 'POST').then(next => {
      if (!Array.isArray(next)) throw new Error('视频方案初始化返回内容无效，请重试');
      if (!mounted.current) return; setLocal(previous => [...previous.filter(item => !next.some(saved => saved.id === item.id)), ...next]); callbacks.current.onUpdated();
    }).catch(e => { if (mounted.current) setInitialError(errorText(e)); }).finally(() => { inFlight.current = false; if (mounted.current) setInitializing(false); });
  }, [active, props.visible, props.rewrite.id, sourceKey, props.mediaStatus, retry, platform]);
  async function choose(id: string) { try { await saveBeforeLeave.current?.(); setSelected(id); } catch (e) { message.error(errorText(e)); } }
  const waiting = ['pending', 'queued', 'downloading', 'collecting'].includes(props.mediaStatus);
  return <div className="video-rewrite-workspace">
    <div className="video-rewrite-toolbar"><Select aria-label="视频方案" value={project?.id} placeholder="选择视频方案" options={choices.map((item, index) => ({ value: item.id, label: `方案 ${index + 1}` }))} onChange={id => void choose(id)} />
      <Button size="small" icon={<PlusOutlined />} loading={creating} disabled={initializing} onClick={() => void create()}>新方案</Button></div>
    {initialError && <Alert showIcon type="error" title="视频方案初始化失败" description={initialError} action={<Button size="small" onClick={() => setRetry(value => value + 1)}>重试</Button>} />}
    {!project && <div className="video-rewrite-start"><Spin spinning={initializing} />{waiting ? <Typography.Text type="secondary">笔记视频正在下载，完成后会自动带入。</Typography.Text> : <Typography.Text type="secondary">可从视频、图片或文字要求开始创作。</Typography.Text>}
      <Button type="primary" disabled={initializing} onClick={() => void create()}>开始视频仿写</Button></div>}
    {project && <VideoEditor key={project.id} {...props} project={project} onSaved={saved} saveBeforeLeave={saveBeforeLeave} />}
  </div>;
}

function VideoEditor({ project, rewrite, version, assets, jobs, visible, onSaved, onUpdated, onApplied, saveBeforeLeave }: Props & {
  project: VideoProject; onSaved: (project: VideoProject) => void; saveBeforeLeave: { current: (() => Promise<VideoProject>) | undefined };
}) {
  const { active, platform } = useWorkspace(); const { message, modal } = App.useApp();
  const [current, setCurrent] = useState(project); const currentRef = useRef(project);
  const [form, setForm] = useState(() => videoForm(project.video_rewrite)); const formRef = useRef(form);
  const baseline = useRef(signature(form)); const chain = useRef<Promise<unknown>>(Promise.resolve()); const saving = useRef(false);
  const [saveStatus, setSaveStatus] = useState(''); const [saveError, setSaveError] = useState(''); const [busy, setBusy] = useState('');
  const saveErrorRef = useRef('');
  const [step, setStep] = useState(project.video_rewrite?.results?.length ? 2 : project.video_rewrite?.script_input_digest ? 1 : 0);
  const [picker, setPicker] = useState<Slot>(); const [search, setSearch] = useState(''); const [picked, setPicked] = useState('');
  const [uploaded, setUploaded] = useState<Asset[]>([]); const [uploading, setUploading] = useState(false);
  const [taskID, setTaskID] = useState(''); const [remoteID, setRemoteID] = useState(''); const seenTask = useRef('');
  const intent = useRef<{ revision: number; id: string } | undefined>(undefined);
  const path = `/video-projects/${project.id}`;
  const polled = useResource<VideoProject>(active && visible ? workspacePath(path, platform) : null, 2000);
  const caps = useResource<VideoCapabilities>(active && visible ? workspacePath('/video-capabilities', platform) : null);
  const task = jobs.find(job => job.id === taskID);
  const relevantJobs = jobs.filter(job => job.entity_id === project.id && ['video_script', 'video_rewrite'].includes(job.kind));
  const running = relevantJobs.some(job => activeStates.includes(job.state)) || !!taskID && (!task || activeStates.includes(task.state));
  const state = current.video_rewrite; const lastSubmission = state?.submissions?.at(-1);
  const paused = !!lastSubmission && ['paused', 'submission_unknown', 'failed', 'waiting_config'].includes(lastSubmission.state);
  const locked = running || paused || !!busy;
  const lockedRef = useRef(locked); lockedRef.current = locked;
  useJobsCleared(platform, ids => {
    if (ids.includes(taskID)) { setTaskID(''); intent.current = undefined; }
    void polled.reload(true); onUpdated();
  });
  const dirty = signature(form) !== baseline.current;
  const latestJob = [...relevantJobs].sort((a, b) => b.created_at.localeCompare(a.created_at))[0];
  const latestFailure = latestJob && ['failed', 'waiting_config', 'blocked'].includes(latestJob.state) ? latestJob : undefined;
  function install(next: VideoProject) {
    currentRef.current = next; setCurrent(next); const savedForm = videoForm(next.video_rewrite); baseline.current = signature(savedForm); formRef.current = savedForm; setForm(savedForm); onSaved(next);
  }
  useEffect(() => {
    const next = polled.data && polled.data.revision >= project.revision ? polled.data : project;
    if (saving.current || signature(formRef.current) !== baseline.current || next.revision < currentRef.current.revision) return;
    install(next);
  }, [project, polled.data]);
  function change(patch: (form: VideoRewriteForm) => VideoRewriteForm) { if (lockedRef.current) return; const next = patch(formRef.current); formRef.current = next; setForm(next); saveErrorRef.current = ''; setSaveError(''); }
  async function flush(retry = false): Promise<VideoProject> {
    const work = chain.current.then(async () => {
      if (saveErrorRef.current && !retry) throw new Error(saveErrorRef.current);
      const draft = formRef.current; if (signature(draft) === baseline.current) return currentRef.current;
      saving.current = true; setSaveStatus('正在保存'); saveErrorRef.current = ''; setSaveError('');
      try {
        const next = await api<VideoProject>(workspacePath(path, platform), 'PATCH', { revision: currentRef.current.revision, video_rewrite: draft });
        currentRef.current = next; setCurrent(next); baseline.current = signature(videoForm(next.video_rewrite));
        if (signature(formRef.current) === signature(draft)) { const saved = videoForm(next.video_rewrite); formRef.current = saved; setForm(saved); }
        onSaved(next); onUpdated(); setSaveStatus(''); return next;
      } catch (e) { setSaveStatus(''); saveErrorRef.current = errorText(e); setSaveError(saveErrorRef.current); throw e; } finally { saving.current = false; }
    }); chain.current = work.catch(() => undefined); return work;
  }
  useEffect(() => { saveBeforeLeave.current = flush; return () => { saveBeforeLeave.current = undefined; }; });
  useEffect(() => { if (!dirty || locked || !active || saveError) return; const timer = setTimeout(() => { void flush().catch(() => undefined); }, 600); return () => clearTimeout(timer); }, [form, dirty, locked, active, saveError]);
  useEffect(() => {
    if (!task || activeStates.includes(task.state) || seenTask.current === task.id) return;
    seenTask.current = task.id; setTaskID(''); void polled.reload(true); onUpdated();
    if (task.state === 'succeeded' && task.kind === 'video_script') setStep(1);
  }, [task]);
  const inputIDs = [...new Set(Object.values(form.purpose_sources).filter((id): id is string => !!id))];
  const inputReady = !!inputIDs.length || !!form.requirements.trim();
  const usesMaterial = !!inputIDs.length;
  const configuredCompose = !!caps.data?.text_configured && (!usesMaterial || !!caps.data?.vision_configured);
  const scriptCurrent = state?.script_rules_version === caps.data?.rules_version && !!state?.script_input_digest && videoInputSignature(form) === videoInputSignature(videoForm(state));
  const allAssets = [...assets, ...uploaded.filter(asset => !assets.some(saved => saved.id === asset.id))];
  const find = (id: string) => allAssets.find(asset => asset.id === id);
  const timing = videoTiming(form, allAssets);
  const waitingMaterial = inputIDs.some(id => find(id)?.status !== 'ready');
  async function run(kind: 'compose-script' | 'generate') {
    if (busy || running) return; setBusy(kind);
    try {
      const saved = await flush(); let requestID: string | undefined;
      if (kind === 'generate') { if (!intent.current || intent.current.revision !== saved.revision) intent.current = { revision: saved.revision, id: crypto.randomUUID() }; requestID = intent.current.id; }
      const result = await api<AsyncResult>(workspacePath(`${path}/${kind}`, platform), 'POST', { revision: saved.revision, ...(requestID ? { request_id: requestID, prompt: saved.video_rewrite?.prompt } : {}) });
      const cleared = !!result.job_id && jobWasCleared(platform, result.job_id);
      setTaskID(cleared ? '' : result.job_id || ''); if (kind === 'generate') { intent.current = undefined; if (!cleared) setStep(2); }
      onUpdated(); void polled.reload(true);
    } catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  async function resume(action: 'restore' | 'retry' | 'cancel', confirmNoResult = false) {
    if (!lastSubmission) return; setBusy(action);
    try { const result = await api<AsyncResult>(workspacePath(`${path}/resume`, platform), 'POST', { submission_id: lastSubmission.id, action, remote_task_id: remoteID.trim() || undefined, confirm_no_result: confirmNoResult }); setTaskID(result.job_id && !jobWasCleared(platform, result.job_id) ? result.job_id : ''); onUpdated(); void polled.reload(true); }
    catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  function retryUnknown() { modal.confirm({ title: '已核实没有生成结果？', content: '请先核对供应商记录和费用。已有结果时填写任务 ID 恢复查询，避免再次生成。', okText: '已核实，重新提交', cancelText: '返回核实', onOk: () => resume('retry', true) }); }
  async function apply(result: VideoRewriteResult) {
    if (!version) return; setBusy(result.asset_id);
    try {
      const saved = await flush(); const primary = saved.video_rewrite?.primary_asset_id || '';
      if ((result.snapshot.primary_asset_id || '') !== primary) throw new Error('被仿写视频已更改，请在对应方案中采用此结果');
      const next = await api<Version>(workspacePath(`${path}/apply`, platform), 'POST', { asset_id: result.asset_id, version_id: version.id });
      const sources = primary ? [primary, ...(saved.video_rewrite?.results || []).filter(item => item.snapshot.primary_asset_id === primary).map(item => item.asset_id)] : [];
      onApplied(next, sources, result.asset_id); onUpdated(); message.success(primary ? '已替换对应视频，其他素材与文案已保留' : '已加入发布素材');
    } catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  async function addUpload(file: File, slot = picker, apply = false) {
    if (locked || !slot) return false;
    if (file.size > (slot === 'reference_image_id' ? 20 : 100) * 1024 * 1024) { message.warning(slot === 'reference_image_id' ? '图片不得超过 20 MB' : '参考视频不得超过 100 MB'); return false; }
    setUploading(true);
    try { const asset = await upload(file, rewrite.id, workspacePath('/assets', platform)) as Asset; setUploaded(previous => [...previous, asset]); if (apply) change(value => replaceVideoMaterial(value, slot, asset.id)); else setPicked(asset.id); onUpdated(); message.success('已上传到项目素材库'); }
    catch (e) { message.error(errorText(e)); } finally { setUploading(false); } return false;
  }
  function openPicker(slot: Slot) { setPicker(slot); setSearch(''); setPicked(form[slot]); }
  function navigate(next: number) { setStep(next); if (!locked) void flush().catch(e => message.error(errorText(e))); }
  const selectedIDs = slots.map(slot => form[slot.key]).filter(Boolean);
  const pickerItems = allAssets.filter(asset => asset.status === 'ready' && !asset.library_deleted_at && asset.kind !== 'style_reference' && (picker === 'reference_image_id' ? asset.mime?.startsWith('image/') : asset.mime?.startsWith('video/'))
    && (!selectedIDs.includes(asset.id) || asset.id === (picker && form[picker])) && `${asset.name || ''} ${asset.position + 1} ${asset.extraction?.text || ''}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()));
  const results = [...(state?.results || [])].reverse();
  const resultCard = (result: VideoRewriteResult) => <Card className="video-rewrite-result" size="small" key={result.asset_id}>
    <video controls preload="metadata" src={assetURL(result.preview_asset_id || result.asset_id, platform)} />
    <Space wrap><Tag>{result.snapshot.output.duration} 秒 · {result.snapshot.output.resolution} · {result.snapshot.output.ratio}</Tag><Tag>已保存到素材库</Tag></Space>
    {result.warning && <Alert type="warning" showIcon title={result.warning} />}
    <Typography.Text type="secondary">{formatDate(result.created_at)}</Typography.Text>
    <Space wrap><Button type="primary" disabled={!version || locked} loading={busy === result.asset_id} onClick={() => void apply(result)}>采用此视频</Button><Button icon={<DownloadOutlined />} href={assetURL(result.asset_id, platform)} download>下载原视频</Button></Space>
    <Collapse items={[{ key: 'basis', label: '本次生成依据', children: <div className="video-generation-basis"><Typography.Text>{result.snapshot.model}</Typography.Text><div>{videoPurposes.filter(purpose => result.snapshot.purpose_sources[purpose.key]).map(purpose => `${purpose.label}：${videoSnapshotLabels(result.snapshot)[result.snapshot.purpose_sources[purpose.key]!] || '原始素材'}`).join(' · ')}</div><div className="video-basis-previews">{result.snapshot.input_asset_ids.map(id => <div key={id}>{Object.hasOwn(result.snapshot.video_urls || {}, id) ? <video controls preload="metadata" src={assetURL(id, platform)} /> : <img alt={videoSnapshotLabels(result.snapshot)[id]} src={assetURL(id, platform)} />}<span>{videoSnapshotLabels(result.snapshot)[id]}</span></div>)}</div>{result.snapshot.requirements && <p>补充要求：{result.snapshot.requirements}</p>}<Typography.Paragraph className="preserve">{result.snapshot.prompt}</Typography.Paragraph></div> }]} />
  </Card>;
  return <>
    <div className="video-rewrite-steps">{['素材与要求', '审阅提示词', '审阅结果'].map((label, index) => <button type="button" key={label} aria-current={step === index ? 'step' : undefined} onClick={() => navigate(index)}>{index + 1}. {label}</button>)}</div>
    {timing.error && <Alert type="warning" showIcon title={timing.error} />}
    {saveError && <Alert type="error" showIcon title="草稿保存失败，编辑仍已保留" description={saveError} action={<Button size="small" onClick={() => void flush(true).catch(() => undefined)}>重试保存</Button>} />}
    {polled.error && <Alert type="warning" showIcon title={polled.error} action={<Button size="small" icon={<ReloadOutlined />} onClick={() => void polled.reload()}>刷新</Button>} />}
    {caps.error && <Alert type="warning" showIcon title={caps.error} action={<Button size="small" onClick={() => void caps.reload()}>重试</Button>} />}
    {!!latestFailure && !running && <Alert type="warning" showIcon title={latestFailure.error || latestFailure.message || '任务未完成，输入已保留'} />}
    {running && <Alert type="info" showIcon title={task?.message || '任务处理中，关闭页面后可继续查询'} />}
    {paused && <Alert type="warning" showIcon title={lastSubmission.state === 'submission_unknown' ? '提交结果待核实' : '生成已暂停'} description={<div className="video-resume-actions"><p>核对供应商任务后恢复查询；工作台不会自动重新生成。</p>{!lastSubmission.remote_task_id && <Input aria-label="供应商视频任务 ID" value={remoteID} onChange={event => setRemoteID(event.target.value)} placeholder="已生成的供应商任务 ID" />}<Space wrap><Button disabled={!!busy || !lastSubmission.remote_task_id && !lastSubmission.result_url && !remoteID.trim()} onClick={() => void resume('restore')}>恢复查询／下载</Button><Button disabled={!!busy} onClick={() => lastSubmission.state === 'submission_unknown' ? retryUnknown() : void resume('retry')}>重新提交</Button><Button disabled={!!busy} onClick={() => void resume('cancel')}>结束本次任务</Button></Space></div>} />}
    {step === 0 && <>
      <div className="video-material-grid">{slots.map(slot => { const id = form[slot.key]; const asset = find(id); return <div key={slot.key} className="video-material-slot">
        <ReferenceSlotCard title={slot.title} kind={slot.image ? 'image' : 'video'} active={active && visible} disabled={locked || uploading} primary={slot.key === 'primary_asset_id'}
          preview={id ? slot.image ? <img alt={slot.title} src={assetURL(id, platform)} /> : asset?.status === 'ready' ? <video aria-label={`${slot.title}预览`} controls preload="metadata" src={assetURL(id, platform)} /> : <Typography.Text type="secondary">视频下载完成后可预览</Typography.Text> : undefined}
          meta={asset?.duration ? `${asset.duration.toFixed(1)} 秒` : undefined} onChoose={() => openPicker(slot.key)} onRemove={() => change(value => replaceVideoMaterial(value, slot.key, ''))} onUpload={file => addUpload(file, slot.key, true)} />
        {!!id && <div className="video-purpose-list">{videoPurposes.filter(purpose => !slot.image || ['subject', 'style'].includes(purpose.key)).map(purpose => <Checkbox key={purpose.key} disabled={locked} checked={form.purpose_sources[purpose.key] === id} onChange={event => change(value => { const purpose_sources = { ...value.purpose_sources }; if (event.target.checked) purpose_sources[purpose.key] = id; else delete purpose_sources[purpose.key]; return { ...value, purpose_sources }; })}>{purpose.label}</Checkbox>)}</div>}
      </div>; })}</div>
      <Typography.Paragraph className="video-rewrite-note" type="secondary">百炼万相 3.0：参考视频单条 1–15 秒、最多 100 MB，合计不超过 15 秒；输入与生成视频合计不超过 30 秒。参考图片最多 20 MB，PNG 需不透明。</Typography.Paragraph>
      {waitingMaterial && <Alert type="info" showIcon title="所选视频仍在下载，完成后可生成提示词" />}
      <Field label="补充要求（可留空）"><Input.TextArea aria-label="视频补充要求" value={form.requirements} rows={3} maxLength={1000} showCount readOnly={locked} placeholder="例如：保留主角与动作，改为夜景；声音轻松自然。" onChange={event => change(value => ({ ...value, requirements: event.target.value }))} /></Field>
      {(!caps.data?.text_configured || usesMaterial && !caps.data?.vision_configured) && caps.data && <Alert type="warning" showIcon title={!caps.data.text_configured ? '提示词编排待配置：请先设置文本模型' : '素材分析待配置：请先设置视觉模型'} />}
    </>}
    {step === 1 && <div className="video-script-review">
      {!!state?.script_input_digest && !scriptCurrent && <Alert type="warning" showIcon title="素材、要求或输出设置已修改，请重新生成提示词后确认" />}
      <Field label="最终提交提示词" hint="可直接修改，编辑会自动保存；确认后按此内容生成视频。"><Input.TextArea aria-label="最终提交提示词" autoSize={{ minRows: 12, maxRows: 28 }} maxLength={20000} showCount value={form.prompt} readOnly={locked} onChange={event => change(value => ({ ...value, prompt: event.target.value }))} /></Field>
      {caps.data && (!caps.data.video_configured || !caps.data.supported) && <Alert type="warning" showIcon title={!caps.data.video_configured ? '视频生成待配置：请先设置视频模型' : '当前视频模型不支持此工作流，请配置支持参考生成的模型'} />}
    </div>}
    {step === 2 && <div className="video-rewrite-results">{lastSubmission && <Space wrap><Status value={lastSubmission.state} /><Typography.Text type="secondary">仅点击采用后才加入发布素材</Typography.Text></Space>}{results[0] ? resultCard(results[0]) : !running && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无生成结果，可返回审阅提示词" />}{results.length > 1 && <Collapse items={[{ key: 'history', label: `历史结果（${results.length - 1}）`, children: <div className="video-rewrite-results">{results.slice(1).map(resultCard)}</div> }]} />}</div>}
    <Modal title={slots.find(slot => slot.key === picker)?.title || '选择素材'} open={active && visible && !!picker} rootClassName={!active || !visible ? 'inactive-workspace-portal' : undefined} width={820} okText="确认选择" cancelText="取消" onCancel={() => setPicker(undefined)} okButtonProps={{ disabled: locked || !picked || uploading }} onOk={() => { if (picker && picked) change(value => replaceVideoMaterial(value, picker, picked)); setPicker(undefined); }}>
      <div className="video-picker-toolbar"><Input.Search aria-label="搜索视频仿写素材" placeholder="搜索素材名称或文字" allowClear value={search} onChange={event => setSearch(event.target.value)} /><Upload accept={picker === 'reference_image_id' ? 'image/*' : 'video/mp4,video/quicktime'} beforeUpload={file => addUpload(file)} showUploadList={false}><Button icon={<UploadOutlined />} loading={uploading}>上传{picker === 'reference_image_id' ? '图片' : '视频'}</Button></Upload></div>
      <div className="video-picker-grid">{pickerItems.map(asset => <Card size="small" key={asset.id} className={picked === asset.id ? 'is-selected' : ''}>{picker === 'reference_image_id' ? <img alt={asset.name || `图片 ${asset.position + 1}`} src={assetURL(asset.id, platform)} /> : <video controls preload="metadata" src={assetURL(asset.id, platform)} />}<Checkbox checked={picked === asset.id} onChange={event => setPicked(event.target.checked ? asset.id : '')}>{asset.name || `${picker === 'reference_image_id' ? '图片' : '视频'} ${asset.position + 1}`}</Checkbox><div className="subtle">{asset.mime?.startsWith('video/') ? `${asset.duration.toFixed(1)} 秒` : `${asset.width} × ${asset.height}`}</div></Card>)}</div>
      {!pickerItems.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无符合条件的项目素材，可上传文件" />}
    </Modal>
    {visible && <WorkspaceActionDock className="video-rewrite-actions"><div className="video-output-options"><InputNumber aria-label="生成视频时长" precision={0} min={caps.data?.min_duration || 2} max={timing.maxDuration} value={form.output.duration} suffix="秒" disabled={locked} onChange={value => change(current => ({ ...current, output: { ...current.output, duration: value ?? 10 } }))} /><Select aria-label="视频分辨率" value={form.output.resolution} disabled={locked} options={(caps.data?.resolutions || ['480p', '720p', '1080p']).map(value => ({ value, label: value }))} onChange={resolution => change(current => ({ ...current, output: { ...current.output, resolution } }))} /><Select aria-label="视频长宽比" value={form.output.ratio} disabled={locked} options={(caps.data?.ratios || ['16:9', '9:16', '1:1', '4:3', '3:4', '21:9']).map(value => ({ value, label: value }))} onChange={ratio => change(current => ({ ...current, output: { ...current.output, ratio } }))} /><label className="video-audio-setting"><Switch aria-label="生成模型原生声音" checked={form.output.generate_audio} disabled={locked} onChange={generate_audio => change(current => ({ ...current, output: { ...current.output, generate_audio } }))} /> 有声</label><Typography.Text type="secondary">1 条</Typography.Text></div>
      <Space wrap>{saveStatus && <Typography.Text type="secondary">{saveStatus}</Typography.Text>}{step === 0 ? <Button type="primary" className="ai-generation-button" icon={<GenerationSparkle />} loading={busy === 'compose-script' || running} disabled={locked || !inputReady || waitingMaterial || !configuredCompose || !!saveError || !!timing.error} onClick={() => void run('compose-script')}>生成提示词</Button> : step === 1 ? <><Button onClick={() => navigate(0)}>返回修改要求</Button>{!scriptCurrent ? <ParticleGenerateButton label="重新生成提示词" ariaLabel="重新生成提示词" accelerating={busy === 'compose-script' || running} disabled={locked || !inputReady || waitingMaterial || !configuredCompose || !!saveError || !!timing.error} onGenerate={() => run('compose-script')} /> : <Button type="primary" icon={<ThunderboltOutlined />} loading={busy === 'generate'} disabled={locked || !form.prompt.trim() || waitingMaterial || !caps.data?.video_configured || !caps.data.supported || !!saveError || !!timing.error} onClick={() => void run('generate')}>确认并生成视频</Button>}</> : <Button onClick={() => navigate(1)}>返回修改提示词</Button>}</Space>
    </WorkspaceActionDock>}
  </>;
}
