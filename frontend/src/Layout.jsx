// =============================================================================
// Layout.jsx — App shell with sidebar navigation and content area
// =============================================================================

import React, { useState, useEffect } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import { useAuth } from './contexts/useAuth.js';
import './Layout.css';

const NAV_ITEMS = [
  { to: '/',         label: 'Dashboard',    icon: '\uD83D\uDCCA' },   // bar chart
  { to: '/comms',    label: 'Comms Bridge', icon: '\uD83C\uDF99' },   // microphone
  { to: '/gps',      label: 'GPS / GNSS',   icon: '\uD83D\uDCE1' },   // satellite
  { to: '/blos',     label: 'BLOS',          icon: '\uD83D\uDD17' },   // link
  { to: '/settings', label: 'Settings',     icon: '\u2699\uFE0F' },   // gear
];

export default function Layout() {
  const { logout } = useAuth();
  const [collapsed, setCollapsed] = useState(false);
  const [isMobile, setIsMobile] = useState(window.innerWidth < 768);

  useEffect(() => {
    const onResize = () => setIsMobile(window.innerWidth < 768);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  // Mobile: bottom tab bar
  if (isMobile) {
    return (
      <div className="layout-mobile">
        <div className="layout-content">
          <Outlet />
        </div>
        <nav className="bottom-tab-bar">
          {NAV_ITEMS.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === '/'}
              className={({ isActive }) => 'tab-item' + (isActive ? ' active' : '')}
            >
              <span className="tab-icon">{item.icon}</span>
              <span className="tab-label">{item.label}</span>
            </NavLink>
          ))}
          <button className="tab-item tab-logout" onClick={logout} title="Sign out">
            <span className="tab-icon">🔒</span>
            <span className="tab-label">Sign Out</span>
          </button>
        </nav>
      </div>
    );
  }

  // Desktop: collapsible sidebar
  return (
    <div className="layout-desktop">
      <nav className={'sidebar' + (collapsed ? ' collapsed' : '')}>
        <div className="sidebar-header">
          {!collapsed && (
            <div className="sidebar-brand">
              <div className="brand-title">OpenMANET</div>
              <div className="brand-sub">Mesh Radio WebUI</div>
            </div>
          )}
          <button
            className="sidebar-toggle"
            onClick={() => setCollapsed(!collapsed)}
            title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          >
            {collapsed ? '\u25B6' : '\u25C0'}
          </button>
        </div>
        <div className="sidebar-nav">
          {NAV_ITEMS.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === '/'}
              className={({ isActive }) => 'nav-item' + (isActive ? ' active' : '')}
              title={item.label}
            >
              <span className="nav-icon">{item.icon}</span>
              {!collapsed && <span className="nav-label">{item.label}</span>}
            </NavLink>
          ))}
        </div>
        <div className="sidebar-footer">
          <button className="sidebar-logout" onClick={logout} title="Sign out">
            {collapsed ? '🔒' : '🔒 Sign Out'}
          </button>
        </div>
      </nav>
      <main className="layout-main">
        <Outlet />
      </main>
    </div>
  );
}
