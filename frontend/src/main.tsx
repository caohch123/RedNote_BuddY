import React from 'react';
import ReactDOM from 'react-dom/client';
import { App as AntApp, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import App from './App';
import './styles/index.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ConfigProvider locale={zhCN} theme={{ token: {
      colorPrimary: '#2563eb', colorBgLayout: '#f7f8fc', colorText: '#25314c',
      colorBorder: '#e4e8f0', borderRadius: 9, fontFamily: 'Inter, "Microsoft YaHei", "PingFang SC", sans-serif',
    }, components: { Table: { headerBg: '#fafbfe' }, Menu: { itemSelectedBg: '#eef3ff', itemSelectedColor: '#2563eb' } } }}>
      <AntApp><App /></AntApp>
    </ConfigProvider>
  </React.StrictMode>,
);
