import { useEffect, useState, type ReactNode } from 'react';
import { Button, Checkbox, Empty, Input, Modal, Select, Space, Spin, Tag, Segmented } from 'antd';
import { EyeOutlined } from '@ant-design/icons';
import { useWorkspace } from '../workspace';
import { Failure } from './Common';

export interface PictureChoice {
  id: string; title: string; src: string; subtitle?: string; searchText?: string; category?: string; tags?: string[]; credit?: string; group?: string;
}

// Selections remain a local draft until confirmed. Preview clicks, filtering
// and cancelling never change the caller's saved choice.
export default function PicturePicker({ open, title, items, selected, limit, categories, groups, toolbar, loading, error, empty, onRetry, onCancel, onConfirm }: {
  open: boolean; title: string; items: PictureChoice[]; selected: string[]; limit: number;
  categories?: { id: string; name: string }[]; loading?: boolean; error?: string; empty?: string;
  groups?: { value: string; label: string }[]; toolbar?: ReactNode;
  onRetry?: () => void; onCancel: () => void; onConfirm: (ids: string[]) => void;
}) {
  const { active } = useWorkspace();
  const [draft, setDraft] = useState<string[]>([]); const [search, setSearch] = useState('');
  const [category, setCategory] = useState(''); const [preview, setPreview] = useState<PictureChoice>();
  const [group, setGroup] = useState('');
  useEffect(() => { if (open) { setDraft([...selected]); setSearch(''); setCategory(''); setGroup(''); setPreview(undefined); } }, [open]);
  // Dimensions are display metadata, not image names or content keywords.
  const terms = search.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  const choices = items.filter(item => {
    const text = `${item.title} ${item.searchText || ''} ${item.tags?.join(' ') || ''}`.toLocaleLowerCase();
    return (!group || item.group === group) && (!category || item.category === category) && terms.every(term => text.includes(term));
  });
  const valid = draft.filter(id => items.some(item => item.id === id));
  function toggle(id: string, checked: boolean) {
    setDraft(ids => checked ? limit === 1 ? [id] : ids.includes(id) || ids.length >= limit ? ids : [...ids, id] : ids.filter(value => value !== id));
  }
  return <>
    <Modal title={title} open={active && open} width={880} className="picture-picker" rootClassName={!active ? 'inactive-workspace-portal' : undefined}
      onCancel={onCancel} onOk={() => onConfirm(valid)} okText="确认选择" cancelText="取消" cancelButtonProps={{ 'aria-label': '取消' }} okButtonProps={{ disabled: !valid.length || !!error || loading }}>
      {(groups || toolbar) && <Space wrap className="picture-picker-sources">{groups && <Segmented aria-label="图片来源" options={[{ value: '', label: '全部' }, ...groups]} value={group} onChange={value => { setGroup(value); setCategory(''); }} />}{toolbar}</Space>}
      <div className="picture-picker-toolbar"><Input.Search aria-label={`${title}搜索`} allowClear placeholder="搜索图片名称或关键词" value={search} onChange={e => setSearch(e.target.value)} onSearch={setSearch} />
        {!!categories?.length && <Select aria-label="风格参考分类" virtual={false} value={category} onChange={setCategory} options={[{ value: '', label: '全部领域' }, ...categories.map(c => ({ value: c.id, label: c.name }))]} />}
        <Tag>已选 {valid.length} / {limit}</Tag>
      </div>
      <Failure error={error || ''} retry={onRetry} />
      <Spin spinning={!!loading}><div className="picture-picker-body">
        {!choices.length && !loading && !error && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={items.length ? '没有符合条件的图片' : empty || '暂无可用图片'} />}
        <div className="picture-choice-grid">{choices.map(item => <div key={item.id} className={`picture-choice ${draft.includes(item.id) ? 'is-selected' : ''}`}>
          <button type="button" className="picture-choice-preview" aria-label={`预览${item.title}`} onClick={() => setPreview(item)}><img src={item.src} alt={item.title} loading="lazy" /><span><EyeOutlined /> 预览</span></button>
          <div className="picture-choice-info"><Checkbox aria-label={`选择${item.title}`} checked={draft.includes(item.id)} disabled={limit > 1 && draft.length >= limit && !draft.includes(item.id)} onChange={e => toggle(item.id, e.target.checked)}>{item.title}</Checkbox>
            {item.subtitle && <div className="subtle">{item.subtitle}</div>}{!!item.tags?.length && <Space size={[0, 4]} wrap>{item.tags.map(tag => <Tag key={tag}>{tag}</Tag>)}</Space>}
          </div>
        </div>)}</div>
      </div></Spin>
    </Modal>
    <Modal title={preview?.title} open={active && open && !!preview} width={820} zIndex={1100} rootClassName={!active ? 'inactive-workspace-portal' : undefined}
      onCancel={() => setPreview(undefined)} footer={<Button onClick={() => setPreview(undefined)}>关闭预览</Button>}>
      {preview && <><img className="picture-full-preview" src={preview.src} alt={`${preview.title}大图`} />{preview.credit && <div className="subtle">{preview.credit}</div>}</>}
    </Modal>
  </>;
}
