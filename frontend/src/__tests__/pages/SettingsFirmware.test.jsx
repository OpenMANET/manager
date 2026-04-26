// =============================================================================
// SettingsFirmware.test.jsx — page-level tests for the Firmware settings tab
// =============================================================================

import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';

import { Phase } from '../../gen/openmanet/sysupgrade/v1/sysupgrade_pb.js';

// ---- mock the sysupgrade API module ---------------------------------------
const apiState = {
  systemInfo: null,
  systemInfoErr: null,
  updates: [],
  fetchedAt: null,
  release: null,
  releaseErr: null,
  startCalls: [],
  startErr: null,
  cancelCalls: 0,
  cancelErr: null,
  initialStatus: null,
  streamFn: null, // override to drive the stream from a test
  staged: null,
  stagedErr: null,
  discardCalls: 0,
  discardErr: null,
  startLocalCalls: [],
  startLocalErr: null,
  uploadCalls: [],
  uploadResult: null,
  uploadErr: null,
};

function defaultStream() {
  return {
    [Symbol.asyncIterator]() {
      return {
        next: () => new Promise(() => {}),
        return: () => Promise.resolve({ value: undefined, done: true }),
      };
    },
  };
}

vi.mock('../../services/sysupgradeApi.js', () => ({
  fetchSystemInfo: vi.fn(async () => {
    if (apiState.systemInfoErr) throw apiState.systemInfoErr;
    return apiState.systemInfo;
  }),
  listAvailableUpdates: vi.fn(async () => ({
    updates: apiState.updates,
    fetchedAt: apiState.fetchedAt,
  })),
  getReleaseDetail: vi.fn(async () => {
    if (apiState.releaseErr) throw apiState.releaseErr;
    return apiState.release;
  }),
  startUpgrade: vi.fn(async (req) => {
    apiState.startCalls.push(req);
    if (apiState.startErr) throw apiState.startErr;
  }),
  cancelUpgrade: vi.fn(async () => {
    apiState.cancelCalls += 1;
    if (apiState.cancelErr) throw apiState.cancelErr;
  }),
  getUpgradeStatus: vi.fn(async () => apiState.initialStatus),
  streamUpgradeProgress: vi.fn(() => (apiState.streamFn ?? defaultStream)()),
  getStagedImage: vi.fn(async () => {
    if (apiState.stagedErr) throw apiState.stagedErr;
    return apiState.staged;
  }),
  discardStagedImage: vi.fn(async () => {
    apiState.discardCalls += 1;
    if (apiState.discardErr) throw apiState.discardErr;
    apiState.staged = null;
  }),
  startLocalUpgrade: vi.fn(async (req) => {
    apiState.startLocalCalls.push(req);
    if (apiState.startLocalErr) throw apiState.startLocalErr;
  }),
  uploadFirmware: vi.fn(async (file, opts) => {
    apiState.uploadCalls.push({ file, opts });
    if (apiState.uploadErr) throw apiState.uploadErr;
    return apiState.uploadResult;
  }),
}));

// Imported AFTER the mock is registered.
import SettingsFirmware from '../../pages/SettingsFirmware.jsx';

function resetApiState() {
  apiState.systemInfo = null;
  apiState.systemInfoErr = null;
  apiState.updates = [];
  apiState.fetchedAt = null;
  apiState.release = null;
  apiState.releaseErr = null;
  apiState.startCalls = [];
  apiState.startErr = null;
  apiState.cancelCalls = 0;
  apiState.cancelErr = null;
  apiState.initialStatus = null;
  apiState.streamFn = null;
  apiState.staged = null;
  apiState.stagedErr = null;
  apiState.discardCalls = 0;
  apiState.discardErr = null;
  apiState.startLocalCalls = [];
  apiState.startLocalErr = null;
  apiState.uploadCalls = [];
  apiState.uploadResult = null;
  apiState.uploadErr = null;
}

const capableInfo = {
  hostname: 'test-1',
  distribution: 'OpenMANET',
  release: '24.10',
  target: 'bcm27xx/bcm2711',
  boardName: 'rpi-4b',
  model: 'RPI 4B',
  openmanetVersion: '1.7.0',
  kernel: '6.6.102',
  architecture: 'aarch64',
  buildDate: '2025-06-23',
  sysupgradeCapable: true,
  sysupgradeCapableReason: '',
  rootfsType: 'squashfs',
};

const incapableInfo = {
  ...capableInfo,
  hostname: 'dev',
  sysupgradeCapable: false,
  sysupgradeCapableReason: 'no /sbin/sysupgrade',
  rootfsType: 'overlay',
};

const sampleUpdate = {
  release: {
    tag: 'v1.9.0',
    name: 'OpenMANET 1.9.0',
    body: '## Changes\n- one\n- two\n',
    publishedAt: new Date('2026-04-22T12:00:00Z'),
    prerelease: false,
    version: '1.9.0',
    assets: [],
  },
  matchedAsset: {
    name: 'openmanet-1.9.0-sysupgrade.img.gz',
    sizeBytes: 52_400_000,
    downloadUrl: 'https://example.com/v1.9.0',
  },
  newerThanCurrent: true,
};

describe('SettingsFirmware', () => {
  beforeEach(() => {
    resetApiState();
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('renders system info from the API and the capable chip', async () => {
    apiState.systemInfo = capableInfo;
    apiState.updates = [];
    apiState.fetchedAt = new Date('2026-04-25T12:00:00Z');

    render(<SettingsFirmware />);

    expect(await screen.findByText('test-1')).toBeInTheDocument();
    expect(await screen.findByText('OpenMANET 24.10')).toBeInTheDocument();
    expect(screen.getByText(/Sysupgrade Capable/i)).toBeInTheDocument();
    expect(screen.getByText('1.7.0')).toBeInTheDocument();
  });

  it('shows the not-capable alert and hides the updates panel when the device is incapable', async () => {
    apiState.systemInfo = incapableInfo;

    render(<SettingsFirmware />);

    expect(await screen.findByText(/Not Sysupgrade Capable/i)).toBeInTheDocument();
    expect(screen.getAllByText(/no \/sbin\/sysupgrade/i).length).toBeGreaterThan(0);
    expect(screen.queryByRole('button', { name: /check for updates/i })).not.toBeInTheDocument();
  });

  it('renders the available updates table when capable', async () => {
    apiState.systemInfo = capableInfo;
    apiState.updates = [sampleUpdate];
    apiState.fetchedAt = new Date('2026-04-25T12:00:00Z');

    render(<SettingsFirmware />);

    expect(await screen.findByText('v1.9.0')).toBeInTheDocument();
    expect(screen.getByText('openmanet-1.9.0-sysupgrade.img.gz')).toBeInTheDocument();
    // matchedAsset chip says "1 newer"
    expect(screen.getByText(/1 newer/i)).toBeInTheDocument();
  });

  it('opens the confirm card when Install is clicked', async () => {
    apiState.systemInfo = capableInfo;
    apiState.updates = [sampleUpdate];
    apiState.release = {
      tag: 'v1.9.0',
      body: '# Release v1.9.0\n\nGreat new features.',
      assets: [],
    };

    render(<SettingsFirmware />);

    const installBtn = await screen.findByRole('button', { name: /^install$/i });
    fireEvent.click(installBtn);

    // Confirm card content
    expect(await screen.findByRole('button', { name: /install — device will reboot/i })).toBeInTheDocument();
    expect(screen.getByText(/Selected/i)).toBeInTheDocument();
    // Release notes loaded via getReleaseDetail
    await waitFor(() => {
      expect(screen.getByText('Release v1.9.0')).toBeInTheDocument();
    });
  });

  it('calls startUpgrade with the chosen options', async () => {
    apiState.systemInfo = capableInfo;
    apiState.updates = [sampleUpdate];
    apiState.release = { tag: 'v1.9.0', body: 'notes', assets: [] };

    render(<SettingsFirmware />);

    fireEvent.click(await screen.findByRole('button', { name: /^install$/i }));

    const testOnly = await screen.findByLabelText(/Test only/i);
    fireEvent.click(testOnly);

    fireEvent.click(screen.getByRole('button', { name: /install — device will reboot/i }));

    await waitFor(() => {
      expect(apiState.startCalls).toHaveLength(1);
    });
    expect(apiState.startCalls[0].releaseTag).toBe('v1.9.0');
    expect(apiState.startCalls[0].assetName).toBe('openmanet-1.9.0-sysupgrade.img.gz');
    expect(apiState.startCalls[0].options.testOnly).toBe(true);
  });

  it('disables the confirm button when test-only and force conflict', async () => {
    apiState.systemInfo = capableInfo;
    apiState.updates = [sampleUpdate];
    apiState.release = { tag: 'v1.9.0', body: 'notes', assets: [] };

    render(<SettingsFirmware />);

    fireEvent.click(await screen.findByRole('button', { name: /^install$/i }));

    fireEvent.click(await screen.findByLabelText(/Test only/i));
    fireEvent.click(screen.getByLabelText(/^Force/i));

    const confirm = screen.getByRole('button', { name: /install — device will reboot/i });
    expect(confirm).toBeDisabled();
    expect(screen.getByText(/mutually exclusive/i)).toBeInTheDocument();
  });

  it('shows a downloading progress bar from the streaming RPC', async () => {
    apiState.systemInfo = capableInfo;
    apiState.updates = [sampleUpdate];

    apiState.initialStatus = {
      phase: Phase.DOWNLOADING,
      percent: 42,
      bytesDone: 1000,
      bytesTotal: 2000,
      releaseTag: 'v1.9.0',
      assetName: sampleUpdate.matchedAsset.name,
    };

    render(<SettingsFirmware />);

    expect(await screen.findByText(/Phase: Downloading/i)).toBeInTheDocument();
    expect(screen.getByText('42%')).toBeInTheDocument();
    expect(screen.getByText(/Upgrade in progress/i)).toBeInTheDocument();
  });

  it('shows the rebooting alert when phase=UPGRADING', async () => {
    apiState.systemInfo = capableInfo;
    apiState.initialStatus = {
      phase: Phase.UPGRADING,
      percent: 100,
      bytesDone: 0,
      bytesTotal: 0,
      releaseTag: 'v1.9.0',
      childPid: 28473,
    };

    render(<SettingsFirmware />);

    expect(await screen.findByText(/Phase: Upgrading/i)).toBeInTheDocument();
    expect(screen.getByText(/Sysupgrade is running/i)).toBeInTheDocument();
    expect(screen.getByText('28473')).toBeInTheDocument();
  });

  it('shows the failure alert when phase=FAILED', async () => {
    apiState.systemInfo = capableInfo;
    apiState.initialStatus = {
      phase: Phase.FAILED,
      percent: 0,
      bytesDone: 0,
      bytesTotal: 0,
      error: 'sha256 mismatch',
    };

    render(<SettingsFirmware />);

    expect(await screen.findByText(/Phase: Failed/i)).toBeInTheDocument();
    expect(screen.getByText(/sha256 mismatch/i)).toBeInTheDocument();
  });

  it('cancel upgrade triggers cancelUpgrade RPC', async () => {
    apiState.systemInfo = capableInfo;
    apiState.initialStatus = {
      phase: Phase.DOWNLOADING,
      percent: 30,
      bytesDone: 100,
      bytesTotal: 200,
      releaseTag: 'v1.9.0',
    };

    render(<SettingsFirmware />);

    const cancelBtn = await screen.findByRole('button', { name: /cancel upgrade/i });
    fireEvent.click(cancelBtn);

    await waitFor(() => {
      expect(apiState.cancelCalls).toBe(1);
    });
  });

  it('check-for-updates calls listAvailableUpdates with forceRefresh=true', async () => {
    apiState.systemInfo = capableInfo;
    apiState.updates = [];

    const { listAvailableUpdates } = await import('../../services/sysupgradeApi.js');

    render(<SettingsFirmware />);

    await screen.findByRole('button', { name: /check for updates/i });
    listAvailableUpdates.mockClear();

    fireEvent.click(screen.getByRole('button', { name: /check for updates/i }));

    await waitFor(() => {
      expect(listAvailableUpdates).toHaveBeenCalledWith(
        expect.objectContaining({ forceRefresh: true }),
      );
    });
  });

  // ---- Local Image (upload) panel -----------------------------------------

  const stagedOK = {
    filename: 'openmanet-bcm27xx-bcm2711-custom.img.gz',
    sizeBytes: 12_345_678,
    sha256: 'd34db33fcafe',
    uploadedAt: new Date('2026-04-26T12:00:00Z'),
    filenameMatchesTarget: true,
    preflightOk: true,
    preflightError: '',
  };

  const stagedFailed = {
    ...stagedOK,
    filenameMatchesTarget: false,
    preflightOk: false,
    preflightError: 'image bad magic',
  };

  it('renders the empty Local Image panel when no upload is staged', async () => {
    apiState.systemInfo = capableInfo;

    render(<SettingsFirmware />);

    expect(await screen.findByRole('heading', { name: /Local Image/i })).toBeInTheDocument();
    expect(screen.getByText(/No image staged/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Choose File/i })).toBeInTheDocument();
  });

  it('renders staged image metadata and the install button', async () => {
    apiState.systemInfo = capableInfo;
    apiState.staged = stagedOK;

    render(<SettingsFirmware />);

    expect(await screen.findByText(stagedOK.filename)).toBeInTheDocument();
    expect(screen.getByText(stagedOK.sha256)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Install Staged Image/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Discard/i })).toBeInTheDocument();
    // Replace button shows when an image is already staged.
    expect(screen.getByRole('button', { name: /Replace/i })).toBeInTheDocument();
  });

  it('shows a preflight-failed alert when the staged image preflight failed', async () => {
    apiState.systemInfo = capableInfo;
    apiState.staged = stagedFailed;

    render(<SettingsFirmware />);

    const matches = await screen.findAllByText(/Preflight failed/i);
    expect(matches.length).toBeGreaterThan(0);
    expect(screen.getByText(/image bad magic/)).toBeInTheDocument();
    expect(screen.getByText(/Filename mismatch/i)).toBeInTheDocument();
  });

  it('discard staged image calls discardStagedImage', async () => {
    apiState.systemInfo = capableInfo;
    apiState.staged = stagedOK;

    render(<SettingsFirmware />);

    const discardBtn = await screen.findByRole('button', { name: /Discard/i });
    fireEvent.click(discardBtn);

    await waitFor(() => {
      expect(apiState.discardCalls).toBe(1);
    });
  });

  it('uploading a file updates the staged slot via uploadFirmware', async () => {
    apiState.systemInfo = capableInfo;
    apiState.uploadResult = stagedOK;

    render(<SettingsFirmware />);

    await screen.findByRole('heading', { name: /Local Image/i });

    // The file <input type=file> is hidden; access it directly.
    const fileInput = document.querySelector('input[type="file"]');
    const file = new File(['content'], 'openmanet-bcm27xx-bcm2711-custom.img.gz', {
      type: 'application/octet-stream',
    });

    fireEvent.change(fileInput, { target: { files: [file] } });

    await waitFor(() => {
      expect(apiState.uploadCalls).toHaveLength(1);
    });
    expect(apiState.uploadCalls[0].file).toBe(file);

    await waitFor(() => {
      expect(screen.getByText(stagedOK.filename)).toBeInTheDocument();
    });
  });

  it('install of a staged image calls startLocalUpgrade with options', async () => {
    apiState.systemInfo = capableInfo;
    apiState.staged = stagedOK;

    render(<SettingsFirmware />);

    fireEvent.click(await screen.findByRole('button', { name: /Install Staged Image/i }));

    // Confirm card opens for source=local.
    const confirmBtn = await screen.findByRole('button', {
      name: /install — device will reboot/i,
    });
    expect(confirmBtn).toBeInTheDocument();

    fireEvent.click(confirmBtn);

    await waitFor(() => {
      expect(apiState.startLocalCalls).toHaveLength(1);
    });
    expect(apiState.startLocalCalls[0]).toMatchObject({
      forceInstallUnknownCurrent: false,
      skipPreflight: false,
    });
  });

  it('install confirm is blocked when preflight failed unless skipPreflight is set', async () => {
    apiState.systemInfo = capableInfo;
    apiState.staged = stagedFailed;

    render(<SettingsFirmware />);

    fireEvent.click(await screen.findByRole('button', { name: /Install Staged Image/i }));

    const confirmBtn = await screen.findByRole('button', {
      name: /install — device will reboot/i,
    });
    expect(confirmBtn).toBeDisabled();

    // Tick the skip-preflight checkbox; the button enables.
    fireEvent.click(screen.getByLabelText(/Skip preflight/i));
    expect(confirmBtn).toBeEnabled();

    fireEvent.click(confirmBtn);

    await waitFor(() => {
      expect(apiState.startLocalCalls).toHaveLength(1);
    });
    expect(apiState.startLocalCalls[0].skipPreflight).toBe(true);
  });
});
