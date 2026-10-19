import React from 'react';
import ReactDOM from 'react-dom/client';
import { App as AntApp, ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import { PortalApp } from './auth';
import './styles/index.css';
import './styles/editorial-theme.css';
import { applyWorkbenchTheme, workbenchTheme } from './theme';

applyWorkbenchTheme();

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ConfigProvider locale={zhCN} theme={workbenchTheme}>
      <AntApp><PortalApp /></AntApp>
    </ConfigProvider>
  </React.StrictMode>,
);
