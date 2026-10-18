import { useEffect, useMemo, useRef, useState } from 'react';
import { App, Button, Descriptions, Empty, InputNumber, Modal, Segmented, Select, Spin, Table, Tag, Tooltip } from 'antd';
import { ArrowDownOutlined, ArrowUpOutlined, DownloadOutlined, ReloadOutlined, SettingOutlined, WalletOutlined } from '@ant-design/icons';
import { api, download, errorText } from '../api';
import { useResource } from '../hooks';
import { useWorkspace } from '../workspace';
import { workspacePath } from '../platform';
import { Failure } from '../components/Common';
import { billingCapabilities, billingOperations, billingQuery, costNumber, tokenNumber, trendPoints } from '../billing';
import type { BillingData, BillingPeriod, TokenPrice, TrendPoint, UsageGroup, UsageRecord } from '../billing';

const stateLabels: Record<string, { name: string; color?: string }> = {
  success: { name: '接口成功', color: 'green' }, failed: { name: '接口失败', color: 'red' },
  pending: { name: '生成中', color: 'blue' }, unknown: { name: '结果待核验', color: 'gold' },
};

function TrendChart({ points, reported }: { points: TrendPoint[]; reported: number }) {
  const [selected, setSelected] = useState<number>();
  const container = useRef<HTMLDivElement>(null); const [chartWidth, setChartWidth] = useState(760);
  useEffect(() => {
    if (!container.current) return;
    const observer = new ResizeObserver(entries => { const width = entries[0]?.contentRect.width; if (width) setChartWidth(Math.max(280, Math.min(760, width))); });
    observer.observe(container.current); return () => observer.disconnect();
  }, [reported]);
  const max = Math.max(1, ...points.map(p => p.input + p.output + p.other));
  const width = chartWidth, height = 208, left = 55, plot = width - left - 14, bottom = 170;
  const step = plot / Math.max(1, points.length), bar = Math.min(20, step * .57);
  const selectedPoint = selected == null ? undefined : points[selected];
  if (!reported) return <div className="billing-chart-empty"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无服务商返回的 Token 用量" /></div>;
  return <>
    <div className="billing-chart-scroll" ref={container}>
      <svg className="billing-trend" viewBox={`0 0 ${width} ${height}`} aria-label="Token 用量趋势图，蓝色为输入，青色为输出，灰色为未拆分用量" role="img">
        {[0, .5, 1].map(ratio => <g key={ratio}>
          <line x1={left} x2={width - 14} y1={bottom - ratio * 140} y2={bottom - ratio * 140} stroke="#e9edf5" strokeDasharray={ratio ? '4 5' : undefined} />
          <text x={left - 10} y={bottom - ratio * 140 + 4} textAnchor="end" fill="#8a95a9" fontSize="11">{new Intl.NumberFormat('zh-CN', { notation: 'compact', maximumFractionDigits: 1 }).format(max * ratio)}</text>
        </g>)}
        {points.map((p, i) => {
          const x = left + step * i + step / 2 - bar / 2;
          const input = p.input / max * 140, output = p.output / max * 140, other = p.other / max * 140;
          const description = `${p.label}：输入 ${tokenNumber(p.input)}，输出 ${tokenNumber(p.output)}，未拆分 ${tokenNumber(p.other)} Token`;
          return <g key={p.key} role="button" tabIndex={0} aria-label={description} onFocus={() => setSelected(i)} onMouseEnter={() => setSelected(i)} onClick={() => setSelected(i)} onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setSelected(i); } }}>
            <title>{description}</title>
            <rect x={x - 5} y={22} width={bar + 10} height={150} fill={selected === i ? '#f0f4ff' : 'transparent'} rx="4" />
            <rect x={x} y={bottom - input} width={bar} height={input} fill="#4777f5" rx="2" />
            <rect x={x} y={bottom - input - output} width={bar} height={output} fill="#43b9b0" rx="2" />
            <rect x={x} y={bottom - input - output - other} width={bar} height={other} fill="#b3bed2" rx="2" />
            {(i % Math.max(1, Math.ceil(points.length / (width < 450 ? 4 : 7))) === 0 || i === points.length - 1) && <text x={x + bar / 2} y={194} textAnchor="middle" fill="#8a95a9" fontSize="11">{p.label}</text>}
          </g>;
        })}
      </svg>
    </div>
    <div className="billing-chart-detail" aria-live="polite">{selectedPoint ? `${selectedPoint.label} · 输入 ${tokenNumber(selectedPoint.input)} / 输出 ${tokenNumber(selectedPoint.output)} / 未拆分 ${tokenNumber(selectedPoint.other)}` : '悬停或选中柱形，查看具体用量'}</div>
  </>;
}

function Distribution({ rows, dimension, onDimension, admin = false }: { rows: UsageGroup[]; dimension: string; onDimension: (v: string) => void; admin?: boolean }) {
  const total = rows.reduce((n, row) => n + row.total_tokens, 0);
  return <section className="billing-panel billing-distribution">
    <div className="billing-panel-heading"><h3>用量分布</h3><Segmented size="small" value={dimension} options={[{ label: '模型', value: 'models' }, { label: '功能', value: 'capabilities' }, ...(admin ? [{ label: '用户', value: 'users' }] : [])]} onChange={onDimension} /></div>
    {!rows.length ? <div className="billing-chart-empty"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无调用记录" /></div> : <div className="billing-distribution-list">{rows.map((row, index) => <div key={row.key} className="billing-distribution-row">
      <div><Tooltip title={row.key}><strong>{dimension === 'capabilities' ? billingCapabilities[row.key] || row.key : row.key}</strong></Tooltip><span>{tokenNumber(row.reported ? row.total_tokens : null)} <small>Token</small></span></div>
      <div className="billing-progress"><span style={{ width: `${total ? row.total_tokens / total * 100 : 0}%`, background: ['#4777f5', '#43b9b0', '#9b8bd9', '#eca06c'][index % 4] }} /></div>
      <small>{row.requests} 次调用 · {row.missing ? `${row.missing} 次未返回 Token` : '用量已返回'}</small>
    </div>)}</div>}
  </section>;
}

function PricesModal({ data, open, close, saved, admin }: { data: BillingData; open: boolean; close: () => void; saved: () => void; admin: boolean }) {
  const { message } = App.useApp();
  const [selection, setSelection] = useState(''); const [input, setInput] = useState<number | null>(null); const [output, setOutput] = useState<number | null>(null); const [cache, setCache] = useState<number | null>(null); const [busy, setBusy] = useState(false);
  const options = data.model_options.map(o => ({ value: JSON.stringify([o.provider, o.model]), label: `${o.model} · ${new URL(o.provider).host}` }));
  useEffect(() => {
    const option = data.model_options.find(o => JSON.stringify([o.provider, o.model]) === selection);
    const price = data.prices.find(p => p.model === option?.model && p.provider === option.provider);
    setInput(price?.input_per_million ?? null); setOutput(price?.output_per_million ?? null); setCache(price?.cached_per_million ?? null);
  }, [selection, open]);
  async function save(remove = false) {
    if (!selection) return;
    const [provider, model] = JSON.parse(selection) as [string, string];
    setBusy(true);
    try {
      if (!admin) return;
      const latest = await api<BillingData>('/admin/billing');
      const items: TokenPrice[] = latest.prices.filter(p => p.provider !== provider || p.model !== model);
      if (!remove && input != null && output != null) items.push({ provider, model, input_per_million: input, output_per_million: output, ...(cache == null ? {} : { cached_per_million: cache }) });
      await api('/admin/billing/prices', 'PUT', { items });
      message.success(remove ? '已移除单价，历史估算保持不变' : '已保存单价，将用于后续调用的费用估算'); saved(); close();
    } catch (e) { message.error(errorText(e)); } finally { setBusy(false); }
  }
  return <Modal title="Token 单价设置" open={open} onCancel={close} onOk={() => void save()} confirmLoading={busy} okButtonProps={{ disabled: !selection || input == null || output == null }} okText="保存单价" cancelText="取消" className="billing-prices-modal">
    <p className="billing-muted">按你实际使用的服务价格填写，单位：元 / 百万 Token。仅估算后续调用，历史记录保留当时单价。</p>
    <label className="billing-price-field">模型与服务来源<Select aria-label="计费模型" value={selection || undefined} placeholder="选择模型" options={options} onChange={setSelection} showSearch optionFilterProp="label" /></label>
    <div className="billing-price-grid">
      <label className="billing-price-field">输入单价<InputNumber aria-label="输入单价" min={0} max={1e9} value={input} onChange={setInput} placeholder="未配置" /></label>
      <label className="billing-price-field">输出单价<InputNumber aria-label="输出单价" min={0} max={1e9} value={output} onChange={setOutput} placeholder="未配置" /></label>
    </div>
    <label className="billing-price-field">缓存输入单价（可选）<InputNumber aria-label="缓存输入单价" min={0} max={1e9} value={cache} onChange={setCache} placeholder="留空则按普通输入单价估算" /></label>
    <p className="billing-muted">缓存属于输入 Token，推理属于输出 Token，不额外累加。图片、视频等按张或时长计费的用量单独展示，不换算为 Token 费用。</p>
    <Button type="text" danger disabled={!selection || busy || !data.prices.some(p => JSON.stringify([p.provider, p.model]) === selection)} onClick={() => void save(true)}>移除该模型单价</Button>
  </Modal>;
}

export default function Billing({ admin = false }: { admin?: boolean } = {}) {
  const { platform, active } = useWorkspace(); const { message } = App.useApp();
  const [period, setPeriod] = useState<BillingPeriod>('7d'); const [model, setModel] = useState(''); const [capability, setCapability] = useState(''); const [page, setPage] = useState(1); const [dimension, setDimension] = useState('models'); const [pricesOpen, setPricesOpen] = useState(false); const [exporting, setExporting] = useState(false);
  const [clock, setClock] = useState(() => new Date());
  const [userID, setUserID] = useState('');
  const scoped = (path: string) => admin ? `/admin${path}` : workspacePath(path, platform);
  useEffect(() => { if (!active) return; setClock(new Date()); const timer = setInterval(() => setClock(new Date()), 60000); return () => clearInterval(timer); }, [active]);
  const query = billingQuery(period, model, capability, page, clock, admin ? userID : '');
  const result = useResource<BillingData>(scoped(`/billing?${query}`), active ? 15000 : 0);
  const data = result.data; const points = useMemo(() => trendPoints(data?.timeline || [], period, clock), [data?.timeline, period, clock]);
  useEffect(() => { if (active) void result.reload(true); }, [active, result.reload]);
  async function exportData() {
    setExporting(true);
    try { await download(scoped(`/billing/export?${query}`), 'token-usage.csv'); } catch (e) { message.error(errorText(e)); } finally { setExporting(false); }
  }
  const summary = data?.summary;
  return <div className="billing-page">
    <div className="billing-heading"><div className="billing-heading-copy"><span className="billing-heading-icon"><WalletOutlined /></span><div><h2>计费面板</h2><p>每一次创作，用量清晰可见</p></div></div><div className="billing-heading-actions">
      {admin && <Button aria-label="单价设置" icon={<SettingOutlined />} onClick={() => setPricesOpen(true)} disabled={!data}>单价设置</Button>}
      <Button aria-label="导出明细" icon={<DownloadOutlined />} loading={exporting} disabled={!data?.total} onClick={() => void exportData()}>导出明细</Button>
      <Tooltip title="刷新用量"><Button aria-label="刷新用量" icon={<ReloadOutlined />} loading={result.loading} onClick={() => void result.reload()} /></Tooltip>
    </div></div>
    <div className="billing-filters">
      <Segmented aria-label="统计时间" value={period} options={[{ label: '今天', value: 'today' }, { label: '近 7 天', value: '7d' }, { label: '近 30 天', value: '30d' }, { label: '全部', value: 'all' }]} onChange={v => { setPeriod(v as BillingPeriod); setPage(1); }} />
      <div className="billing-filter-selects">{admin && <Select aria-label="筛选用户" value={userID || undefined} placeholder="全部用户" allowClear showSearch optionFilterProp="label" options={(data?.user_options || []).map(u => ({ value: u.id, label: `${u.display_name || u.username} · ${u.username}` }))} onChange={v => { setUserID(v || ''); setPage(1); }} />}<Select aria-label="筛选模型" value={model || undefined} placeholder="全部模型" allowClear showSearch optionFilterProp="label" options={[...new Set(data?.model_options.map(o => o.model) || [])].map(v => ({ value: v, label: v }))} onChange={v => { setModel(v || ''); setPage(1); }} />
      <Select aria-label="筛选调用类型" value={capability || undefined} placeholder="全部调用类型" allowClear options={Object.entries(billingCapabilities).map(([value, label]) => ({ value, label }))} onChange={v => { setCapability(v || ''); setPage(1); }} /></div>
    </div>
    <Failure error={result.error} retry={() => void result.reload()} />
    {!data ? result.loading && <div className="billing-loading"><Spin /></div> : <>
      <div className="billing-summary-context"><span><strong>{summary!.requests}</strong> 次模型调用{summary!.missing > 0 && <span> · {summary!.missing} 次未返回 Token</span>}</span><span>按本地时间统计</span></div>
      <div className="billing-metrics">
        <div className="billing-metric billing-metric-primary"><span>总 Token</span><strong>{tokenNumber(summary!.reported || !summary!.requests ? summary!.total_tokens : null)}</strong><small>服务商已报告 {summary!.reported} / {summary!.requests} 次</small></div>
        <div className="billing-metric"><span><ArrowUpOutlined /> 输入 Token</span><strong>{tokenNumber(summary!.input_reported || !summary!.requests ? summary!.input_tokens : null)}</strong><small>{summary!.cached_reported ? `已报告缓存 ${tokenNumber(summary!.cached_tokens)}（含于输入）` : '未返回缓存明细'}</small></div>
        <div className="billing-metric"><span><ArrowDownOutlined /> 输出 Token</span><strong>{tokenNumber(summary!.output_reported || !summary!.requests ? summary!.output_tokens : null)}</strong><small>{summary!.reasoning_reported ? `已报告推理 ${tokenNumber(summary!.reasoning_tokens)}（含于输出）` : '未返回推理明细'}</small></div>
        <div className="billing-metric"><span>估算费用 <small>CNY</small></span><strong className="billing-money">{summary!.priced ? costNumber(summary!.estimated_cny) : '未估算'}</strong><small>{summary!.priced ? `已估算 ${summary!.priced} / ${summary!.requests} 次调用` : '配置单价后估算新调用费用'}</small></div>
      </div>
      <div className="billing-charts">
        <section className="billing-panel"><div className="billing-panel-heading"><div><h3>Token 用量趋势</h3><p>{data.hourly ? '每小时' : '按日期'}查看输入与输出</p></div><div className="billing-legend"><span><i />输入</span><span><i />输出</span><span><i />未拆分</span></div></div><TrendChart points={points} reported={summary!.reported} /></section>
        <Distribution rows={dimension === 'models' ? data.models : dimension === 'users' ? data.users || [] : data.capabilities} dimension={dimension} onDimension={setDimension} admin={admin} />
      </div>
      {(summary!.images > 0 || summary!.video_seconds > 0 || summary!.failed > 0 || summary!.pending > 0 || summary!.unknown > 0) && <div className="billing-secondary-stats">
        {summary!.images > 0 && <span>图片生成 <strong>{summary!.images} 张</strong></span>}{summary!.video_seconds > 0 && <span>视频生成 <strong>{summary!.video_seconds} 秒</strong></span>}
        {summary!.failed > 0 && <span>接口失败 <strong>{summary!.failed} 次</strong></span>}{summary!.pending > 0 && <span>生成中 <strong>{summary!.pending} 次</strong></span>}{summary!.unknown > 0 && <span>结果待核验 <strong>{summary!.unknown} 次</strong></span>}
      </div>}
      <section className="billing-panel billing-records"><div className="billing-panel-heading"><div><h3>调用明细</h3><p>展开记录查看缓存、推理与计量详情</p></div><span className="billing-muted">{data.total} 条记录</span></div>
        <Table<UsageRecord> rowKey="id" size="middle" dataSource={data.items} scroll={{ x: 1000 }} loading={result.loading} pagination={{ current: page, pageSize: 20, total: data.total, showSizeChanger: false, onChange: setPage, hideOnSinglePage: true }} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无调用记录，下一次创作后将在这里显示" /> }}
          columns={[
            ...(admin ? [{ title: '用户', dataIndex: 'username', width: 130, render: (value: string) => value || '历史记录' }] : []),
            { title: '调用时间', dataIndex: 'created_at', width: 158, render: value => new Date(value).toLocaleString('zh-CN', { hour12: false }) },
            { title: '功能 / 模型', key: 'model', width: 215, render: (_, r) => <div className="billing-model-cell"><strong>{billingOperations[r.operation] || billingCapabilities[r.capability] || r.capability}</strong><span title={r.model}>{r.model}</span></div> },
            { title: '输入', dataIndex: 'input_tokens', align: 'right', render: tokenNumber }, { title: '输出', dataIndex: 'output_tokens', align: 'right', render: tokenNumber },
            { title: '总 Token', dataIndex: 'total_tokens', align: 'right', render: (value: number | null) => <strong>{tokenNumber(value)}</strong> },
            { title: '估算费用', dataIndex: 'estimated_cny', align: 'right', render: (value: number | null) => value == null ? <span className="billing-muted">未估算</span> : costNumber(value) },
            { title: '状态', dataIndex: 'status', width: 115, render: (value: string) => <Tag color={stateLabels[value]?.color}>{stateLabels[value]?.name || value}</Tag> },
          ]}
          expandable={{ expandedRowRender: r => <Descriptions size="small" column={{ xs: 1, sm: 2, lg: 3 }} items={[
            { key: 'provider', label: '服务来源', children: r.provider }, { key: 'credential', label: '凭据标识', children: r.credential_id },
            { key: 'cache', label: '缓存（含于输入）', children: tokenNumber(r.cached_tokens) }, { key: 'reason', label: '推理（含于输出）', children: tokenNumber(r.reasoning_tokens) },
            { key: 'image', label: '图片生成', children: r.image_count == null ? '—' : `${r.image_count} 张` }, { key: 'video', label: '视频生成', children: r.video_seconds == null ? '—' : `${r.video_seconds} 秒` },
            { key: 'usage', label: '统计口径', children: r.total_tokens == null ? (['image', 'video', 'asr'].includes(r.capability) ? '未返回 Token，按媒体计量' : '服务未返回完整用量') : '服务响应中的 usage 用量' },
            { key: 'price', label: '单价快照', children: r.price ? `输入 ${r.price.input_per_million} / 输出 ${r.price.output_per_million} 元/百万 Token${r.price.cached_per_million == null ? '' : `，缓存 ${r.price.cached_per_million}`}` : '调用时未设置单价' },
            { key: 'job', label: '任务编号', children: r.job_id || '—' },
          ]} /> }} />
      </section>
      <p className="billing-footnote">从 {new Date(data.started_at).toLocaleString('zh-CN', { hour12: false })} 开始记录，此前用量不补算。未返回的用量显示“—”；费用按管理员配置单价估算，实际以服务商账单为准。</p>
      {admin && <PricesModal data={data} open={pricesOpen} close={() => setPricesOpen(false)} saved={() => void result.reload(true)} admin={admin} />}
    </>}
  </div>;
}
