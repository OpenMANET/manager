// =============================================================================
// sysupgradeApi.js — Firmware upgrade discovery and execution via ConnectRPC
// =============================================================================
//
// Thin wrapper over SysupgradeService. Six unary calls + one server stream.
// Each function returns plain JS objects with the wire-format wrappers
// already unwrapped, mirroring the pattern in commsApi.js / meshApi.js.

import { createClient } from '@connectrpc/connect';
import { transport } from './connectClient.js';
import { SysupgradeService } from '../gen/openmanet/sysupgrade/v1/sysupgrade_service_pb.js';

const client = createClient(SysupgradeService, transport);

// ---------------------------------------------------------------------------
// Mappers
// ---------------------------------------------------------------------------

function timestampToDate(ts) {
  if (!ts) return null;
  const seconds = Number(ts.seconds ?? 0);
  if (!Number.isFinite(seconds)) return null;
  return new Date(seconds * 1000);
}

function mapSystemInfo(info) {
  if (!info) return null;
  return {
    hostname: info.hostname ?? '',
    distribution: info.distribution ?? '',
    release: info.release ?? '',
    revision: info.revision ?? '',
    target: info.target ?? '',
    boardName: info.boardName ?? '',
    model: info.model ?? '',
    description: info.description ?? '',
    openmanetVersion: info.openmanetVersion ?? '',
    kernel: info.kernel ?? '',
    architecture: info.architecture ?? '',
    buildDate: info.buildDate ?? '',
    sysupgradeCapable: info.sysupgradeCapable ?? false,
    sysupgradeCapableReason: info.sysupgradeCapableReason ?? '',
    rootfsType: info.rootfsType ?? '',
  };
}

function mapAsset(asset) {
  if (!asset) return null;
  return {
    name: asset.name ?? '',
    downloadUrl: asset.downloadUrl ?? '',
    sizeBytes: Number(asset.sizeBytes ?? 0),
  };
}

function mapRelease(release) {
  if (!release) return null;
  return {
    tag: release.tag ?? '',
    name: release.name ?? '',
    body: release.body ?? '',
    publishedAt: timestampToDate(release.publishedAt),
    prerelease: release.prerelease ?? false,
    version: release.version ?? '',
    assets: (release.assets ?? []).map(mapAsset),
  };
}

function mapUpdate(update) {
  if (!update) return null;
  return {
    release: mapRelease(update.release),
    matchedAsset: mapAsset(update.matchedAsset),
    newerThanCurrent: update.newerThanCurrent ?? false,
  };
}

export function mapStagedImage(image) {
  if (!image) return null;
  return {
    filename: image.filename ?? '',
    sizeBytes: Number(image.sizeBytes ?? 0),
    sha256: image.sha256 ?? '',
    uploadedAt: timestampToDate(image.uploadedAt),
    metadataPresent: image.metadataPresent ?? false,
    compatVersion: image.compatVersion ?? '',
    compatMessage: image.compatMessage ?? '',
    supportedDevices: image.supportedDevices ?? [],
    deviceCompat: image.deviceCompat ?? '',
    imageCompatible: image.imageCompatible ?? false,
    preflightOk: image.preflightOk ?? false,
    preflightError: image.preflightError ?? '',
  };
}

export function mapProgressEvent(ev) {
  if (!ev) return null;
  return {
    phase: ev.phase ?? 0,
    percent: ev.percent ?? 0,
    bytesDone: Number(ev.bytesDone ?? 0),
    bytesTotal: Number(ev.bytesTotal ?? 0),
    message: ev.message ?? '',
    error: ev.error ?? '',
    releaseTag: ev.releaseTag ?? '',
    assetName: ev.assetName ?? '',
    childPid: ev.childPid ?? 0,
    logTail: ev.logTail ?? '',
    updatedAt: timestampToDate(ev.updatedAt),
  };
}

// ---------------------------------------------------------------------------
// Unary RPCs
// ---------------------------------------------------------------------------

export async function fetchSystemInfo() {
  const resp = await client.getSystemInfo({});
  return mapSystemInfo(resp.info);
}

export async function listAvailableUpdates({ forceRefresh = false, includePrerelease = false } = {}) {
  const resp = await client.listAvailableUpdates({
    forceRefresh,
    includePrerelease,
  });
  return {
    updates: (resp.updates ?? []).map(mapUpdate),
    fetchedAt: timestampToDate(resp.fetchedAt),
  };
}

export async function getReleaseDetail(tag) {
  const resp = await client.getReleaseDetail({ tag });
  return mapRelease(resp.release);
}

export async function startUpgrade({ releaseTag, assetName, options, forceInstallUnknownCurrent = false }) {
  await client.startUpgrade({
    releaseTag,
    assetName,
    options: options ?? {},
    forceInstallUnknownCurrent,
  });
}

export async function cancelUpgrade() {
  await client.cancelUpgrade({});
}

export async function getUpgradeStatus() {
  const resp = await client.getUpgradeStatus({});
  return mapProgressEvent(resp.event);
}

// ---------------------------------------------------------------------------
// Staged image (uploaded local firmware)
// ---------------------------------------------------------------------------

export async function getStagedImage() {
  const resp = await client.getStagedImage({});
  return mapStagedImage(resp.image);
}

export async function discardStagedImage() {
  await client.discardStagedImage({});
}

export function mapFactoryResetCapability(cap) {
  if (!cap) {
    return {
      capable: false,
      reason: 'capability not yet detected',
      overlayMountpoint: '',
      backingFs: '',
      firstbootPath: '',
      hostname: '',
    };
  }
  return {
    capable: cap.capable ?? false,
    reason: cap.reason ?? '',
    overlayMountpoint: cap.overlayMountpoint ?? '',
    backingFs: cap.backingFs ?? '',
    firstbootPath: cap.firstbootPath ?? '',
    hostname: cap.hostname ?? '',
  };
}

export async function fetchFactoryResetCapability() {
  const resp = await client.getFactoryResetCapability({});
  return mapFactoryResetCapability(resp.capability);
}

export async function performFactoryReset({ confirmHostname }) {
  await client.performFactoryReset({ confirmHostname });
}

export async function startLocalUpgrade({
  options,
  forceInstallUnknownCurrent = false,
  skipPreflight = false,
} = {}) {
  await client.startLocalUpgrade({
    options: options ?? {},
    forceInstallUnknownCurrent,
    skipPreflight,
  });
}

// uploadFirmware POSTs the supplied File/Blob to /api/sysupgrade/upload
// using multipart/form-data so the browser's native upload-progress
// events drive the UI. Returns the StagedImage metadata on success.
//
// The XHR layer is used (rather than fetch) so onProgress receives
// per-chunk byte counts instead of just an "uploading" boolean.
//
// signal is an AbortSignal; aborting it cancels the upload mid-flight.
export function uploadFirmware(file, { signal, onProgress } = {}) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const form = new FormData();
    form.append('image', file, file?.name ?? 'uploaded-image.bin');

    xhr.open('POST', '/api/sysupgrade/upload', true);
    xhr.responseType = 'json';
    xhr.withCredentials = true;

    xhr.upload.onprogress = (ev) => {
      if (!ev.lengthComputable || typeof onProgress !== 'function') return;
      onProgress({ loaded: ev.loaded, total: ev.total });
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        const body = xhr.response;
        // XHR responseType='json' delivers an object on supported
        // browsers; fall back to manual parsing if the server emitted
        // text or the browser left the body as a string.
        const parsed = typeof body === 'string' ? safeJSON(body) : body;
        resolve(mapUploadResponse(parsed));
        return;
      }
      const errBody = typeof xhr.response === 'string' ? safeJSON(xhr.response) : xhr.response;
      const message = errBody?.error || `upload failed (${xhr.status})`;
      const err = new Error(message);
      err.status = xhr.status;
      reject(err);
    };

    xhr.onerror = () => {
      reject(new Error('upload network error'));
    };

    xhr.onabort = () => {
      reject(new DOMException('upload aborted', 'AbortError'));
    };

    if (signal) {
      if (signal.aborted) {
        xhr.abort();
        return;
      }
      signal.addEventListener('abort', () => xhr.abort(), { once: true });
    }

    xhr.send(form);
  });
}

function safeJSON(s) {
  if (typeof s !== 'string' || s === '') return null;
  try { return JSON.parse(s); } catch { return null; }
}

// mapUploadResponse normalizes the JSON body returned by
// /api/sysupgrade/upload into the same shape as the unary
// GetStagedImage RPC so callers can treat both sources uniformly.
function mapUploadResponse(body) {
  if (!body) return null;
  return {
    filename: body.filename ?? '',
    sizeBytes: Number(body.size_bytes ?? 0),
    sha256: body.sha256 ?? '',
    uploadedAt: body.uploaded_at ? new Date(body.uploaded_at) : null,
    metadataPresent: body.metadata_present ?? false,
    compatVersion: body.compat_version ?? '',
    compatMessage: body.compat_message ?? '',
    supportedDevices: body.supported_devices ?? [],
    deviceCompat: body.device_compat ?? '',
    imageCompatible: body.image_compatible ?? false,
    preflightOk: body.preflight_ok ?? false,
    preflightError: body.preflight_error ?? '',
  };
}

// ---------------------------------------------------------------------------
// Server stream
// ---------------------------------------------------------------------------

// streamUpgradeProgress yields mapped ProgressEvent objects until the
// server closes the stream or `signal` is aborted. Callers should
// `for await (...)` it and pass an AbortSignal to terminate cleanly.
export async function* streamUpgradeProgress(signal) {
  const stream = client.streamUpgradeProgress({}, { signal });
  for await (const frame of stream) {
    yield mapProgressEvent(frame.event);
  }
}
