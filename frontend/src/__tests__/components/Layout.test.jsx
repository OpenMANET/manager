// =============================================================================
// Layout.test.jsx — Tests for responsive app shell layout
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Layout from '../../Layout.jsx';

afterEach(() => {
  cleanup();
});

function renderLayout(initialWidth = 1024) {
  Object.defineProperty(window, 'innerWidth', { value: initialWidth, writable: true });
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Layout />
    </MemoryRouter>
  );
}

describe('TestLayoutDesktop', () => {
  it('renders sidebar with brand on wide viewport', () => {
    const { container } = renderLayout(1024);
    expect(container.querySelector('.sidebar')).toBeTruthy();
    expect(screen.getByText('OpenMANET')).toBeTruthy();
    expect(screen.getByText('Comms Bridge')).toBeTruthy();
    expect(screen.getByText('Settings')).toBeTruthy();
  });

  it('renders version footer', () => {
    renderLayout(1024);
    expect(screen.getByText('OpenMANET v1.0')).toBeTruthy();
  });
});

describe('TestLayoutMobile', () => {
  it('renders bottom tab bar on narrow viewport', () => {
    const { container } = renderLayout(500);
    expect(container.querySelector('.bottom-tab-bar')).toBeTruthy();
    expect(container.querySelector('.sidebar')).toBeNull();
    expect(screen.getByText('Comms Bridge')).toBeTruthy();
    expect(screen.getByText('Settings')).toBeTruthy();
  });
});

describe('TestLayoutSidebarCollapse', () => {
  it('collapses and expands sidebar', () => {
    const { container } = renderLayout(1024);
    const sidebar = container.querySelector('.sidebar');
    expect(sidebar.classList.contains('collapsed')).toBe(false);

    // Collapse
    const toggleBtn = container.querySelector('.sidebar-toggle');
    fireEvent.click(toggleBtn);
    expect(sidebar.classList.contains('collapsed')).toBe(true);
    // Brand should be hidden
    expect(screen.queryByText('OpenMANET')).toBeNull();

    // Expand
    fireEvent.click(toggleBtn);
    expect(sidebar.classList.contains('collapsed')).toBe(false);
    expect(screen.getByText('OpenMANET')).toBeTruthy();
  });
});

describe('TestLayoutResize', () => {
  it('switches from desktop to mobile on resize', () => {
    const { container } = renderLayout(1024);
    expect(container.querySelector('.sidebar')).toBeTruthy();

    act(() => {
      window.innerWidth = 500;
      window.dispatchEvent(new Event('resize'));
    });

    expect(container.querySelector('.bottom-tab-bar')).toBeTruthy();
    expect(container.querySelector('.sidebar')).toBeNull();
  });

  it('switches from mobile to desktop on resize', () => {
    const { container } = renderLayout(500);
    expect(container.querySelector('.bottom-tab-bar')).toBeTruthy();

    act(() => {
      window.innerWidth = 1024;
      window.dispatchEvent(new Event('resize'));
    });

    expect(container.querySelector('.sidebar')).toBeTruthy();
    expect(container.querySelector('.bottom-tab-bar')).toBeNull();
  });
});

describe('TestLayoutNavLinks', () => {
  it('has correct navigation paths', () => {
    const { container } = renderLayout(1024);
    const links = container.querySelectorAll('a');
    const hrefs = Array.from(links).map(a => a.getAttribute('href'));
    expect(hrefs).toContain('/');
    expect(hrefs).toContain('/settings');
  });
});
