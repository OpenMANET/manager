// =============================================================================
// WhisperManager.jsx — Whisper model download/remove UI for Settings page
// =============================================================================

import React, { useState, useEffect, useCallback } from 'react';
import {
  checkWhisperAvailable,
  downloadWhisperModel,
  removeWhisperModel,
} from '../services/whisperService.js';

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
  useEffect(() => {
    if (!downloading) return;

    const poll = setInterval(async () => {
      const s = await checkWhisperAvailable();
      setStatus(s);
      setProgress(s.progress || 0);

      if (s.state === 'ready') {
        setDownloading(false);
        clearInterval(poll);
      } else if (s.state === 'error') {
        setDownloading(false);
        setError(s.error || 'Download failed');
        clearInterval(poll);
      }
    }, 1000);

    return () => clearInterval(poll);
  }, [downloading]);

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

  const btnStyle = {
    padding: '8px 20px', border: 'none', borderRadius: 6, cursor: 'pointer',
    fontSize: '0.85em', fontWeight: 600, transition: 'opacity 0.15s',
  };
  const primaryBtn = { ...btnStyle, background: 'var(--accent)', color: 'var(--text)' };
  const dangerBtn = { ...btnStyle, background: 'rgba(204,51,51,0.15)', border: '1px solid var(--red)', color: 'var(--red)' };

  return (
    <div className="card">
      <div className="card-title">Whisper Speech-to-Text</div>
      <p style={{ fontSize: '0.82em', color: 'var(--muted)', margin: '0 0 10px' }}>
        Offline speech-to-text for closed captions. The model (~75 MB) is downloaded
        on-demand and stored temporarily.
      </p>

      {/* Status */}
      <div style={{ fontSize: '0.85em', marginBottom: 10 }}>
        Status:{' '}
        {downloading ? (
          <span style={{ color: 'var(--yellow)' }}>Downloading...</span>
        ) : available ? (
          <span style={{ color: 'var(--green)' }}>Available</span>
        ) : (
          <span style={{ color: 'var(--muted)' }}>Not downloaded</span>
        )}
      </div>

      {/* Progress bar */}
      {downloading && (
        <div style={{
          background: 'var(--border)', borderRadius: 4, height: 8,
          marginBottom: 10, overflow: 'hidden',
        }}>
          <div style={{
            background: 'var(--green)', height: '100%', borderRadius: 4,
            width: `${progress}%`, transition: 'width 0.3s ease',
          }} />
        </div>
      )}

      {/* Error */}
      {error && (
        <div style={{
          background: 'rgba(204,51,51,0.1)', border: '1px solid var(--red)', borderRadius: 6,
          padding: '6px 10px', marginBottom: 10, fontSize: '0.82em', color: 'var(--red)',
        }}>
          {error}
        </div>
      )}

      {/* Actions */}
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
        {!available && !downloading && (
          <button onClick={handleDownload} style={primaryBtn}>
            Download Model
          </button>
        )}
        {downloading && (
          <span style={{ fontSize: '0.82em', color: 'var(--muted)' }}>{progress}%</span>
        )}
        {error && !downloading && (
          <button onClick={handleDownload} style={primaryBtn}>
            Retry Download
          </button>
        )}
        {available && (
          <button
            onClick={handleRemove}
            disabled={removing}
            style={{ ...dangerBtn, opacity: removing ? 0.5 : 1 }}
          >
            {removing ? 'Removing...' : 'Remove Model'}
          </button>
        )}
      </div>

      <p style={{ fontSize: '0.75em', color: 'var(--muted)', margin: '10px 0 0' }}>
        Files stored in /tmp and will need to be re-downloaded after a device reboot.
      </p>
    </div>
  );
}
