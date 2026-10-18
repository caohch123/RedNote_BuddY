import {
  lazy,
  Suspense,
  useEffect,
  useRef,
  useState,
} from 'react';
import { App as AntApp, Badge, Button, Dropdown, Layout, Menu, Spin, Typography } from 'antd';
import {
  DatabaseOutlined,
  EditOutlined,
  FileDoneOutlined,
  LoadingOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  SettingOutlined,
  SwapOutlined,
  UnorderedListOutlined,
  UserOutlined,
  WalletOutlined,
} from '@ant-design/icons';
const Collections = lazy(() => import('./pages/Collections'));
const Rewrites = lazy(() => import('./pages/Rewrites'));
const Publications = lazy(() => import('./pages/Publications'));
const Settings = lazy(() => import('./pages/AccountSettings'));
const Billing = lazy(() => import('./pages/Billing'));
import JobsDrawer from './components/JobsDrawer';
import { useResource } from './hooks';
import type { Job, List, Platform } from './types';
import { platformNames, workspacePath } from './platform';
import { WorkspaceContext, readWorkspace, saveWorkspace } from './workspace';
import { api } from './api';
import { Failure } from './components/Common';
import WorkspaceManager from './components/WorkspaceManager';
import { WorkspaceRegistryContext, type WorkspaceRecord } from './workspaceRegistry';
import type { DeletionResult } from './deletion';
import { WorkspaceScrollbarHost } from './components/WorkspaceTableScrollbar';
import { WorkspaceActionHost } from './components/WorkspaceActionDock';
import { useAuth } from './auth-context';

type Page = 'collections' | 'rewrites' | 'publications' | 'billing' | 'settings';
const pageNames: Record<Page, string> = {
  collections: '采集库',
  rewrites: '仿写库',
  publications: '发布管理',
  settings: '设置',
  billing: '计费面板',
};
const navigationItems = [
  { key: 'collections', icon: <DatabaseOutlined />, label: '采集库', 'aria-label': '采集库' },
  { key: 'rewrites', icon: <EditOutlined />, label: '仿写库', 'aria-label': '仿写库' },
  { key: 'publications', icon: <FileDoneOutlined />, label: '发布管理', 'aria-label': '发布管理' },
];
const settingsItems = [
  { key: 'billing', icon: <WalletOutlined />, label: '计费面板', 'aria-label': '计费面板' },
  { key: 'settings', icon: <SettingOutlined />, label: '设置', 'aria-label': '设置' },
];

function savedPage(platform: Platform): Page {
  try {
    const value = localStorage.getItem(`workbench.${platform}.page`);
    if (value === 'library') {
      localStorage.setItem(`workbench.${platform}.page`, 'rewrites');
      return 'rewrites';
    }
    if (value && value in pageNames) return value as Page;
  } catch {
    /* The workspace still works without local storage. */
  }
  return 'collections';
}

function savedSidebar(platform: Platform): boolean | undefined {
  try {
    const value = localStorage.getItem(`workbench.${platform}.sidebarCollapsed`);
    if (value === 'true' || value === 'false') return value === 'true';
  } catch {
    /* Keep the sidebar usable without browser storage. */
  }
  return undefined;
}

function Workspace({
  platform,
  active,
  other,
  onSwitch,
}: {
  platform: Platform;
  active: boolean;
  other?: Platform;
  onSwitch: () => void;
}) {
  const { user, logout } = useAuth();
  const { message } = AntApp.useApp();
  const [scrollbarHost, setScrollbarHost] = useState<HTMLDivElement | null>(null);
  const [actionHost, setActionHost] = useState<HTMLDivElement | null>(null);
  const [page, setPage] = useState<Page>(() => savedPage(platform));
  const [visited, setVisited] = useState<Page[]>([page]);
  const [sidebarPreference, setSidebarPreference] = useState<boolean | undefined>(() => savedSidebar(platform));
  const [sidebarNarrow, setSidebarNarrow] = useState(() => window.matchMedia('(max-width: 991.98px)').matches);
  const sidebarCollapsed = sidebarPreference ?? sidebarNarrow;
  const sidebarID = `workspace-sidebar-${platform}`;
  const [rewriteID, setRewriteID] = useState('');
  const [rewriteNavigation, setRewriteNavigation] = useState(0);
  const [publicationID, setPublicationID] = useState('');
  const [jobsOpen, setJobsOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const jobs = useResource<List<Job>>(workspacePath('/jobs', platform), active ? 2000 : 0);
  const activeJobs = jobs.data?.items?.filter(j => j.state === 'running' || j.state === 'queued').length || 0;

  function navigate(value: Page) {
    setPage(value);
    setVisited(v => v.includes(value) ? v : [...v, value]);
    try {
      localStorage.setItem(`workbench.${platform}.page`, value);
    } catch {
      /* Keep navigation in memory. */
    }
  }

  function toggleSidebar() {
    const next = !sidebarCollapsed;
    setSidebarPreference(next);
    try {
      localStorage.setItem(`workbench.${platform}.sidebarCollapsed`, String(next));
    } catch {
      /* Keep the choice in memory. */
    }
  }

  useEffect(() => {
    root.current?.querySelectorAll<HTMLMediaElement>('video, audio').forEach(media => {
      if (!active || media.closest('[hidden]')) media.pause();
    });
    document
      .querySelectorAll<HTMLMediaElement>('.inactive-workspace-portal video, .inactive-workspace-portal audio')
      .forEach(media => media.pause());
  }, [active, page]);
  // Keep visited pages mounted so each workspace retains its filters and drafts.
  return (
    <WorkspaceContext.Provider value={{ platform, active }}>
      <WorkspaceScrollbarHost.Provider value={scrollbarHost}>
        <WorkspaceActionHost.Provider value={actionHost}>
          <div ref={root} hidden={!active} data-workspace={platform}>
            <Layout className={`app-shell workspace-${platform}`}>
              <Layout.Sider
                id={sidebarID}
                width={224}
                theme="light"
                className="sidebar"
                breakpoint="lg"
                onBreakpoint={setSidebarNarrow}
                collapsible
                collapsed={sidebarCollapsed}
                collapsedWidth={68}
                trigger={null}
              >
                <div className="brand">
                  <img src="/workbench-logo.png" alt="工作台标识" />
                  <div>
                    <strong>{platformNames[platform]}工作区</strong>
                    <small>XIAOHONGSHU WORKSPACE</small>
                  </div>
                </div>
                <Menu
                  mode="inline"
                  selectedKeys={[page]}
                  onClick={({ key }) => navigate(key as Page)}
                  items={navigationItems}
                />
                <div className="sidebar-bottom">
                  <div className="workspace-label">
                    <span className="online-dot" />
                    {platformNames[platform]} · {user.display_name || user.username}的工作区
                  </div>
                  <Menu
                    mode="inline"
                    selectedKeys={[page]}
                    onClick={({ key }) => navigate(key as Page)}
                    items={settingsItems}
                  />
                  <div className="sidebar-toggle">
                    <Button
                      type="text"
                      block
                      aria-label={sidebarCollapsed ? '展开侧边栏' : '收起侧边栏'}
                      title={sidebarCollapsed ? '展开侧边栏' : '收起侧边栏'}
                      aria-expanded={!sidebarCollapsed}
                      aria-controls={sidebarID}
                      icon={sidebarCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                      onClick={toggleSidebar}
                    >
                      {!sidebarCollapsed && '收起侧边栏'}
                    </Button>
                  </div>
                  <Typography.Text type="secondary" className="sidebar-version">
                    第一阶段 · v1.0
                  </Typography.Text>
                </div>
              </Layout.Sider>
              {!sidebarCollapsed && (
                <button
                  className="sidebar-scrim"
                  aria-label="关闭侧边栏遮罩"
                  onClick={toggleSidebar}
                />
              )}
              <Layout>
                <Layout.Header className="topbar">
                  <div className="breadcrumb">
                    <span className={`workspace-indicator ${platform}`} />
                    <span className="current-workspace">{platformNames[platform]}工作区</span>
                    <span>/</span>
                    <strong>{pageNames[page]}</strong>
                  </div>
                  <span className="topbar-brand" aria-label="RedNote"><span>RedNote</span></span>
                  <div className="topbar-actions">
                    <Button
                      className="workspace-switch"
                      aria-label={other ? `切换到${platformNames[other]}工作区` : '切换工作区'}
                      disabled={!other}
                      icon={<SwapOutlined />}
                      onClick={onSwitch}
                    >
                      {other ? `切换到${platformNames[other]}` : '切换工作区'}
                    </Button>
                    <Badge count={activeJobs} size="small">
                      <Button
                        icon={activeJobs ? <LoadingOutlined /> : <UnorderedListOutlined />}
                        onClick={() => setJobsOpen(true)}
                      >
                        后台任务
                      </Button>
                    </Badge>
                    <Dropdown trigger={['click']} menu={{
                      items: [
                        { key: 'account', icon: <UserOutlined />, label: '账户设置' },
                        { type: 'divider' },
                        { key: 'logout', icon: <LogoutOutlined />, label: '退出登录' },
                      ],
                      onClick: ({ key }) => {
                        if (key === 'account') navigate('settings');
                        if (key === 'logout') void logout().catch(reason => message.error(reason instanceof Error ? reason.message : '退出登录失败'));
                      },
                    }}>
                      <Button className="workspace-account-button" icon={<UserOutlined />} aria-label="工作台账户菜单" title={user.display_name || user.username}><span className="workspace-account-name">{user.display_name || user.username}</span></Button>
                    </Dropdown>
                  </div>
                </Layout.Header>
                <Layout.Content
                  className={`main-content${page === 'rewrites' ? ' main-content-rewrites' : ''}`}
                >
                  <Suspense fallback={<div className="empty-panel"><Spin /></div>}>
                    {visited.includes('collections') && (
                      <WorkspaceContext.Provider
                        value={{ platform, active: active && page === 'collections' }}
                      >
                        <div hidden={page !== 'collections'}>
                          <Collections
                            onRewrite={id => {
                              setRewriteID(id);
                              setRewriteNavigation(value => value + 1);
                              navigate('rewrites');
                            }}
                          />
                        </div>
                      </WorkspaceContext.Provider>
                    )}
                    {visited.includes('rewrites') && (
                      <WorkspaceContext.Provider
                        value={{ platform, active: active && page === 'rewrites' }}
                      >
                        <div hidden={page !== 'rewrites'}>
                          <Rewrites
                            initialID={rewriteID}
                            navigation={rewriteNavigation}
                            jobs={jobs.data?.items || []}
                            onPublication={id => {
                              setPublicationID(id);
                              navigate('publications');
                            }}
                          />
                        </div>
                      </WorkspaceContext.Provider>
                    )}
                    {visited.includes('publications') && (
                      <WorkspaceContext.Provider
                        value={{ platform, active: active && page === 'publications' }}
                      >
                        <div hidden={page !== 'publications'}>
                          <Publications initialID={publicationID} />
                        </div>
                      </WorkspaceContext.Provider>
                    )}
                    {visited.includes('billing') && (
                      <WorkspaceContext.Provider value={{ platform, active: active && page === 'billing' }}>
                        <div hidden={page !== 'billing'}><Billing /></div>
                      </WorkspaceContext.Provider>
                    )}
                    {visited.includes('settings') && (
                      <WorkspaceContext.Provider
                        value={{ platform, active: active && page === 'settings' }}
                      >
                        <div hidden={page !== 'settings'}>
                          <Settings />
                        </div>
                      </WorkspaceContext.Provider>
                    )}
                  </Suspense>
                </Layout.Content>
                <div ref={setActionHost} className="workspace-action-slot" />
                <div ref={setScrollbarHost} className="workspace-scrollbar-slot" />
              </Layout>
            </Layout>
            <JobsDrawer
              open={jobsOpen}
              onClose={() => setJobsOpen(false)}
              jobs={jobs.data?.items || []}
              error={jobs.error}
              refresh={() => void jobs.reload(true)}
            />
          </div>
        </WorkspaceActionHost.Provider>
      </WorkspaceScrollbarHost.Provider>
    </WorkspaceContext.Provider>
  );
}

export default function App() {
  const { user, logout } = useAuth();
  const { message } = AntApp.useApp();
  const emptyAccountActions = <div className="empty-account-actions"><Typography.Text type="secondary">{user.display_name || user.username}</Typography.Text><Button type="text" icon={<LogoutOutlined />} onClick={() => void logout().catch(reason => message.error(reason instanceof Error ? reason.message : '退出登录失败'))}>退出登录</Button></div>;
  const [platform, setPlatform] = useState<Platform>(readWorkspace);
  const [visited, setVisited] = useState<Platform[]>([]);
  const registry = useResource<List<WorkspaceRecord>>('/workspaces', 5000);
  const [registered, setRegistered] = useState<WorkspaceRecord[]>();
  const items = registered || registry.data?.items || [];
  useEffect(() => {
    if (registry.data) setRegistered(registry.data.items);
  }, [registry.data]);
  const available = items.map(item => item.platform);
  const other = available.find(value => value !== platform);

  useEffect(() => {
    if (!registry.data) return;
    setVisited(values => values.filter(value => available.includes(value)));
    const next = available.includes(platform) ? platform : available[0];
    if (next) {
      setPlatform(next);
      saveWorkspace();
      setVisited(values => values.includes(next) ? values : [...values, next]);
    }
  }, [registered, registry.data, platform]);

  function switchWorkspace() {
    if (!other) return;
    setVisited(v => v.includes(other) ? v : [...v, other]);
    setPlatform(other);
    saveWorkspace();
  }

  async function addWorkspace(value: Platform) {
    const item = await api<WorkspaceRecord>('/workspaces', 'POST', { platform: value });
    setRegistered(values => [...(values || []).filter(existing => existing.platform !== value), item]);
    await registry.reload(true);
  }

  async function removeWorkspace(value: Platform) {
    const deleted = await api<DeletionResult>(`/workspaces/${value}`, 'DELETE', { confirm: true });
    // Purge editors and portals immediately, before the registry refresh completes.
    setVisited(values => values.filter(item => item !== value));
    setRegistered(values => (values || []).filter(item => item.platform !== value));
    for (const key of [
      `workbench.${value}.page`,
      `workbench.${value}.columns`,
      `workbench.${value}.sidebarCollapsed`,
    ]) {
      try {
        localStorage.removeItem(key);
      } catch {
        /* Keep deletion usable without browser storage. */
      }
    }
    await registry.reload(true);
    return deleted;
  }

  useEffect(() => {
    document.title = `${platformNames[platform]}内容工作台`;
  }, [platform]);
  // Each workspace retains its own navigation and unfinished editor state.
  return (
    <WorkspaceRegistryContext.Provider value={{ items, add: addWorkspace, remove: removeWorkspace }}>
      {!registry.data ? (
        <div className="workspace-empty">
          {emptyAccountActions}
          <Typography.Title level={3}>内容工作台</Typography.Title>
          <Failure error={registry.error} retry={() => void registry.reload()} />
          {registry.loading && <Spin />}
        </div>
      ) : !items.length ? (
        <div className="workspace-empty">
          {emptyAccountActions}
          <Typography.Title level={3}>添加你的第一个工作区</Typography.Title>
          <WorkspaceManager empty />
        </div>
      ) : (
        visited
          .filter(value => available.includes(value))
          .map(value => (
            <Workspace
              key={value}
              platform={value}
              active={platform === value}
              other={available.find(item => item !== value)}
              onSwitch={switchWorkspace}
            />
          ))
      )}
    </WorkspaceRegistryContext.Provider>
  );
}
