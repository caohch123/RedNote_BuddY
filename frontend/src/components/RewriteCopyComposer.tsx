import type { Dispatch, SetStateAction } from 'react';
import { AutoComplete, Button, Input, Select } from 'antd';
import { ArrowUpOutlined } from '@ant-design/icons';
import type { RewriteOptions, Version } from '../types';
import { Field } from './Common';
import WorkspaceActionDock from './WorkspaceActionDock';

interface Props {
  options: RewriteOptions;
  onOptionsChange: Dispatch<SetStateAction<RewriteOptions>>;
  displayModel: string;
  onModelChange: (value: string) => void;
  versions: Version[];
  activeVersion: string;
  confirmedVersion?: string;
  onVersionChange: (value: string) => void;
  onGenerate: () => void;
  generating: boolean;
  pending: boolean;
  disabled: boolean;
}

export default function RewriteCopyComposer({ options, onOptionsChange, displayModel, onModelChange, versions, activeVersion,
  confirmedVersion, onVersionChange, onGenerate, generating, pending, disabled }: Props) {
  const blocked = disabled || generating || pending;
  return <WorkspaceActionDock className="copy-composer-dock">
    <section className="copy-composer" aria-label="AI 文案对话框">
      <Input.TextArea aria-label="补充要求" className="copy-composer-input" value={options.instructions} variant="borderless"
        onChange={event => onOptionsChange(previous => ({ ...previous, instructions: event.target.value }))}
        autoSize={{ minRows: 2, maxRows: 4 }} placeholder="描述你希望怎样调整这篇笔记…"
        aria-keyshortcuts="Control+Enter Meta+Enter" onKeyDown={event => {
          if (event.key === 'Enter' && (event.ctrlKey || event.metaKey) && !event.nativeEvent.isComposing) {
            event.preventDefault(); if (!blocked) onGenerate();
          }
        }} />
      <div className="options-grid copy-options-grid">
        <Field label="目标读者"><Input aria-label="目标读者" variant="filled" value={options.audience}
          onChange={event => onOptionsChange(previous => ({ ...previous, audience: event.target.value }))} placeholder="如：职场新人、宝妈" /></Field>
        <Field label="表达语气"><Select aria-label="表达语气" variant="filled" value={options.tone} popupMatchSelectWidth={false}
          onChange={value => onOptionsChange(previous => ({ ...previous, tone: value }))}
          options={['自然分享', '专业实用', '轻松幽默', '故事叙述', '简洁直接'].map(value => ({ value, label: value }))} /></Field>
        <Field label="正文长度"><Select aria-label="正文长度" variant="filled" value={options.length} popupMatchSelectWidth={false}
          onChange={value => onOptionsChange(previous => ({ ...previous, length: value }))}
          options={['精简（约 200 字）', '中等（约 500 字）', '详细（约 800 字）'].map(value => ({ value, label: value }))} /></Field>
      </div>
      <div className="copy-composer-footer">
        <div className="copy-model-picker">
          <AutoComplete aria-label="文案模型" variant="borderless" value={displayModel} onChange={onModelChange} placeholder="选择或输入模型名称"
            popupMatchSelectWidth={false} options={['默认模型', '通义千问', 'DeepSeek'].map(value => ({ value }))} />
          <Select aria-label="仿写版本" variant="borderless" className="rewrite-version-select" value={activeVersion || undefined} placeholder="尚无保存版本"
            popupMatchSelectWidth={false} onChange={onVersionChange}
            options={versions.map(version => ({ value: version.id, label: `版本 ${version.number}${version.id === confirmedVersion ? ' · 已确认' : ''}` }))} />
        </div>
        <Button aria-label="AI 生成新版本" title={pending ? '正在生成新版本' : 'AI 生成新版本（Ctrl / ⌘ + Enter）'}
          className="copy-ai-generate" type="primary" shape="circle" icon={<ArrowUpOutlined aria-hidden="true" />}
          onClick={onGenerate} loading={generating || pending} disabled={blocked} />
      </div>
    </section>
  </WorkspaceActionDock>;
}
