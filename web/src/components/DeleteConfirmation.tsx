import { useEffect, useState } from 'react';
import { Alert, Checkbox, Modal, Typography } from 'antd';
import { errorText } from '../api';
import { useWorkspace } from '../workspace';

/** A second, explicit confirmation keeps destructive actions distinct from navigation. */
export default function DeleteConfirmation({ open, title, subject, consequences, onCancel, onConfirm }: {
  open: boolean; title: string; subject: string; consequences: string[];
  onCancel: () => void; onConfirm: () => Promise<void>;
}) {
  const { active } = useWorkspace();
  const [stage, setStage] = useState(1); const [accepted, setAccepted] = useState(false);
  const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  useEffect(() => { if (!open) { setStage(1); setAccepted(false); setError(''); } }, [open]);
  async function confirm() {
    if (stage === 1) { setStage(2); return; }
    if (!accepted || busy) return;
    setBusy(true); setError('');
    try { await onConfirm(); onCancel(); }
    catch (e) { setError(errorText(e)); }
    finally { setBusy(false); }
  }
  return <Modal title={stage === 1 ? title : `再次确认：${title}`} open={active && open}
    rootClassName={active && open ? undefined : 'inactive-workspace-portal'}
    onCancel={() => { if (!busy) onCancel(); }} onOk={() => void confirm()}
    okText={stage === 1 ? '继续删除' : '确认删除'} cancelText="取消" confirmLoading={busy}
    okButtonProps={{ danger: true, disabled: stage === 2 && !accepted }}
    cancelButtonProps={{ disabled: busy }} closable={!busy} mask={{ closable: !busy }}>
    <Typography.Paragraph>将删除 <Typography.Text strong>{subject}</Typography.Text>。</Typography.Paragraph>
    <ul className="delete-consequences">{consequences.map(text => <li key={text}>{text}</li>)}</ul>
    <Alert showIcon type="warning" title="删除后无法恢复" description="排队任务会一并移除。有关联任务正在运行或提交结果待核实时，服务会阻止删除。请先在后台任务中查看状态。" />
    {stage === 2 && <Checkbox className="delete-acknowledgement" checked={accepted} onChange={e => setAccepted(e.target.checked)}>我已了解删除范围，确认永久删除</Checkbox>}
    {error && <Alert className="resource-error" showIcon type="error" title={error} />}
  </Modal>;
}
