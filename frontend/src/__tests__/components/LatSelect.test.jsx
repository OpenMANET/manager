// =============================================================================
// LatSelect.test.jsx — Custom Lattice-themed dropdown
// =============================================================================
//
// LatSelect replaces native <select> across the frontend so the popup matches
// the Lattice dark theme. These tests pin the contract every consumer relies
// on: trigger renders selected label, popup opens/closes on click, options
// fire onChange with the option's value (not its label), keyboard works,
// click-outside closes, and ARIA attributes are correctly set so screen
// readers can navigate.

import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';

import LatSelect from '../../components/LatSelect.jsx';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const OPTIONS = [
  { value: 1, label: 'One' },
  { value: 2, label: 'Two' },
  { value: 3, label: 'Three' },
];

function renderSelect(overrides = {}) {
  const onChange = vi.fn();
  const props = {
    value: 1,
    onChange,
    options: OPTIONS,
    ariaLabel: 'Pick',
    ...overrides,
  };
  const result = render(<LatSelect {...props} />);
  return { ...result, onChange };
}

describe('TestLatSelect_Closed', () => {
  it('renders a button with the selected label and the placeholder caret', () => {
    const { container } = renderSelect();
    const btn = screen.getByRole('button', { name: 'Pick' });
    expect(btn.textContent).toContain('One');
    expect(btn.getAttribute('aria-expanded')).toBe('false');
    expect(btn.getAttribute('aria-haspopup')).toBe('listbox');
    // No popup until opened.
    expect(container.querySelector('[role="listbox"]')).toBeNull();
  });

  it('falls back to placeholder when value matches no option', () => {
    renderSelect({ value: 99, placeholder: 'choose…' });
    const btn = screen.getByRole('button', { name: 'Pick' });
    expect(btn.textContent).toContain('choose…');
  });

  it('renders disabled when disabled', () => {
    renderSelect({ disabled: true });
    const btn = screen.getByRole('button', { name: 'Pick' });
    expect(btn.disabled).toBe(true);
  });
});

describe('TestLatSelect_OpenAndChoose', () => {
  it('opens the listbox on click and shows all options', () => {
    renderSelect();
    fireEvent.click(screen.getByRole('button', { name: 'Pick' }));

    expect(screen.getByRole('listbox')).toBeTruthy();
    expect(screen.getAllByRole('option')).toHaveLength(3);
  });

  it('marks the current value as aria-selected', () => {
    renderSelect({ value: 2 });
    fireEvent.click(screen.getByRole('button', { name: 'Pick' }));

    const opts = screen.getAllByRole('option');
    expect(opts[0].getAttribute('aria-selected')).toBe('false');
    expect(opts[1].getAttribute('aria-selected')).toBe('true');
    expect(opts[2].getAttribute('aria-selected')).toBe('false');
  });

  it('fires onChange with the option value (not the label) and closes', () => {
    const { onChange, container } = renderSelect();
    fireEvent.click(screen.getByRole('button', { name: 'Pick' }));
    fireEvent.click(screen.getByRole('option', { name: 'Three' }));

    expect(onChange).toHaveBeenCalledWith(3);
    expect(container.querySelector('[role="listbox"]')).toBeNull();
  });

  it('toggles closed if the trigger is clicked while open', () => {
    const { container } = renderSelect();
    const btn = screen.getByRole('button', { name: 'Pick' });

    fireEvent.click(btn);
    expect(container.querySelector('[role="listbox"]')).toBeTruthy();

    fireEvent.click(btn);
    expect(container.querySelector('[role="listbox"]')).toBeNull();
  });
});

describe('TestLatSelect_Keyboard', () => {
  it('opens on ArrowDown', () => {
    const { container } = renderSelect();
    const btn = screen.getByRole('button', { name: 'Pick' });
    fireEvent.keyDown(btn, { key: 'ArrowDown' });
    expect(container.querySelector('[role="listbox"]')).toBeTruthy();
  });

  it('Escape closes the popup', () => {
    const { container } = renderSelect();
    const btn = screen.getByRole('button', { name: 'Pick' });

    fireEvent.click(btn);
    expect(container.querySelector('[role="listbox"]')).toBeTruthy();

    fireEvent.keyDown(btn, { key: 'Escape' });
    expect(container.querySelector('[role="listbox"]')).toBeNull();
  });

  it('ArrowDown then Enter chooses the next option', () => {
    const { onChange } = renderSelect({ value: 1 });
    const btn = screen.getByRole('button', { name: 'Pick' });

    fireEvent.click(btn);
    fireEvent.keyDown(btn, { key: 'ArrowDown' });
    fireEvent.keyDown(btn, { key: 'Enter' });

    expect(onChange).toHaveBeenCalledWith(2);
  });

  it('End jumps to the last option', () => {
    const { onChange } = renderSelect({ value: 1 });
    const btn = screen.getByRole('button', { name: 'Pick' });

    fireEvent.click(btn);
    fireEvent.keyDown(btn, { key: 'End' });
    fireEvent.keyDown(btn, { key: 'Enter' });

    expect(onChange).toHaveBeenCalledWith(3);
  });
});

describe('TestLatSelect_ClickOutside', () => {
  it('closes when the user mousedowns outside the wrapper', () => {
    const { container } = render(
      <div>
        <LatSelect value={1} onChange={() => {}} options={OPTIONS} ariaLabel="Pick" />
        <button type="button" data-testid="outside">outside</button>
      </div>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Pick' }));
    expect(container.querySelector('[role="listbox"]')).toBeTruthy();

    // mousedown outside the wrap closes the popup.
    fireEvent.mouseDown(screen.getByTestId('outside'));
    expect(container.querySelector('[role="listbox"]')).toBeNull();
  });
});

describe('TestLatSelect_StringValues', () => {
  it('handles string values (channels, device IDs) without coercing to number', () => {
    const opts = [
      { value: '1', label: 'Ch 1' },
      { value: '6', label: 'Ch 6' },
      { value: '11', label: 'Ch 11' },
    ];
    const onChange = vi.fn();
    render(<LatSelect value="6" onChange={onChange} options={opts} ariaLabel="Channel" />);

    const btn = screen.getByRole('button', { name: 'Channel' });
    expect(btn.textContent).toContain('Ch 6');

    fireEvent.click(btn);
    fireEvent.click(screen.getByRole('option', { name: 'Ch 11' }));
    expect(onChange).toHaveBeenCalledWith('11');
  });
});

describe('TestLatSelect_EmptyOptions', () => {
  it('renders the empty placeholder when options is []', () => {
    render(<LatSelect value="" onChange={() => {}} options={[]} ariaLabel="Pick" />);
    fireEvent.click(screen.getByRole('button', { name: 'Pick' }));
    expect(screen.getByText(/no options/i)).toBeTruthy();
  });
});
