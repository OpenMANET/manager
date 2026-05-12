import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import SettingsLayout from '../pages/SettingsLayout.jsx';

function renderAt(path) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/settings" element={<SettingsLayout />}>
          <Route index element={<div>GENERAL_BODY</div>} />
          <Route path="terminal" element={<div>TERMINAL_BODY</div>} />
          <Route path="logs" element={<div>LOGS_BODY</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe('SettingsLayout', () => {
  it('renders all tabs and the General body on /settings', () => {
    renderAt('/settings');
    expect(screen.getByRole('tab', { name: /general/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /terminal/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /logs/i })).toBeInTheDocument();
    expect(screen.getByText('GENERAL_BODY')).toBeInTheDocument();
  });

  it('renders the Terminal body on /settings/terminal', () => {
    renderAt('/settings/terminal');
    expect(screen.getByText('TERMINAL_BODY')).toBeInTheDocument();
  });

  it('renders the Logs body on /settings/logs', () => {
    renderAt('/settings/logs');
    expect(screen.getByText('LOGS_BODY')).toBeInTheDocument();
  });

  it('marks the active tab with aria-selected=true', () => {
    renderAt('/settings/terminal');
    const terminalTab = screen.getByRole('tab', { name: /terminal/i });
    const generalTab = screen.getByRole('tab', { name: /general/i });
    expect(terminalTab).toHaveAttribute('aria-selected', 'true');
    expect(generalTab).toHaveAttribute('aria-selected', 'false');
  });

  it('marks the Logs tab active on /settings/logs', () => {
    renderAt('/settings/logs');
    const logsTab = screen.getByRole('tab', { name: /logs/i });
    expect(logsTab).toHaveAttribute('aria-selected', 'true');
  });
});
