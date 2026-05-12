// =============================================================================
// WhisperManager.jsx — Whisper model download/remove UI for Settings page
// =============================================================================

import React, { useState, useEffect, useCallback } from 'react';
import {
  checkWhisperAvailable,
  downloadWhisperModel,
  removeWhisperModel,
} from '../services/whisperService.js';
import { useVisibleInterval } from '../hooks/useVisibleInterval.js';

export default function WhisperManager() {
  const [status, setStatus] = useState(null); // server status response
  const [downloading, setDownloading] = useState(false);
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState(null);
  const [removing, setRemoving] = useState(false);

  const fetchStatus = useCallback(async () => {
    const s = await checkWhisperAvailable();
    setStatus(s);

    // Sync local state with server state.
    if (s.state === 'downloading') {
      setDownloading(true);
      setProgress(s.progress || 0);
    } else if (s.state === 'error') {
      setError(s.error || 'Download failed');
      setDownloading(false);
    } else {
      setDownloading(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  // If we land on the page while a download is already in progress, poll for it.
  // useVisibleInterval pauses while the tab is hidden; downloading=false gates
  // the interval to 0 so the effect tears down once the download completes.
  const pollDownload = useCallback(async () => {
    const s = await checkWhisperAvailable();
    setStatus(s);
    setProgress(s.progress || 0);
    if (s.state === 'ready') {
      setDownloading(false);
    } else if (s.state === 'error') {
      setDownloading(false);
      setError(s.error || 'Download failed');
    }
  }, []);
  useVisibleInterval(pollDownload, downloading ? 1000 : 0, [downloading]);

  const handleDownload = async () => {
    setError(null);
    setDownloading(true);
    setProgress(0);

    const ok = await downloadWhisperModel(
      (pct) => setProgress(pct),
      (msg) => setError(msg),
    );

    setDownloading(false);
    if (ok) fetchStatus();
  };

  const handleRemove = async () => {
    setError(null);
    setRemoving(true);

    const ok = await removeWhisperModel();
    if (!ok) {
      setError('Failed to remove whisper files');
    }

    setRemoving(false);
    fetchStatus();
  };

  const available = status?.available === true;

  let statusNode;
  if (downloading) {
    statusNode = <span className="v warn">Downloading...</span>;
  } else if (available) {
    statusNode = <span className="v ok">Available</span>;
  } else {
    statusNode = <span className="v">Not downloaded</span>;
  }

  return (
    <div className="lat-panel">
      <div className="panel-head"><h3>Whisper Speech-to-Text</h3></div>

      <p className="whisper-blurb">
        Offline speech-to-text for closed captions. The model (~75 MB) is downloaded
        on-demand and stored temporarily.
      </p>

      <div className="kv">
        <span className="k">Status</span>
        {statusNode}
      </div>

      {downloading && (
        <div className="pbar">
          <span style={{ width: `${progress}%` }} />
        </div>
      )}

      {error && (
        <div className="lat-alert crit" role="alert">{error}</div>
      )}

      <div className="whisper-actions">
        {!available && !downloading && !error && (
          <button type="button" className="lat-btn primary" onClick={handleDownload}>
            Download Model
          </button>
        )}
        {downloading && (
          <span className="whisper-progress">{progress}%</span>
        )}
        {error && !downloading && (
          <button type="button" className="lat-btn primary" onClick={handleDownload}>
            Retry Download
          </button>
        )}
        {available && (
          <button
            type="button"
            className="lat-btn danger"
            onClick={handleRemove}
            disabled={removing}
          >
            {removing ? 'Removing...' : 'Remove Model'}
          </button>
        )}
      </div>

      <p className="whisper-footnote">
        Files stored in /tmp and will need to be re-downloaded after a device reboot.
      </p>
    </div>
  );
}
