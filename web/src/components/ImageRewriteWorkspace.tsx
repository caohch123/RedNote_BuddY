import { useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Card, Collapse, Image, Input, Radio, Select, Space, Tag, Typography, Upload } from 'antd';
import { ExperimentOutlined, PictureOutlined, PlusOutlined, UploadOutlined } from '@ant-design/icons';
import { api, assetURL, errorText, upload } from '../api';
import type { Asset, AsyncResult, ImageCapabilities, ImageEditIntent, ImageFeatureControl, ImageFeatureControls, ImageFeatures, ImageReference, ImageResult, ImageRewrite, Job, Rewrite, StyleCatalogue, Version } from '../types';
import { useWorkspace } from '../workspace';
import { workspacePath } from '../platform';
import { Field } from './Common';
import { formatDate } from '../utils';
import PicturePicker, { type PictureChoice } from './PicturePicker';
import { useResource } from '../hooks';
import WorkspaceActionDock from './WorkspaceActionDock';
import ParticleGenerateButton from './ParticleGenerateButton';
import GenerationSparkle from './GenerationSparkle';
import { jobWasCleared, useJobsCleared } from '../jobEvents';
import ReferenceSlotCard from './ReferenceSlotCard';

const dimensions: { key: keyof ImageFeatures; label: string }[] = [
  { key: 'subject', label: '主体与对象' }, { key: 'action', label: '动作与关系' }, { key: 'scene', label: '场景与背景' }, { key: 'composition', label: '布局与视角' },
  { key: 'palette', label: '全局配色' }, { key: 'lighting', label: '光影' }, { key: 'texture', label: '画法与质感' }, { key: 'text', label: '文字版式' },
];
function intentSummary(intent?: ImageEditIntent) {
  if (!intent) return '';
  if (intent.mode === 'flexible') return '按已确认的生成要求组织画面';
  return [intent.mode === 'preserve_content' ? '保留主图主体、现有文案与布局' : '自由组合所选特征', intent.style && `画法：${intent.style}`, intent.palette && `配色：${intent.palette}`].filter(Boolean).join(' · ');
}
function isFlexible(unit: ImageRewrite) { return unit.workflow_version === 2 && unit.edit_intent?.mode === 'flexible'; }
function controlFor(controls: ImageFeatureControls | undefined, key: keyof ImageFeatures): ImageFeatureControl { return controls?.[key] || { mode: 'image', asset_id: '' }; }
function controlSummary(control: ImageFeatureControl, name: (id: string) => string): string {
  return control.mode === 'auto' ? '交给模型' : control.mode === 'custom' ? `自定义：${control.value?.trim() || '待填写'}` : control.asset_id ? name(control.asset_id) : '请重新选择';
}
function inputIDs(unit?: ImageRewrite): string[] { return unit ? [...new Set([unit.primary_asset_id, ...(unit.references || []).map(ref => ref.asset_id), ...(unit.source_asset_ids || []), unit.style_reference_id].filter((id): id is string => !!id))] : []; }
function WorkflowImage({ src, alt, fit = 'contain' }: { src: string; alt: string; fit?: 'contain' | 'cover' }) {
  const { active } = useWorkspace(); const [open, setOpen] = useState(false);
  return <Image src={src} alt={alt}
    classNames={{ root: 'image-bounded-root', popup: { root: active ? undefined : 'inactive-workspace-portal' } }}
    styles={{ image: { width: '100%', height: '100%', objectFit: fit } }}
    preview={{ open: active && open, onOpenChange: value => { if (active) setOpen(value); } }} />;
}
function AssetImage({ id, alt, fit }: { id: string; alt: string; fit?: 'contain' | 'cover' }) {
  const { platform } = useWorkspace();
  return <WorkflowImage src={assetURL(id, platform)} alt={alt} fit={fit} />;
}
type Props = { rewrite: Rewrite; version: Version; assets: Asset[]; jobs: Job[]; onUpdated: () => void; onApplied: (version: Version, sources: string[], assetID: string) => void };

export default function ImageRewriteWorkspace(props: Props) {
  const { active, platform } = useWorkspace(); const { message } = App.useApp();
  const [initializedUnits, setInitializedUnits] = useState<ImageRewrite[]>([]);
  const units = props.rewrite.image_rewrites?.length ? props.rewrite.image_rewrites : initializedUnits;
  const [selected, setSelected] = useState(units[0]?.id || ''); const [local, setLocal] = useState<ImageRewrite>();
  const [initializing, setInitializing] = useState(false); const [initializeError, setInitializeError] = useState('');
  const [initializeRetry, setInitializeRetry] = useState(0); const initializeAttempt = useRef(''); const initializeInFlight = useRef(false);
  const mounted = useRef(true); const onUpdated = useRef(props.onUpdated); onUpdated.current = props.onUpdated;
  const [creating, setCreating] = useState(false); const [choose, setChoose] = useState(false); const [legacySource, setLegacySource] = useState<ImageRewrite>();
  const saveBeforeLeave = useRef<(() => Promise<ImageRewrite>) | undefined>(undefined);
  async function leaveThen(action: () => void) { try { await saveBeforeLeave.current?.(); action(); } catch (e) { message.error(errorText(e)); } }
  const saved = units.find(item => item.id === selected);
  const unit = local?.id === selected && (!saved || local.revision > saved.revision) ? local : saved;
  const images = props.assets.filter(a => a.status === 'ready' && !a.library_deleted_at && a.mime.startsWith('image/') && a.kind !== 'style_reference');
  const originalImagesKey = images.filter(a => a.owner_id === props.rewrite.note_id).map(a => a.id).join(',');
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => {
    if (units.length) setInitializeError('');
    setSelected(current => current && (units.some(item => item.id === current) || local?.id === current) ? current : units[0]?.id || '');
  }, [units, local]);
  useEffect(() => {
    if (!active || platform !== 'xiaohongshu' || units.length || !originalImagesKey || initializeInFlight.current) return;
    const attempt = `${props.rewrite.id}:${originalImagesKey}:${initializeRetry}`;
    if (initializeAttempt.current === attempt) return;
    initializeAttempt.current = attempt; initializeInFlight.current = true; setInitializing(true); setInitializeError('');
    void api<ImageRewrite[]>(workspacePath(`/rewrites/${props.rewrite.id}/images/initialize`, platform), 'POST').then(next => {
      if (!mounted.current) return;
      setInitializedUnits(next); onUpdated.current();
    }).catch(e => { if (mounted.current) setInitializeError(errorText(e)); }).finally(() => {
      initializeInFlight.current = false; if (mounted.current) setInitializing(false);
    });
  }, [active, platform, props.rewrite.id, units.length, originalImagesKey, initializeRetry]);
  async function create(ids: string[], source = legacySource) {
    setCreating(true);
    try {
      const references: ImageReference[] = source?.references?.length ? source.references.filter(ref => ref.asset_id !== ids[0]).slice(0, 2) : inputIDs(source).filter(id => id !== ids[0]).slice(0, 2).map(asset_id => ({ asset_id }));
      const next = await api<ImageRewrite>(workspacePath(`/rewrites/${props.rewrite.id}/images`, platform), 'POST', { workflow_version: 2, edit_intent: { mode: 'flexible' }, version_id: props.version.id, primary_asset_id: ids[0], references });
      setLocal(next); setSelected(next.id); setChoose(false); setLegacySource(undefined); props.onUpdated();
    } catch (e) { message.error(errorText(e)); } finally { setCreating(false); }
  }
  return <div className="image-workflow-v2">
    <div className="image-scheme-toolbar"><Select aria-label="图片方案" virtual={false} value={unit?.id} placeholder="选择图片方案" onChange={value => void leaveThen(() => { setSelected(value); setLocal(undefined); })}
      options={[...units, ...(local && !units.some(u => u.id === local.id) ? [local] : [])].map((u, i) => ({ value: u.id, label: `方案 ${i + 1}${isFlexible(u) ? '' : ' · 历史方案'}` }))} />
      <Button size="small" icon={<PlusOutlined />} loading={creating} disabled={initializing} onClick={() => void leaveThen(() => { setLegacySource(undefined); setChoose(true); })}>新方案</Button></div>
    {initializeError && <Alert type="error" showIcon title="图片方案初始化失败" description={initializeError} action={<Button size="small" onClick={() => setInitializeRetry(value => value + 1)}>重试</Button>} />}
    {!unit && <Button icon={<PictureOutlined />} loading={initializing} onClick={() => setChoose(true)}>{initializing ? '正在准备图片方案' : '选择被仿写图'}</Button>}
    {unit && isFlexible(unit) && <ImageEditor key={unit.id} {...props} unit={unit} onSaved={setLocal} saveBeforeLeave={saveBeforeLeave} />}
    {unit && !isFlexible(unit) && <div className="image-legacy-view">
      <Space wrap><Tag>历史方案</Tag><Button icon={<PlusOutlined />} loading={creating} onClick={() => { const primary = unit.primary_asset_id || unit.source_asset_ids[0]; if (primary) void create([primary], unit); else { setLegacySource(unit); setChoose(true); } }}>以此创建新版方案</Button></Space>
      <Typography.Paragraph type="secondary">原方案及生成依据已保留。创建新版后，可分析图片、选择建议方向并填写附加要求。</Typography.Paragraph>
      <div className="image-input-grid">{inputIDs(unit).map((id, i) => <Card key={id} size="small" title={`原输入图 ${i + 1}`}><div className="image-input-preview"><AssetImage id={id} alt={`历史输入图 ${i + 1}`} /></div></Card>)}</div>
      <Collapse items={[{ key: 'prompt', label: '原方案提示词', children: <Typography.Paragraph className="preserve">{unit.prompt}</Typography.Paragraph> }]} />
      <Results results={unit.results || []} />
    </div>}
    <PicturePicker title="选择被仿写图" open={active && choose} items={images.map(a => ({ id: a.id, title: a.name || `图片 ${a.position + 1}`, searchText: a.extraction?.text, src: assetURL(a.id, platform), subtitle: `${a.width} × ${a.height}` }))}
      selected={legacySource ? inputIDs(legacySource).slice(0, 1) : []} limit={1} loading={creating} empty="请先采集笔记或在项目素材库上传图片" onCancel={() => setChoose(false)} onConfirm={ids => void create(ids)} />
  </div>;
}

type Form = Pick<ImageRewrite, 'primary_asset_id' | 'references' | 'output' | 'prompt' | 'selected_proposal_id' | 'extra_requirements'> & { edit_intent: ImageEditIntent };
function toForm(u: ImageRewrite): Form { return { primary_asset_id: u.primary_asset_id || '', references: u.references || [], selected_proposal_id: u.selected_proposal_id || '', extra_requirements: u.extra_requirements || '', edit_intent: { mode: 'flexible' }, output: u.output || { ratio: '3:4', clarity: 'standard', count: 1 }, prompt: u.prompt || '' }; }
const signature = (v: unknown) => JSON.stringify(v);
const imageBasisSignature = (f: Form) => signature([f.primary_asset_id, (f.references || []).map(r => r.asset_id || `official:${r.library_id}`)]);
const semanticSignature = (f: Form) => signature([imageBasisSignature(f), f.selected_proposal_id, f.extra_requirements?.trim(), f.output?.ratio]);

function ImageEditor({ unit, rewrite, version, assets, jobs, onSaved, onUpdated, onApplied, saveBeforeLeave }: Props & { unit: ImageRewrite; onSaved: (u: ImageRewrite) => void; saveBeforeLeave: { current: (() => Promise<ImageRewrite>) | undefined } }) {
  const { platform, active } = useWorkspace(); const { message, modal } = App.useApp();
  const [current, setCurrent] = useState(unit); const currentRef = useRef(unit);
  const [form, setForm] = useState<Form>(() => toForm(unit)); const formRef = useRef(form); const baseline = useRef(signature(form));
  const chain = useRef<Promise<unknown>>(Promise.resolve()); const saving = useRef(false);
  const [saveStatus, setSaveStatus] = useState(''); const [saveError, setSaveError] = useState(''); const [busy, setBusy] = useState('');
  const saveErrorRef = useRef('');
  const [step, setStep] = useState(unit.results?.some(r => r.revision === unit.revision) ? 2 : unit.prompt_input_digest ? 1 : 0);
  const [picker, setPicker] = useState<number>(); const [uploaded, setUploaded] = useState<Asset[]>([]); const [uploading, setUploading] = useState(false);
  const [taskID, setTaskID] = useState(''); const seenTask = useRef(''); const intent = useRef<{ revision: number; prompt: string; id: string } | undefined>(undefined);
  const navigation = useRef(0); const taskNavigation = useRef(0);
  const capabilities = useResource<ImageCapabilities>(active ? workspacePath('/image-capabilities', platform) : null);
  const catalogue = useResource<StyleCatalogue>(active && picker !== undefined && picker >= 0 ? workspacePath('/style-library', platform) : null);
  const task = jobs.find(j => j.id === taskID);
  const running = jobs.some(j => j.entity_id === rewrite.id && j.scene_id === unit.id && ['queued', 'running'].includes(j.state)) || (!!taskID && (!task || ['queued', 'running'].includes(task.state)));
  const batch = current.batches?.at(-1); const paused = !!batch && ['paused', 'submission_unknown'].includes(batch.state);
  const locked = running || paused || !!busy;
  const lockedRef = useRef(locked); lockedRef.current = locked;
  useJobsCleared(platform, ids => {
    if (ids.includes(taskID)) { setTaskID(''); intent.current = undefined; }
    onUpdated();
  });
  const dirty = signature(form) !== baseline.current;
  const path = `/rewrites/${rewrite.id}/images/${unit.id}`;
  function change(update: (f: Form) => Form) { if (lockedRef.current) return; const next = update(formRef.current); formRef.current = next; setForm(next); saveErrorRef.current = ''; setSaveError(''); }
  useEffect(() => {
    if (saving.current || signature(formRef.current) !== baseline.current || unit.revision < currentRef.current.revision) return;
    currentRef.current = unit; setCurrent(unit); const next = toForm(unit); formRef.current = next; baseline.current = signature(next); setForm(next);
  }, [unit]);
  async function flush(retry = false): Promise<ImageRewrite> {
    const work = chain.current.then(async () => {
      if (saveErrorRef.current && !retry) throw new Error(saveErrorRef.current);
      const draft = formRef.current;
      if (signature(draft) === baseline.current) return currentRef.current;
      saving.current = true; setSaveStatus('正在保存'); saveErrorRef.current = ''; setSaveError('');
      try {
        const next = await api<ImageRewrite>(workspacePath(path, platform), 'PATCH', { revision: currentRef.current.revision, ...draft });
        currentRef.current = next; setCurrent(next); baseline.current = signature(toForm(next));
        if (signature(formRef.current) === signature(draft)) { const saved = toForm(next); formRef.current = saved; setForm(saved); }
        else {
          const latest = { ...formRef.current, references: formRef.current.references?.map(ref => !ref.asset_id && ref.library_id ? next.references?.find(saved => saved.library_id === ref.library_id) || ref : ref) };
          formRef.current = latest; setForm(latest);
        }
        onSaved(next); onUpdated(); setSaveStatus(''); return next;
      } catch (e) { saveErrorRef.current = errorText(e); setSaveError(saveErrorRef.current); setSaveStatus(''); throw e; }
      finally { saving.current = false; }
    }); chain.current = work.catch(() => undefined); return work;
  }
  useEffect(() => { saveBeforeLeave.current = flush; return () => { saveBeforeLeave.current = undefined; }; });
  useEffect(() => { if (!dirty || locked || !active || saveError) return; const timer = setTimeout(() => { void flush().catch(() => undefined); }, 600); return () => clearTimeout(timer); }, [form, dirty, locked, active, saveError]);
  useEffect(() => {
    if (!task || ['queued', 'running'].includes(task.state) || seenTask.current === task.id) return;
    seenTask.current = task.id; setTaskID(''); onUpdated();
    if (task.state === 'succeeded' && task.kind === 'image_compose' && taskNavigation.current === navigation.current) setStep(1);
  }, [task, onUpdated]);
  async function run(kind: 'analyze' | 'compose-prompt' | 'generate') {
    taskNavigation.current = navigation.current;
    setBusy(kind);
    try {
      const saved = await flush(); let requestID: string | undefined;
      if (kind === 'generate') { if (!intent.current || intent.current.revision !== saved.revision || intent.current.prompt !== saved.prompt) intent.current = { revision: saved.revision, prompt: saved.prompt, id: crypto.randomUUID() }; requestID = intent.current.id; }
      const result = await api<AsyncResult>(workspacePath(`${path}/${kind}`, platform), 'POST', { revision: saved.revision, ...(kind === 'generate' ? { prompt: saved.prompt, request_id: requestID } : {}), ...(kind === 'analyze' ? { suggest_proposals: true } : {}) });
      const cleared = !!result.job_id && jobWasCleared(platform, result.job_id);
      setTaskID(cleared ? '' : result.job_id || ''); if (kind === 'generate') { intent.current = undefined; if (!cleared && taskNavigation.current === navigation.current) setStep(2); } onUpdated();
    } catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  async function resume(action: string, confirmNoResult = false) {
    if (!batch) return; setBusy(action);
    try { const result = await api<AsyncResult>(workspacePath(`${path}/batches/${batch.id}/resume`, platform), 'POST', { action, confirm_no_result: confirmNoResult }); setTaskID(result.job_id && !jobWasCleared(platform, result.job_id) ? result.job_id : ''); onUpdated(); }
    catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  function recover(action: string) {
    if (batch?.state !== 'submission_unknown' || action === 'restore') { void resume(action); return; }
    modal.confirm({ title: '已核实供应商没有生成结果？', content: '请先核对供应商记录。存在结果时使用恢复下载，避免再次生成。', okText: '已核实没有结果', cancelText: '返回核实', onOk: () => resume(action, true) });
  }
  async function apply(result: ImageResult) {
    setBusy(result.asset_id);
    try { const saved = await flush(); if (result.primary_asset_id !== saved.primary_asset_id) throw new Error('主图已更改，请在对应方案中采用这张历史结果'); const next = await api<Version>(workspacePath(`${path}/apply`, platform), 'POST', { asset_id: result.asset_id, version_id: version.id }); onApplied(next, [saved.primary_asset_id!, ...saved.results.filter(r => r.primary_asset_id === saved.primary_asset_id).map(r => r.asset_id)], result.asset_id); onUpdated(); message.success('已替换主图，文案和其他素材已保留'); }
    catch (e) { message.error(errorText(e)); } finally { setBusy(''); }
  }
  function remove(slot: number) {
    change(f => slot < 0 ? { ...f, primary_asset_id: '', selected_proposal_id: '' } : { ...f, references: f.references?.filter((_, i) => i !== slot), selected_proposal_id: '' });
  }
  const allAssets = [...assets, ...uploaded.filter(a => !assets.some(existing => existing.id === a.id))];
  const privateImages = allAssets.filter(a => a.status === 'ready' && !a.library_deleted_at && a.mime.startsWith('image/') && a.kind !== 'style_reference');
  const choices: PictureChoice[] = privateImages.map(a => ({ id: a.id, title: a.name || `图片 ${a.position + 1}`, searchText: [a.extraction?.text, ...Object.values(current.analyses.find(analysis => analysis.asset_id === a.id)?.features || {})].filter(Boolean).join(' '), src: assetURL(a.id, platform), subtitle: `${a.width} × ${a.height}`, group: a.owner_id === rewrite.note_id ? 'note' : 'project' }));
  if (picker !== undefined && picker >= 0) choices.push(...(catalogue.data?.items || []).map(item => ({ ...item, id: `official:${item.id}`, src: `/api/v1${workspacePath(`/style-library/${encodeURIComponent(item.id)}/file`, platform)}`, group: 'official' })));
  const selectedIDs = [form.primary_asset_id, ...(form.references || []).map(r => r.asset_id)].filter(Boolean);
  const pickerChoices = choices.filter(item => !selectedIDs.includes(item.id) || (picker === -1 ? item.id === form.primary_asset_id : item.id === form.references?.[picker!]?.asset_id));
  async function addUpload(file: File, slot?: number) {
    if (locked) return false;
    if (file.size > 10 * 1024 * 1024) { message.warning('输入图片不得超过 10 MB'); return false; } setUploading(true);
    try { const asset = await upload(file, rewrite.id, workspacePath('/assets', platform)) as Asset; setUploaded(items => [...items, asset]); if (slot !== undefined) chooseSlot(slot, asset.id); onUpdated(); message.success('已上传到项目素材库'); }
    catch (e) { message.error(errorText(e)); } finally { setUploading(false); } return false;
  }
  function chooseSlot(slot: number, id: string) {
    change(f => {
      if (slot < 0) return id === f.primary_asset_id ? f : { ...f, primary_asset_id: id, selected_proposal_id: '' };
      const previous = f.references?.[slot]; const next: ImageReference = id.startsWith('official:') ? previous?.library_id === id.slice(9) ? previous : { asset_id: '', library_id: id.slice(9) } : { asset_id: id };
      const references = [...(f.references || [])]; references.splice(Math.min(slot, references.length), 1, next);
      return { ...f, references, selected_proposal_id: previous?.asset_id === next.asset_id && previous?.library_id === next.library_id ? f.selected_proposal_id : '' };
    });
  }
  function choose(ids: string[]) { chooseSlot(picker!, ids[0]); setPicker(undefined); }
  const names = new Map<string, string>([[form.primary_asset_id || '', '主图'], ...(form.references || []).map((r, i): [string, string] => [r.asset_id, `辅助图 ${i + 1}`])]);
  const semanticDirty = semanticSignature(form) !== semanticSignature(toForm(current));
  const proposals = current.proposals || [];
  const proposalsReady = !!current.proposals_input_digest && proposals.length === 3 && imageBasisSignature(form) === imageBasisSignature(toForm(current));
  const selectedProposal = proposalsReady ? proposals.find(proposal => proposal.id === form.selected_proposal_id) : undefined;
  const controls = selectedProposal?.feature_controls || current.feature_controls;
  const rulesCurrent = (current.prompt_rules_version || 0) >= (capabilities.data?.prompt_rules_version || 3);
  const ready = !!current.prompt_input_digest && rulesCurrent && !semanticDirty;
  const selections = dimensions.filter(d => controlFor(controls, d.key).mode === 'image');
  const contributingSources = new Set(selections.map(d => controlFor(controls, d.key).asset_id).filter((id): id is string => !!id && selectedIDs.includes(id)));
  const controlsReady = !!form.primary_asset_id && !!selectedProposal;
  const size = capabilities.data?.sizes.find(s => s.ratio === form.output?.ratio && s.clarity === form.output?.clarity);
  const currentResults = semanticDirty ? [] : current.results.filter(r => r.revision === current.revision && r.prompt === current.prompt); const history = current.results.filter(r => !currentResults.includes(r));
  return <>
    <nav className="image-workflow-steps" aria-label="图片仿写阶段">{['选图与方案', '审阅提示词', '审阅结果'].map((label, index) => <button type="button" key={label} aria-current={step === index ? 'step' : undefined} onClick={() => { navigation.current++; setStep(index); }}>{index + 1}. {label}</button>)}</nav>
    {saveError && <Alert type="error" title={`草稿保存失败：${saveError}`} action={<Button size="small" onClick={() => void flush(true).catch(() => undefined)}>重试保存</Button>} />}
    {current.error && <Alert type="warning" title={current.error} />}
    {capabilities.error && <Alert type="warning" title={capabilities.error} action={<Button size="small" onClick={() => void capabilities.reload()}>重试</Button>} />}
    {step === 0 && <>
      <Typography.Paragraph type="secondary" className="image-control-note">选择主图和可选参考图，点击下方“分析图片”，再从三个建议方向中选择一个。</Typography.Paragraph>
      <div className="image-input-grid">{[-1, 0, 1].map(slot => {
        const reference = slot >= 0 ? form.references?.[slot] : undefined; const id = slot < 0 ? form.primary_asset_id : reference?.asset_id; const title = slot < 0 ? '被仿写图' : `辅助参考图 ${slot + 1}`;
        const preview = id ? assetURL(id, platform) : reference?.library_id ? `/api/v1${workspacePath(`/style-library/${encodeURIComponent(reference.library_id)}/file`, platform)}` : '';
        return <ReferenceSlotCard key={slot} title={title} kind="image" active={active} disabled={locked || uploading} primary={slot < 0}
          badge={selectedProposal && id && !contributingSources.has(id) ? <Tag>未参与生成</Tag> : undefined}
          preview={preview ? <div className="image-input-preview">{id ? <AssetImage id={id} alt={title} fit="cover" /> : <WorkflowImage src={preview} alt={title} fit="cover" />}</div> : undefined}
          onChoose={() => setPicker(slot)} onRemove={() => remove(slot)} onUpload={file => addUpload(file, slot)} />;
      })}</div>
      <section className="image-proposals" aria-labelledby={`image-proposals-${unit.id}`}>
        <div className="image-proposals-heading"><Typography.Text strong id={`image-proposals-${unit.id}`}>选择生成方向</Typography.Text>{proposalsReady && <Typography.Text type="secondary">请选择一个方案</Typography.Text>}</div>
        {proposalsReady ? <Radio.Group aria-label="生成方向" className="image-proposal-options" value={form.selected_proposal_id || undefined} disabled={locked} onChange={event => change(f => ({ ...f, selected_proposal_id: event.target.value }))}>
          {proposals.map((proposal, index) => <Radio className="image-proposal-option" key={proposal.id} value={proposal.id}>
            <span className="image-proposal-body"><strong>{index + 1}. {proposal.title}</strong><span>{proposal.summary}</span></span>
          </Radio>)}
        </Radio.Group> : <div className="image-proposals-placeholder">{proposals.length ? '图片已更换，请重新点击“分析图片”更新三个方案。' : '分析完成后，这里会列出三个可选方案。无需添加参考图。'}</div>}
        {capabilities.data && !capabilities.data.text_configured && <Typography.Text type="secondary">分析图片并生成三个方案前，请先配置文本模型。</Typography.Text>}
      </section>
      <Field label="附加要求（可选）"><Input.TextArea aria-label="附加要求" value={form.extra_requirements} disabled={locked} maxLength={1000} showCount autoSize={{ minRows: 2, maxRows: 5 }} placeholder="补充希望保留或修改的内容，也可以留空" onChange={event => change(f => ({ ...f, extra_requirements: event.target.value }))} /></Field>
    </>}
    {step === 1 && <div className="image-prompt-review">{!ready && <Tag color="warning">{current.prompt && !rulesCurrent ? '旧提示词需按当前特征重新编排' : '提示词待更新'}</Tag>}
      <Field label="生成提示词"><Input.TextArea aria-label="生成提示词" autoSize={{ minRows: 8, maxRows: 18 }} value={form.prompt} disabled={locked || !ready} showCount maxLength={4000} onChange={e => change(f => ({ ...f, prompt: e.target.value }))} /></Field>
      <Button size="small" disabled={locked || !controlsReady || !capabilities.data?.text_configured} onClick={() => void run('compose-prompt')}>重新编排提示词</Button>
    </div>}
    {step === 2 && <>
      {batch && <div className="image-batch-status"><Space wrap>{batch.items.map(item => <Tag key={item.index}>图片 {item.index + 1} · {({ waiting: '待生成', queued: '排队中', running: '生成中', succeeded: '已保存', failed: '失败', submission_unknown: '待核实', skipped: '已跳过', cancelled: '已取消', waiting_config: '待配置' } as Record<string, string>)[item.state] || item.state}</Tag>)}</Space>
        {paused && <Space wrap><Button disabled={!!busy} onClick={() => recover('retry')}>重试当前图片</Button><Button disabled={!!busy} onClick={() => recover('restore')}>恢复已返回图片</Button><Button disabled={!!busy} onClick={() => recover('continue')}>跳过并继续</Button><Button disabled={!!busy} onClick={() => recover('cancel')}>结束本批次</Button></Space>}
      </div>}
      {!currentResults.length && !!history.length && <Typography.Paragraph type="secondary">当前修改尚未生成图片，历史结果使用当时的生成依据。</Typography.Paragraph>}
      <Results results={currentResults} locked={locked} onApply={apply} applied={rewrite.versions.find(v => v.id === current.applied_version_id)?.asset_ids.find(id => current.results.some(r => r.asset_id === id))} />
      {!!history.length && <Collapse items={[{ key: 'history', label: `历史结果（${history.length}）`, children: <Results results={history} /> }]} />}
    </>}
    <WorkspaceActionDock className="image-workflow-actions"><div className="image-output-options">
      <Select aria-label="图片比例" size="small" virtual={false} value={form.output?.ratio} disabled={locked} options={['3:4', '1:1', '9:16'].map(value => ({ value, label: value }))} onChange={ratio => change(f => ({ ...f, output: { ...f.output!, ratio } }))} />
      <Select aria-label="图片清晰度" size="small" virtual={false} value={form.output?.clarity} disabled={locked} options={[{ value: 'standard', label: '标准' }, { value: 'high', label: '高清' }]} onChange={clarity => change(f => ({ ...f, output: { ...f.output!, clarity } }))} />
      <Select aria-label="生成数量" size="small" virtual={false} value={form.output?.count} disabled={locked} options={[1, 2, 3, 4].map(value => ({ value, label: `${value} 张` }))} onChange={count => change(f => ({ ...f, output: { ...f.output!, count } }))} />
      <span className="subtle">{size ? `${size.width} × ${size.height}` : '读取输出尺寸'}{saveStatus && ` · ${saveStatus}`}</span>
    </div><Space wrap>
      {step === 0 && <><Button aria-label={capabilities.data && !capabilities.data.vision_configured ? '图片分析待配置' : capabilities.data && !capabilities.data.text_configured ? '方案建议待配置' : '分析图片'} icon={<ExperimentOutlined />} loading={busy === 'analyze' || (running && task?.kind === 'image_analyze')} disabled={locked || !form.primary_asset_id || !capabilities.data?.vision_configured || !capabilities.data?.text_configured} onClick={() => void run('analyze')}>{capabilities.data && !capabilities.data.vision_configured ? '图片分析待配置' : capabilities.data && !capabilities.data.text_configured ? '方案建议待配置' : '分析图片'}</Button>
        <Button type="primary" className="ai-generation-button" icon={<GenerationSparkle />} loading={busy === 'compose-prompt' || (running && task?.kind === 'image_compose')} disabled={locked || !controlsReady || !capabilities.data?.text_configured} onClick={() => void run('compose-prompt')}>{capabilities.data && !capabilities.data.text_configured ? '提示词编排待配置' : '生成提示词'}</Button></>}
      {step === 1 && <><Button onClick={() => setStep(0)}>返回选图</Button><ParticleGenerateButton ariaLabel={`确认并生成 ${form.output?.count} 张`}
        disabled={locked || !ready || !form.prompt?.trim() || !capabilities.data?.image_configured || !capabilities.data.supported}
        onGenerate={() => run('generate')} label={capabilities.data && !capabilities.data.image_configured ? '图片生成待配置' : capabilities.data && !capabilities.data.supported ? '当前模型不支持' : `确认并生成 ${form.output?.count} 张`} /></>}
      {step === 2 && <Button disabled={locked} onClick={() => setStep(1)}>返回修改提示词</Button>}
    </Space></WorkspaceActionDock>
    <PicturePicker title={picker === -1 ? '选择被仿写图' : '选择辅助参考图'} open={active && picker !== undefined} items={pickerChoices}
      groups={[{ value: 'note', label: '本笔记' }, { value: 'project', label: '项目素材' }, ...(picker !== -1 ? [{ value: 'official', label: '官方图库' }] : [])]}
      categories={picker !== -1 ? catalogue.data?.categories : undefined} loading={uploading} error={picker !== -1 ? catalogue.error : undefined} onRetry={() => void catalogue.reload()}
      toolbar={<Upload accept="image/*" showUploadList={false} beforeUpload={file => addUpload(file)}><Button size="small" loading={uploading} icon={<UploadOutlined />}>上传图片</Button></Upload>}
      selected={picker === -1 ? form.primary_asset_id ? [form.primary_asset_id] : [] : form.references?.[picker!]?.library_id ? [`official:${form.references[picker!].library_id}`] : form.references?.[picker!]?.asset_id ? [form.references[picker!].asset_id] : []}
      limit={1} empty="暂无可选图片，可上传图片；官方图库等待上架" onCancel={() => setPicker(undefined)} onConfirm={choose} />
  </>;
}

function Results({ results, locked, onApply, applied }: { results: ImageResult[]; locked?: boolean; onApply?: (result: ImageResult) => void; applied?: string }) {
  const { platform } = useWorkspace();
  return <div className="image-result-grid">{results.map(result => <Card key={result.asset_id} size="small" className="image-result-card">
    <div className="image-result-preview"><AssetImage id={result.asset_id} alt="图片仿写结果" /></div>{result.warning && <Typography.Paragraph type="warning">{result.warning}</Typography.Paragraph>}
    <Space wrap><Tag>{result.width || '—'} × {result.height || '—'}</Tag><Tag>待审阅</Tag></Space><div className="subtle">{formatDate(result.created_at)}</div>
    <Space wrap>{onApply && <Button size="small" type="primary" disabled={locked || applied === result.asset_id} onClick={() => onApply(result)}>{applied === result.asset_id ? '已采用' : '采用此图'}</Button>}<Button size="small" href={assetURL(result.asset_id, platform)} download>下载</Button></Space>
    <Collapse size="small" items={[{ key: 'basis', label: '本次生成依据', children: <>
      {result.selected_proposal && <Typography.Paragraph><Typography.Text strong>{result.selected_proposal.title}</Typography.Text> · {result.selected_proposal.summary}</Typography.Paragraph>}
      {result.extra_requirements && <Typography.Paragraph>附加要求：{result.extra_requirements}</Typography.Paragraph>}
      <div className="image-result-inputs">{(result.source_asset_ids || []).map((id, i) => <div key={id}><AssetImage id={id} alt={id === result.primary_asset_id ? '本次被仿写图' : `本次输入图片 ${i + 1}`} /><span>{`图片 ${i + 1}${id === result.primary_asset_id ? ' · 主图' : ''}`}</span></div>)}</div>
      {result.feature_controls ? <Typography.Paragraph className="image-result-feature-controls">{dimensions.map(d => `${d.label}：${controlSummary(controlFor(result.feature_controls, d.key), id => { const index = result.source_asset_ids?.indexOf(id) ?? -1; return index >= 0 ? `图片 ${index + 1}${id === result.primary_asset_id ? '（主图）' : ''}` : '所选图片'; })}`).join(' · ')}</Typography.Paragraph> : result.feature_sources && <Typography.Paragraph className="image-result-feature-sources">{dimensions.filter(d => result.feature_sources?.[d.key]).map(d => {
        const source = result.feature_sources![d.key]; const index = result.source_asset_ids?.indexOf(source!);
        return `${d.label}：${source === result.primary_asset_id ? '主图' : index && index > 0 ? `辅助图 ${index}` : '所选图片'}`;
      }).join(' · ')}</Typography.Paragraph>}
      {!result.feature_controls && !result.feature_sources && <Typography.Paragraph type="secondary">旧记录未保存特征来源。</Typography.Paragraph>}
      {result.edit_intent?.mode && <Typography.Paragraph className="image-result-edit-intent">{intentSummary(result.edit_intent)}</Typography.Paragraph>}
      <Typography.Paragraph className="preserve">{result.prompt}</Typography.Paragraph><Typography.Text type="secondary">{result.model}</Typography.Text>
    </> }]} />
  </Card>)}</div>;
}
