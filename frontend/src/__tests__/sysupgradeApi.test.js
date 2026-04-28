// =============================================================================
// sysupgradeApi.test.js — tests for the sysupgrade ConnectRPC wrapper
// =============================================================================

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createRouterTransport } from '@connectrpc/connect';
import { Timestamp } from '@bufbuild/protobuf';
import { SysupgradeService } from '../gen/openmanet/sysupgrade/v1/sysupgrade_service_connect.js';
import { Phase } from '../gen/openmanet/sysupgrade/v1/sysupgrade_pb.js';

vi.mock('../services/connectClient.js', () => ({ transport: {} }));

async function loadApiWith(transport) {
  vi.doMock('../services/connectClient.js', () => ({ transport }));
  return import('../services/sysupgradeApi.js');
}

beforeEach(() => {
  vi.resetModules();
});

describe('TestFetchSystemInfo', () => {
  it('unwraps response.info and maps fields', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        getSystemInfo() {
          return {
            info: {
              hostname: 'host-1',
              distribution: 'OpenMANET',
              release: '24.10',
              target: 'bcm27xx/bcm2711',
              boardName: 'rpi-4b',
              openmanetVersion: '1.7.0',
              sysupgradeCapable: true,
              rootfsType: 'squashfs',
            },
          };
        },
      });
    });

    const { fetchSystemInfo } = await loadApiWith(transport);
    const info = await fetchSystemInfo();
    expect(info).toMatchObject({
      hostname: 'host-1',
      distribution: 'OpenMANET',
      release: '24.10',
      target: 'bcm27xx/bcm2711',
      boardName: 'rpi-4b',
      openmanetVersion: '1.7.0',
      sysupgradeCapable: true,
      rootfsType: 'squashfs',
    });
  });

  it('propagates errors from the RPC', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        getSystemInfo() { throw new Error('boom'); },
      });
    });
    const { fetchSystemInfo } = await loadApiWith(transport);
    await expect(fetchSystemInfo()).rejects.toThrow();
  });
});

describe('TestListAvailableUpdates', () => {
  it('maps updates and fetchedAt timestamp', async () => {
    const ts = Timestamp.fromDate(new Date('2026-04-22T12:00:00Z'));
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        listAvailableUpdates(req) {
          expect(req.forceRefresh).toBe(true);
          expect(req.includePrerelease).toBe(false);
          return {
            updates: [
              {
                release: {
                  tag: 'v1.9.0',
                  name: 'OpenMANET 1.9.0',
                  body: '## changes\n- one\n',
                  prerelease: false,
                  version: '1.9.0',
                  publishedAt: ts,
                  assets: [
                    { name: 'fw.img.gz', downloadUrl: 'https://x/y', sizeBytes: 1024n },
                  ],
                },
                matchedAsset: { name: 'fw.img.gz', sizeBytes: 1024n, downloadUrl: 'https://x/y' },
                newerThanCurrent: true,
              },
            ],
            fetchedAt: ts,
          };
        },
      });
    });

    const { listAvailableUpdates } = await loadApiWith(transport);
    const result = await listAvailableUpdates({ forceRefresh: true });
    expect(result.fetchedAt).toBeInstanceOf(Date);
    expect(result.fetchedAt.toISOString()).toBe('2026-04-22T12:00:00.000Z');
    expect(result.updates).toHaveLength(1);
    const u = result.updates[0];
    expect(u.release.tag).toBe('v1.9.0');
    expect(u.release.publishedAt.toISOString()).toBe('2026-04-22T12:00:00.000Z');
    expect(u.matchedAsset.name).toBe('fw.img.gz');
    expect(u.matchedAsset.sizeBytes).toBe(1024);
    expect(u.newerThanCurrent).toBe(true);
  });

  it('handles empty responses', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        listAvailableUpdates() { return {}; },
      });
    });
    const { listAvailableUpdates } = await loadApiWith(transport);
    const result = await listAvailableUpdates();
    expect(result.updates).toEqual([]);
    expect(result.fetchedAt).toBeNull();
  });
});

describe('TestStartUpgrade', () => {
  it('forwards releaseTag, assetName, options, forceInstallUnknownCurrent', async () => {
    const calls = [];
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        startUpgrade(req) {
          calls.push(req);
          return {};
        },
      });
    });
    const { startUpgrade } = await loadApiWith(transport);
    await startUpgrade({
      releaseTag: 'v1.9.0',
      assetName: 'fw.img.gz',
      options: { testOnly: true, verbose: true },
      forceInstallUnknownCurrent: true,
    });
    expect(calls).toHaveLength(1);
    expect(calls[0].releaseTag).toBe('v1.9.0');
    expect(calls[0].assetName).toBe('fw.img.gz');
    expect(calls[0].options.testOnly).toBe(true);
    expect(calls[0].options.verbose).toBe(true);
    expect(calls[0].forceInstallUnknownCurrent).toBe(true);
  });
});

describe('TestGetUpgradeStatus', () => {
  it('unwraps response.event', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        getUpgradeStatus() {
          return {
            event: {
              phase: Phase.DOWNLOADING,
              percent: 42,
              bytesDone: 1234n,
              bytesTotal: 5678n,
              releaseTag: 'v1.9.0',
              assetName: 'fw.img.gz',
            },
          };
        },
      });
    });
    const { getUpgradeStatus } = await loadApiWith(transport);
    const ev = await getUpgradeStatus();
    expect(ev.phase).toBe(Phase.DOWNLOADING);
    expect(ev.percent).toBe(42);
    expect(ev.bytesDone).toBe(1234);
    expect(ev.bytesTotal).toBe(5678);
    expect(ev.releaseTag).toBe('v1.9.0');
  });
});

describe('TestStreamUpgradeProgress', () => {
  it('yields mapped events from the server stream', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        async *streamUpgradeProgress() {
          yield { event: { phase: Phase.DOWNLOADING, percent: 10 } };
          yield { event: { phase: Phase.VERIFYING, percent: 100 } };
          yield { event: { phase: Phase.UPGRADING, percent: 100, childPid: 999 } };
        },
      });
    });

    const { streamUpgradeProgress } = await loadApiWith(transport);
    const out = [];
    for await (const ev of streamUpgradeProgress()) {
      out.push(ev);
    }
    expect(out).toHaveLength(3);
    expect(out[0].phase).toBe(Phase.DOWNLOADING);
    expect(out[1].phase).toBe(Phase.VERIFYING);
    expect(out[2].phase).toBe(Phase.UPGRADING);
    expect(out[2].childPid).toBe(999);
  });

  it('terminates when the AbortSignal is fired', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        async *streamUpgradeProgress() {
          yield { event: { phase: Phase.DOWNLOADING, percent: 10 } };
          // simulate a long-lived stream — the consumer aborts after the first event
          await new Promise((resolve) => setTimeout(resolve, 1000));
          yield { event: { phase: Phase.VERIFYING, percent: 100 } };
        },
      });
    });

    const { streamUpgradeProgress } = await loadApiWith(transport);
    const ctrl = new AbortController();
    const out = [];
    try {
      for await (const ev of streamUpgradeProgress(ctrl.signal)) {
        out.push(ev);
        ctrl.abort();
      }
    } catch {
      // Aborts surface as a thrown ConnectError; that's fine.
    }
    expect(out).toHaveLength(1);
    expect(out[0].phase).toBe(Phase.DOWNLOADING);
  });
});

describe('TestStagedImageRPCs', () => {
  it('GetStagedImage maps a populated response', async () => {
    const ts = Timestamp.fromDate(new Date('2026-04-26T08:00:00Z'));
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        getStagedImage() {
          return {
            image: {
              filename: 'staged.img.gz',
              sizeBytes: 4096n,
              sha256: 'cafeb33f',
              uploadedAt: ts,
              metadataPresent: true,
              compatVersion: '1.0',
              supportedDevices: ['raspberrypi,4-model-b', 'brcm,bcm2711'],
              deviceCompat: 'raspberrypi,4-model-b',
              imageCompatible: true,
              preflightOk: false,
              preflightError: 'wrong arch',
            },
          };
        },
      });
    });

    const { getStagedImage } = await loadApiWith(transport);
    const out = await getStagedImage();
    expect(out).toMatchObject({
      filename: 'staged.img.gz',
      sizeBytes: 4096,
      sha256: 'cafeb33f',
      metadataPresent: true,
      compatVersion: '1.0',
      supportedDevices: ['raspberrypi,4-model-b', 'brcm,bcm2711'],
      deviceCompat: 'raspberrypi,4-model-b',
      imageCompatible: true,
      preflightOk: false,
      preflightError: 'wrong arch',
    });
    expect(out.uploadedAt).toBeInstanceOf(Date);
  });

  it('GetStagedImage returns null when no image is staged', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        getStagedImage() {
          return {};
        },
      });
    });
    const { getStagedImage } = await loadApiWith(transport);
    const out = await getStagedImage();
    expect(out).toBeNull();
  });

  it('DiscardStagedImage forwards the unary call', async () => {
    let called = 0;
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        discardStagedImage() {
          called += 1;
          return {};
        },
      });
    });
    const { discardStagedImage } = await loadApiWith(transport);
    await discardStagedImage();
    expect(called).toBe(1);
  });

  it('StartLocalUpgrade forwards options + force + skip flags', async () => {
    let captured;
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        startLocalUpgrade(req) {
          captured = req;
          return {};
        },
      });
    });
    const { startLocalUpgrade } = await loadApiWith(transport);
    await startLocalUpgrade({
      options: { verbose: true },
      forceInstallUnknownCurrent: true,
      skipPreflight: true,
    });
    expect(captured.options.verbose).toBe(true);
    expect(captured.forceInstallUnknownCurrent).toBe(true);
    expect(captured.skipPreflight).toBe(true);
  });
});

describe('TestFactoryResetRPCs', () => {
  it('GetFactoryResetCapability maps a capable response', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        getFactoryResetCapability() {
          return {
            capability: {
              capable: true,
              reason: 'ok',
              overlayMountpoint: 'overlayfs:/overlay /',
              backingFs: 'overlay',
              firstbootPath: '/sbin/firstboot',
              hostname: 'BCM2711-1003',
            },
          };
        },
      });
    });

    const { fetchFactoryResetCapability } = await loadApiWith(transport);
    const cap = await fetchFactoryResetCapability();
    expect(cap).toMatchObject({
      capable: true,
      reason: 'ok',
      overlayMountpoint: 'overlayfs:/overlay /',
      backingFs: 'overlay',
      firstbootPath: '/sbin/firstboot',
      hostname: 'BCM2711-1003',
    });
  });

  it('GetFactoryResetCapability maps a not-capable response', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        getFactoryResetCapability() {
          return {
            capability: {
              capable: false,
              reason: 'no rootfs_data partition or overlayfs mount',
            },
          };
        },
      });
    });

    const { fetchFactoryResetCapability } = await loadApiWith(transport);
    const cap = await fetchFactoryResetCapability();
    expect(cap.capable).toBe(false);
    expect(cap.reason).toContain('no rootfs_data');
  });

  it('PerformFactoryReset forwards the confirm hostname', async () => {
    let captured;
    const transport = createRouterTransport(({ service }) => {
      service(SysupgradeService, {
        performFactoryReset(req) {
          captured = req;
          return {};
        },
      });
    });

    const { performFactoryReset } = await loadApiWith(transport);
    await performFactoryReset({ confirmHostname: 'BCM2711-1003' });
    expect(captured.confirmHostname).toBe('BCM2711-1003');
  });
});
