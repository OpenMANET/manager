// =============================================================================
// DeviceAudioPanel.jsx — Hardware mixer controls on the radio device itself
// =============================================================================
//
// Distinct from AudioControls ("Web Audio"), which only scales the
// browser's WebAudio graph: these sliders drive the ALSA mixer on the
// OpenVLM sound card via CommsService.GetAudioMixer / UpdateAudioMixer.
// State is polled so VOL+/VOL− hardware-button changes converge into the
// UI; slider writes commit on release, not per tick, to avoid hammering
// the config persist path.

import React, { useState, useRef, useCallback } from 'react';
import { fetchAudioMixer, updateAudioMixer } from '../services/commsApi.js';
import { useVisibleInterval } from '../hooks/useVisibleInterval.js';

const POLL_MS = 5000;

export default function DeviceAudioPanel() {
  const [mixer, setMixer] = useState(null);     // last known AudioMixerState
  const [pending, setPending] = useState({});   // { speaker?, mic? } during drag
  const [error, setError] = useState(false);
  const draggingRef = useRef(false);

  const poll = useCallback(async () => {
    const state = await fetchAudioMixer();
    if (draggingRef.current) return; // never fight an active drag
    if (state === null) {
      setError(true);
      return;
    }
    setError(false);
    setMixer(state);
  }, []);

  useVisibleInterval(poll, POLL_MS);

  const commit = useCallback(async (fields) => {
    draggingRef.current = false;
    const state = await updateAudioMixer(fields);
    setPending({});
    if (state === null) {
      setError(true);
      return;
    }
    setError(false);
    setMixer(state);
  }, []);

  const onSpeakerDrag = (v) => {
    draggingRef.current = true;
    setPending((p) => ({ ...p, speaker: v }));
  };
  const onMicDrag = (v) => {
    draggingRef.current = true;
    setPending((p) => ({ ...p, mic: v }));
  };

  const loading = mixer === null && !error;
  const unavailable = !loading && !error && !mixer?.available;

  const speakerVal = pending.speaker ?? mixer?.speakerVolume ?? 0;
  const micVal = pending.mic ?? mixer?.micVolume ?? 0;
  const hasSpeaker = mixer?.speakerVolume !== undefined;
  const hasMic = mixer?.micVolume !== undefined;
  const hasAgc = mixer?.agcEnabled !== undefined;
  const agcOn = mixer?.agcEnabled === true;

  const commitSpeaker = () => commit({ speakerVolume: speakerVal });
  const commitMic = () => commit({ micVolume: micVal });
  const toggleAgc = () => commit({ agcEnabled: !agcOn });

  return (
    <div className="lat-panel">
      <div className="panel-head"><h3>Device Audio</h3></div>

      {loading && <div className="comms-device-audio-empty">Loading…</div>}

      {error && <div className="lat-alert crit">Mixer unavailable — request failed.</div>}

      {unavailable && (
        <div className="comms-device-audio-empty">No audio device detected.</div>
      )}

      {!loading && !error && mixer?.available && (
        <>
          {hasSpeaker && (
            <div className="pbar-row">
              <span className="pbar-label">Speaker</span>
              <input
                type="range"
                className="lat-slider"
                min="0"
                max="100"
                value={speakerVal}
                onChange={(e) => onSpeakerDrag(Number(e.target.value))}
                onPointerUp={commitSpeaker}
                onKeyUp={commitSpeaker}
                aria-label="Device speaker volume"
              />
              <span className="pbar-val">{speakerVal}</span>
            </div>
          )}

          {hasMic && (
            <div className="pbar-row">
              <span className="pbar-label">Mic</span>
              <input
                type="range"
                className="lat-slider"
                min="0"
                max="100"
                value={micVal}
                onChange={(e) => onMicDrag(Number(e.target.value))}
                onPointerUp={commitMic}
                onKeyUp={commitMic}
                aria-label="Device mic volume"
              />
              <span className="pbar-val">{micVal}</span>
            </div>
          )}

          {hasAgc && (
            <button
              type="button"
              className={`lat-toggle${agcOn ? ' on' : ''}`}
              aria-pressed={agcOn}
              onClick={toggleAgc}
            >
              <span className="track"><span className="thumb"></span></span>
              <span className="label">Auto Gain Control</span>
            </button>
          )}
        </>
      )}
    </div>
  );
}
