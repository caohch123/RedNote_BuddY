import { useState } from 'react';
import { App, Button, Card, Typography } from 'antd';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import type { Platform } from '../types';
import { platformNames } from '../platform';
import { useWorkspaceRegistry } from '../workspaceRegistry';
import DeleteConfirmation from './DeleteConfirmation';
import { errorText } from '../api';
import { deletionSuccess } from '../deletion';

export default function WorkspaceManager({ empty = false }: { empty?: boolean }) {
  const registry = useWorkspaceRegistry(); const { message } = App.useApp();
  const [deleting, setDeleting] = useState<Platform>(); const [adding, setAdding] = useState<Platform>();
  async function add(platform: Platform) {
    setAdding(platform);
    try { await registry.add(platform); message.success(`已添加${platformNames[platform]}工作区`); }
    catch (e) { message.error(errorText(e)); }
    finally { setAdding(undefined); }
  }
  return <Card title="工作区管理" className="workspace-manager">
    <Typography.Paragraph type="secondary">工作区拥有独立的采集、仿写、视频和发布数据。可删除工作区，再添加一个空白工作区。</Typography.Paragraph>
    <div className="workspace-management-list">{(['xiaohongshu'] as Platform[]).map(platform => {
      const exists = registry.items.some(item => item.platform === platform);
      return <div key={platform} className="workspace-management-row"><div><strong>{platformNames[platform]}工作区</strong><div className="field-hint">{exists ? '已添加' : '尚未添加'}</div></div>
        {exists ? <Button danger icon={<DeleteOutlined />} onClick={() => setDeleting(platform)}>删除工作区</Button>
          : <Button type={empty ? 'primary' : 'default'} icon={<PlusOutlined />} loading={adding === platform} disabled={!!adding} onClick={() => void add(platform)}>添加{platformNames[platform]}工作区</Button>}</div>;
    })}</div>
    <DeleteConfirmation open={!!deleting} title="删除工作区" subject={deleting ? `${platformNames[deleting]}工作区` : ''}
      consequences={['删除该工作区的采集记录、仿写项目、视频项目、发布稿和关联后台任务记录。', '删除该工作区拥有且未被其他内容引用的本地素材文件，并清除平台登录会话。', '共用 AI 服务和工具路径配置保留。重新添加后会建立空白工作区。']}
      onCancel={() => setDeleting(undefined)} onConfirm={async () => { if (deleting) { const result = await registry.remove(deleting); message.success(deletionSuccess(result, '工作区已删除')); } }} />
  </Card>;
}
