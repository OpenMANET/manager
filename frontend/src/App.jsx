// =============================================================================
// App.jsx — Main application component (state management hub)
// =============================================================================
// This is the top-level React component that manages all application state and
// wires together the services layer (WebSocket, audio engine, whisper, mesh API)
// with the UI components.

import React, { useState, useEffect, useRef, useCallback, useMemo, Suspense, lazy } from 'react';
import './App.css';
import { CHANNELS_DEF, MSG_TYPE, RX_WAVE_HISTORY, VOX_HANGTIME_MS, NEIGHBOR_HISTORY_LENGTH } from './constants.js';
import { connect as wsConnect, disconnect as wsDisconnect, setCallbacks as wsSetCallbacks, sendToggle as wsSendToggle, sendByte as wsSendByte, send as wsSend, isOpen as wsIsOpen } from './services/websocketService.js';
import { initAudio, decodeAndPlay, resetTxTimestamp, startMic, stopMic, setVolume, setMicGain, playBuffer, startMicMonitor, enumerateDevices, setOutputDevice, setMicDevice, setEncoderCallback, clearEncoderCallback } from './services/audioEngine.js';
import { isReady as whisperIsReady, initWhisper, feedAudio as whisperFeedAudio, checkSilenceAndTranscribe, checkWhisperAvailable } from './services/whisperService.js';
import { fetchMeshStatus, fetchMeshTopology } from './services/meshApi.js';
import { getReplayPcm } from './services/replayBuffer.js';
import StatusBar from './components/StatusBar.jsx';
import ChannelGrid from './components/ChannelGrid.jsx';
import PttButton from './components/PttButton.jsx';
import AudioControls from './components/AudioControls.jsx';
import AudioFileTxPanel from './components/AudioFileTx.jsx';
import MeshStatusPanel from './components/MeshStatus.jsx';
// TopologyMap pulls in reagraph + three.js (~900 KB). Lazy-loaded so the
// initial bundle stays small; chunk is fetched only when the panel is shown.
const TopologyMap = lazy(() => import('./components/TopologyMap.jsx'));

function TopologyMapPlaceholder() {
  return (
    <div className="card span-2">
      <div className="card-title">Network Topology</div>
      <div style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        height: 260,
        color: '#9aa7b3',
        fontSize: 12,
      }}>
        Loading topology…
      </div>
    </div>
  );
}
import Transcript from './components/Transcript.jsx';
import LogBox from './components/LogBox.jsx';
import RxWaveform from './components/RxWaveform.jsx';
import MicMeter from './components/MicMeter.jsx';
import SettingsMenu, { ALL_PANELS } from './components/SettingsMenu.jsx';

export default function App() {
  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  const [wsStatus, setWsStatus] = useState('disconnected');
  const [audioStatus, setAudioStatus] = useState('Audio off');
  const [logs, setLogs] = useState([]);
  const [chatMessages, setChatMessages] = useState([]);
  const [meshData, setMeshData] = useState(null);
  const [meshTopology, setMeshTopology] = useState(null);
  const [pttActive, setPttActive] = useState(false);
  const [whisperEnabled, setWhisperEnabled] = useState(false);
  const [debugMode, setDebugMode] = useState(false);
  const [whisperStatus, setWhisperStatus] = useState('');
  const [speakerVol, setSpeakerVol] = useState(80);
  const [micVol, setMicVol] = useState(80);
  const [activeFilter, setActiveFilter] = useState(0);
  const [micLevel, setMicLevel] = useState(0);

  // VOX state
  const [voxEnabled, setVoxEnabled] = useState(false);
  const [voxThreshold, setVoxThreshold] = useState(0.15);
  const voxTimerRef = useRef(null);
  const voxActiveRef = useRef(false);
  const micLevelRef = useRef(0);

  // Channel aliases (stored in localStorage)
  const [channelAliases, setChannelAliases] = useState(() => {
    try {
      return JSON.parse(localStorage.getItem('channelAliases')) || {};
    } catch { return {}; }
  });

  // Neighbor history for sparkline graphs
  const neighborHistoryRef = useRef({});
  const [neighborHistory, setNeighborHistory] = useState({});

  // Replay availability tracking
  const [replayAvailable, setReplayAvailable] = useState({});

  // Panel visibility (persisted in localStorage)
  const [panelVisibility, setPanelVisibility] = useState(() => {
    try {
      const saved = JSON.parse(localStorage.getItem('panelVisibility'));
      if (saved) return saved;
    } catch { /* ignore invalid JSON in localStorage */ }
    const defaults = {};
    ALL_PANELS.forEach((k) => { defaults[k] = true; });
    defaults.fileTx = false;
    return defaults;
  });

  // Audio device selection
  const [audioDevices, setAudioDevices] = useState({ inputs: [], outputs: [] });
  const [selectedOutput, setSelectedOutput] = useState('');
  const [selectedMic, setSelectedMic] = useState('');

  // Channel enable/disable state — initialized with Ch 1 enabled by default.
  const [rxEnabled, setRxEnabled] = useState(() => {
    const init = {};
    CHANNELS_DEF.forEach((c) => { init[c.ch] = (c.ch === 1); });
    return init;
  });
  const [txEnabled, setTxEnabled] = useState(() => {
    const init = {};
    CHANNELS_DEF.forEach((c) => { init[c.ch] = (c.ch === 1); });
    return init;
  });

  // RX last-received timestamps per channel (for activity dot).
  const rxLastTimeRef = useRef({});

  // RX waveform data — shared mutable buffer for performance.
  const rxWaveDataRef = useRef(new Float32Array(RX_WAVE_HISTORY));
  const rxWaveWritePosRef = useRef(0);
  const [rxWaveWritePos, setRxWaveWritePos] = useState(0);

  // Refs for values needed in callbacks that shouldn't trigger re-renders.
  const pttActiveRef = useRef(false);
  const whisperEnabledRef = useRef(false);
  const debugModeRef = useRef(false);
  const micVolRef = useRef(80);
  const audioInitRef = useRef(false);
  const spaceDownRef = useRef(false);
  const rxEnabledRef = useRef(rxEnabled);
  const txEnabledRef = useRef(txEnabled);
  const voxEnabledRef = useRef(false);

  // Keep refs in sync with state.
  useEffect(() => { pttActiveRef.current = pttActive; }, [pttActive]);
  useEffect(() => { whisperEnabledRef.current = whisperEnabled; }, [whisperEnabled]);
  useEffect(() => { debugModeRef.current = debugMode; }, [debugMode]);
  useEffect(() => { micVolRef.current = micVol; }, [micVol]);
  useEffect(() => { rxEnabledRef.current = rxEnabled; }, [rxEnabled]);
  useEffect(() => { txEnabledRef.current = txEnabled; }, [txEnabled]);
  useEffect(() => { voxEnabledRef.current = voxEnabled; }, [voxEnabled]);
  useEffect(() => { micLevelRef.current = micLevel; }, [micLevel]);

  // ---------------------------------------------------------------------------
  // Logging
  // ---------------------------------------------------------------------------

  const addLog = useCallback((msg, cls) => {
    const ts = new Date().toLocaleTimeString('en-US', { hour12: false });
    setLogs((prev) => {
      const next = [...prev, { msg: `[${ts}] ${msg}`, cls: cls || '' }];
      if (next.length > 80) return next.slice(-80);
      return next;
    });
  }, []);

  const dbg = useCallback((msg) => {
    if (debugModeRef.current) {
      addLog('[DBG] ' + msg, 'info');
    }
  }, [addLog]);

  // ---------------------------------------------------------------------------
  // Audio initialization (runs once on first user interaction)
  // ---------------------------------------------------------------------------

  const initAudioOnce = useCallback(async () => {
    if (audioInitRef.current) return;
    audioInitRef.current = true;

    await initAudio(addLog, {
      onPcm: (pcm, ch, srcIP) => {
        // Feed RX waveform visualizer.
        let peak = 0;
        for (let i = 0; i < pcm.length; i++) {
          const v = Math.abs(pcm[i]);
          if (v > peak) peak = v;
        }
        const wp = rxWaveWritePosRef.current;
        rxWaveDataRef.current[wp % RX_WAVE_HISTORY] = peak;
        rxWaveWritePosRef.current = wp + 1;
        setRxWaveWritePos(wp + 1);

        // Update RX last-received timestamp for activity dot.
        rxLastTimeRef.current[ch] = Date.now();

        // Track replay availability.
        setReplayAvailable((prev) => (prev[ch] ? prev : { ...prev, [ch]: true }));

        // Feed whisper if enabled.
        if (whisperEnabledRef.current && whisperIsReady() && ch) {
          whisperFeedAudio(ch, pcm, srcIP);
        }
      },
    });

    setAudioStatus('Audio ready');
  }, [addLog]);

  // ---------------------------------------------------------------------------
  // WebSocket setup
  // ---------------------------------------------------------------------------

  useEffect(() => {
    wsSetCallbacks({
      statusChange: (status) => {
        setWsStatus(status);
        if (status === 'connected') {
          CHANNELS_DEF.forEach((c) => {
            wsSendToggle(MSG_TYPE.RX_TOGGLE, c.ch, rxEnabledRef.current[c.ch]);
            wsSendToggle(MSG_TYPE.TX_TOGGLE, c.ch, txEnabledRef.current[c.ch]);
          });
        }
      },
      rxAudio: (ch, srcIP, opusData) => {
        rxLastTimeRef.current[ch] = Date.now();
        decodeAndPlay(opusData, ch, srcIP);
      },
      error: (msg) => addLog(msg, 'err'),
      log: (msg, cls) => addLog(msg, cls),
    });

    addLog('Comms Bridge (API Mode) starting...', 'info');
    wsConnect();

    return () => {
      wsDisconnect();
    };
  }, [addLog]);

  // ---------------------------------------------------------------------------
  // Init audio on first user interaction (click or touchstart)
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const handler = () => initAudioOnce();
    document.addEventListener('click', handler, { once: true });
    document.addEventListener('touchstart', handler, { once: true });
    return () => {
      document.removeEventListener('click', handler);
      document.removeEventListener('touchstart', handler);
    };
  }, [initAudioOnce]);

  // ---------------------------------------------------------------------------
  // PTT handlers
  // ---------------------------------------------------------------------------

  const pttDown = useCallback(async () => {
    if (pttActiveRef.current) return;
    if (!CHANNELS_DEF.some((c) => txEnabledRef.current[c.ch])) {
      addLog('No TX channels!', 'err');
      return;
    }
    setPttActive(true);
    pttActiveRef.current = true;

    wsSendByte(MSG_TYPE.PTT_DOWN);

    await initAudioOnce();
    resetTxTimestamp();

    await startMic(
      (encodedBuf) => {
        if (wsIsOpen() && pttActiveRef.current) {
          const msg = new Uint8Array(1 + encodedBuf.byteLength);
          msg[0] = MSG_TYPE.TX_AUDIO;
          msg.set(new Uint8Array(encodedBuf), 1);
          wsSend(msg.buffer);
        }
      },
      (gainedSamples) => {
        let sum = 0;
        for (let i = 0; i < gainedSamples.length; i++) {
          sum += gainedSamples[i] * gainedSamples[i];
        }
        const rms = Math.sqrt(sum / gainedSamples.length);
        setMicLevel(rms);
      }
    );

    addLog('TX start', 'tx');
  }, [addLog, initAudioOnce]);

  const pttUp = useCallback(() => {
    if (!pttActiveRef.current) return;
    setPttActive(false);
    pttActiveRef.current = false;

    // If VOX is on, keep mic in monitor mode; otherwise stop fully.
    if (voxEnabledRef.current) {
      // stopMic stops encoding but we need to restart in monitor mode.
      stopMic();
      // Re-open mic in monitor mode for VOX.
      startMicMonitor((gainedSamples) => {
        let sum = 0;
        for (let i = 0; i < gainedSamples.length; i++) {
          sum += gainedSamples[i] * gainedSamples[i];
        }
        setMicLevel(Math.sqrt(sum / gainedSamples.length));
      });
    } else {
      stopMic();
    }

    wsSendByte(MSG_TYPE.PTT_UP);
    addLog('TX end', 'tx');
  }, [addLog]);

  // ---------------------------------------------------------------------------
  // Keyboard (Space bar) PTT
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const handleKeyDown = async (e) => {
      if (e.code === 'Space' && !e.repeat && !spaceDownRef.current) {
        e.preventDefault();
        spaceDownRef.current = true;
        await initAudioOnce();
        pttDown();
      }
    };
    const handleKeyUp = (e) => {
      if (e.code === 'Space') {
        e.preventDefault();
        spaceDownRef.current = false;
        pttUp();
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    document.addEventListener('keyup', handleKeyUp);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.removeEventListener('keyup', handleKeyUp);
    };
  }, [pttDown, pttUp, initAudioOnce]);

  // ---------------------------------------------------------------------------
  // VOX — voice-activated transmit
  // ---------------------------------------------------------------------------

  const handleVoxToggle = useCallback(async (enabled) => {
    setVoxEnabled(enabled);
    voxEnabledRef.current = enabled;

    if (enabled) {
      await initAudioOnce();
      // Start mic in monitor mode so we can detect speech.
      await startMicMonitor((gainedSamples) => {
        let sum = 0;
        for (let i = 0; i < gainedSamples.length; i++) {
          sum += gainedSamples[i] * gainedSamples[i];
        }
        setMicLevel(Math.sqrt(sum / gainedSamples.length));
      });
      addLog('VOX enabled', 'info');
    } else {
      // If PTT was triggered by VOX, release it.
      if (voxActiveRef.current) {
        voxActiveRef.current = false;
        if (pttActiveRef.current) {
          setPttActive(false);
          pttActiveRef.current = false;
          stopMic();
          wsSendByte(MSG_TYPE.PTT_UP);
          addLog('TX end (VOX off)', 'tx');
        }
      } else if (!pttActiveRef.current) {
        stopMic();
      }
      if (voxTimerRef.current) {
        clearTimeout(voxTimerRef.current);
        voxTimerRef.current = null;
      }
      setMicLevel(0);
      addLog('VOX disabled', 'info');
    }
  }, [addLog, initAudioOnce]);

  // VOX level monitoring effect — polls micLevelRef every 50ms
  const voxThresholdRef = useRef(voxThreshold);
  useEffect(() => { voxThresholdRef.current = voxThreshold; }, [voxThreshold]);
  const pttDownRef = useRef(pttDown);
  const pttUpRef = useRef(pttUp);
  useEffect(() => { pttDownRef.current = pttDown; }, [pttDown]);
  useEffect(() => { pttUpRef.current = pttUp; }, [pttUp]);

  useEffect(() => {
    if (!voxEnabled) return;

    const id = setInterval(() => {
      const level = micLevelRef.current;
      const thresh = voxThresholdRef.current;

      if (level > thresh) {
        if (voxTimerRef.current) {
          clearTimeout(voxTimerRef.current);
          voxTimerRef.current = null;
        }
        if (!pttActiveRef.current) {
          voxActiveRef.current = true;
          pttDownRef.current();
        }
      } else if (voxActiveRef.current && pttActiveRef.current) {
        if (!voxTimerRef.current) {
          voxTimerRef.current = setTimeout(() => {
            voxTimerRef.current = null;
            if (voxActiveRef.current && pttActiveRef.current) {
              voxActiveRef.current = false;
              pttUpRef.current();
            }
          }, VOX_HANGTIME_MS);
        }
      }
    }, 50);

    return () => clearInterval(id);
  }, [voxEnabled]);

  // ---------------------------------------------------------------------------
  // Volume changes
  // ---------------------------------------------------------------------------

  const handleSpeakerChange = useCallback((val) => {
    setSpeakerVol(val);
    setVolume(val);
  }, []);

  const handleMicChange = useCallback((val) => {
    setMicVol(val);
    setMicGain(val);
  }, []);

  // ---------------------------------------------------------------------------
  // Audio device enumeration and selection
  // ---------------------------------------------------------------------------

  // Enumerate devices after audio is initialized (labels require permission).
  useEffect(() => {
    if (audioStatus !== 'Audio ready') return;
    enumerateDevices().then(setAudioDevices);
    // Re-enumerate when devices change (plug/unplug).
    const handler = () => enumerateDevices().then(setAudioDevices);
    if (!navigator.mediaDevices) return;
    navigator.mediaDevices.addEventListener('devicechange', handler);
    return () => navigator.mediaDevices.removeEventListener('devicechange', handler);
  }, [audioStatus]);

  const handleOutputChange = useCallback(async (deviceId) => {
    setSelectedOutput(deviceId);
    const ok = await setOutputDevice(deviceId);
    if (ok) addLog('Output: ' + (audioDevices.outputs.find((d) => d.deviceId === deviceId)?.label || deviceId), 'info');
  }, [addLog, audioDevices]);

  const handleMicDeviceChange = useCallback((deviceId) => {
    setSelectedMic(deviceId);
    setMicDevice(deviceId);
    addLog('Mic: ' + (audioDevices.inputs.find((d) => d.deviceId === deviceId)?.label || deviceId), 'info');
  }, [addLog, audioDevices]);

  // ---------------------------------------------------------------------------
  // Channel toggle handlers
  // ---------------------------------------------------------------------------

  const handleToggleRx = useCallback((ch) => {
    setRxEnabled((prev) => {
      const next = { ...prev, [ch]: !prev[ch] };
      wsSendToggle(MSG_TYPE.RX_TOGGLE, ch, next[ch]);
      return next;
    });
  }, []);

  const handleToggleTx = useCallback((ch) => {
    setTxEnabled((prev) => {
      const next = { ...prev, [ch]: !prev[ch] };
      wsSendToggle(MSG_TYPE.TX_TOGGLE, ch, next[ch]);
      return next;
    });
  }, []);

  const handleRxAll = useCallback((on) => {
    setRxEnabled(() => {
      const next = {};
      CHANNELS_DEF.forEach((c) => { next[c.ch] = on; });
      wsSendByte(on ? MSG_TYPE.RX_ALL_ON : MSG_TYPE.RX_ALL_OFF);
      return next;
    });
  }, []);

  const handleTxAll = useCallback((on) => {
    setTxEnabled(() => {
      const next = {};
      CHANNELS_DEF.forEach((c) => { next[c.ch] = on; });
      wsSendByte(on ? MSG_TYPE.TX_ALL_ON : MSG_TYPE.TX_ALL_OFF);
      return next;
    });
  }, []);

  // ---------------------------------------------------------------------------
  // Channel aliasing
  // ---------------------------------------------------------------------------

  const handleAliasChange = useCallback((ch, name) => {
    setChannelAliases((prev) => {
      const next = { ...prev };
      const trimmed = name.trim();
      if (trimmed && trimmed !== `Ch ${ch}`) {
        next[ch] = trimmed;
      } else {
        delete next[ch];
      }
      localStorage.setItem('channelAliases', JSON.stringify(next));
      return next;
    });
  }, []);

  // ---------------------------------------------------------------------------
  // Panel visibility toggle
  // ---------------------------------------------------------------------------

  const handleTogglePanel = useCallback((key) => {
    setPanelVisibility((prev) => {
      const next = { ...prev, [key]: !prev[key] };
      localStorage.setItem('panelVisibility', JSON.stringify(next));
      return next;
    });
  }, []);

  const show = (key) => panelVisibility[key] !== false;

  // ---------------------------------------------------------------------------
  // Audio replay
  // ---------------------------------------------------------------------------

  const handleReplay = useCallback((ch) => {
    const pcm = getReplayPcm(ch);
    if (!pcm) {
      addLog(`No audio to replay on Ch ${ch}`, 'info');
      return;
    }
    playBuffer(pcm);
    const label = channelAliases[ch] || `Ch ${ch}`;
    addLog(`Replaying ${(pcm.length / 48000).toFixed(1)}s on ${label}`, 'info');
  }, [addLog, channelAliases]);

  // ---------------------------------------------------------------------------
  // Whisper captions
  // ---------------------------------------------------------------------------

  const handleWhisperToggle = useCallback(async (enabled) => {
    setWhisperEnabled(enabled);
    whisperEnabledRef.current = enabled;

    dbg(`CC toggle: enabled=${enabled} ready=${whisperIsReady()}`);

    if (enabled && !whisperIsReady()) {
      // Check if the whisper model is available on the server before
      // attempting to initialize.  If not downloaded, guide the user
      // to the Settings page instead of failing with a cryptic error.
      const serverStatus = await checkWhisperAvailable();
      if (!serverStatus.available) {
        setWhisperStatus('Whisper model not downloaded \u2014 go to Settings to download');
        setWhisperEnabled(false);
        whisperEnabledRef.current = false;
        return;
      }

      const ok = await initWhisper(
        (statusMsg) => setWhisperStatus(statusMsg),
        addLog,
        (msg) => { if (debugModeRef.current) addLog('[DBG] ' + msg, 'info'); }
      );
      if (!ok) {
        setWhisperEnabled(false);
        whisperEnabledRef.current = false;
        return;
      }
    }

    setWhisperStatus(
      enabled
        ? (whisperIsReady() ? 'Whisper ready \u2014 listening on all channels' : 'Loading model...')
        : ''
    );
  }, [addLog, dbg]);

  const handleDebugToggle = useCallback((enabled) => {
    setDebugMode(enabled);
    debugModeRef.current = enabled;
    addLog('Debug mode: ' + (enabled ? 'ON' : 'OFF'), 'info');
  }, [addLog]);

  // Whisper silence check interval
  useEffect(() => {
    const id = setInterval(() => {
      if (!whisperEnabledRef.current || !whisperIsReady()) return;
      checkSilenceAndTranscribe((ch, ip, text) => {
        const ts = new Date().toLocaleTimeString('en-US', { hour12: false });
        setChatMessages((prev) => {
          const next = [...prev, { ch, ip, text, ts }];
          if (next.length > 200) return next.slice(-200);
          return next;
        });
      });
    }, 500);
    return () => clearInterval(id);
  }, []);

  // ---------------------------------------------------------------------------
  // Mesh status polling — every 10 seconds
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const poll = async () => {
      try {
        const [data, topo] = await Promise.all([
          fetchMeshStatus(),
          fetchMeshTopology(),
        ]);
        setMeshData(data);
        setMeshTopology(topo);

        // Accumulate neighbor history for sparklines.
        if (data.neighbors && Array.isArray(data.neighbors)) {
          data.neighbors.forEach((n) => {
            const key = n.name || n.mac;
            if (!neighborHistoryRef.current[key]) {
              neighborHistoryRef.current[key] = { signal: [], throughput: [] };
            }
            const h = neighborHistoryRef.current[key];
            h.signal.push(n.signal);
            h.throughput.push(n.throughput || 0);
            if (h.signal.length > NEIGHBOR_HISTORY_LENGTH) {
              h.signal.shift();
              h.throughput.shift();
            }
          });
          // Snapshot for render.
          setNeighborHistory({ ...neighborHistoryRef.current });
        }
      } catch {
        setMeshData({ status: null, nodes: null, neighbors: null, interfaces: null });
        setMeshTopology(null);
      }
    };

    poll();
    const id = setInterval(poll, 10000);
    return () => clearInterval(id);
  }, []);

  // ---------------------------------------------------------------------------
  // File TX PTT control
  // ---------------------------------------------------------------------------

  const handleFilePttSet = useCallback((active) => {
    setPttActive(active);
    pttActiveRef.current = active;
    if (active) {
      // Wire encoder output to WebSocket so file TX frames are sent.
      setEncoderCallback((encodedBuf) => {
        if (wsIsOpen()) {
          const msg = new Uint8Array(1 + encodedBuf.byteLength);
          msg[0] = MSG_TYPE.TX_AUDIO;
          msg.set(new Uint8Array(encodedBuf), 1);
          wsSend(msg.buffer);
        }
      });
      wsSendByte(MSG_TYPE.PTT_DOWN);
    } else {
      clearEncoderCallback();
      wsSendByte(MSG_TYPE.PTT_UP);
    }
  }, []);

  // ---------------------------------------------------------------------------
  // Derived values
  // ---------------------------------------------------------------------------

  const txChannels = useMemo(
    () => CHANNELS_DEF.filter((c) => txEnabled[c.ch]).map((c) => c.ch),
    [txEnabled]
  );

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <div className="comms-page">
      <div className="lat-topbar">
        <div className="node-id">
          COMMS
          <span className="ip">{audioStatus || '—'}</span>
        </div>
        <StatusBar wsStatus={wsStatus} audioStatus={audioStatus} />
      </div>
      <div className="lat-view-header">
        <div>
          <h2>◇ Comms</h2>
          <div className="crumb">Push-to-talk · channels · transcript</div>
        </div>
        <div className="lat-view-toolbar">
          <SettingsMenu panelVisibility={panelVisibility} onTogglePanel={handleTogglePanel} />
        </div>
      </div>

      <div className="main-grid">
        {/* ── Left column: Channels + Mesh ─────────────────────────── */}
        {show('channels') && (
          <ChannelGrid
            channels={CHANNELS_DEF}
            rxEnabled={rxEnabled}
            txEnabled={txEnabled}
            rxLastTimeRef={rxLastTimeRef}
            onToggleRx={handleToggleRx}
            onToggleTx={handleToggleTx}
            onRxAll={handleRxAll}
            onTxAll={handleTxAll}
            channelAliases={channelAliases}
            onAliasChange={handleAliasChange}
            onReplay={handleReplay}
            replayAvailable={replayAvailable}
          />
        )}

        {/* ── Center column: PTT + meters ──────────────────────────── */}
        {show('ptt') && (
          <PttButton
            active={pttActive}
            onDown={pttDown}
            onUp={pttUp}
            txChannels={txChannels}
            channelAliases={channelAliases}
            voxEnabled={voxEnabled}
            onVoxToggle={handleVoxToggle}
          />
        )}

        {/* ── Right column: Audio controls ─────────────────────────── */}
        {show('audioControls') && (
          <AudioControls
            speakerVol={speakerVol}
            micVol={micVol}
            onSpeakerChange={handleSpeakerChange}
            onMicChange={handleMicChange}
            voxEnabled={voxEnabled}
            voxThreshold={voxThreshold}
            onVoxToggle={handleVoxToggle}
            onVoxThresholdChange={setVoxThreshold}
            audioDevices={audioDevices}
            selectedOutput={selectedOutput}
            selectedMic={selectedMic}
            onOutputChange={handleOutputChange}
            onMicDeviceChange={handleMicDeviceChange}
          />
        )}

        {/* ── RX Waveform + Mic Meter combined ─────────────────────── */}
        {(show('waveform') || show('micMeter')) && (
          <div className="card span-full">
            {show('waveform') && (
              <RxWaveform
                rxWaveData={rxWaveDataRef.current}
                writePos={rxWaveWritePos}
                inline
              />
            )}
            {show('micMeter') && (
              <MicMeter level={micLevel} active={pttActive} voxEnabled={voxEnabled} />
            )}
          </div>
        )}

        {/* ── Row 3: Mesh + File TX + Log ─────────────────────────── */}
        {show('meshStatus') && (
          <MeshStatusPanel data={meshData} neighborHistory={neighborHistory} />
        )}

        {show('topology') && (
          <Suspense fallback={<TopologyMapPlaceholder />}>
            <TopologyMap topology={meshTopology} />
          </Suspense>
        )}

        {show('fileTx') && (
          <AudioFileTxPanel
            onLog={addLog}
            onPttSet={handleFilePttSet}
            txEnabled={txEnabled}
          />
        )}

        {/* ── Full-width sections ──────────────────────────────────── */}
        {show('transcript') && (
          <Transcript
            messages={chatMessages}
            whisperEnabled={whisperEnabled}
            onWhisperToggle={handleWhisperToggle}
            debugMode={debugMode}
            onDebugToggle={handleDebugToggle}
            whisperStatus={whisperStatus}
            activeFilter={activeFilter}
            onFilterChange={setActiveFilter}
            channelAliases={channelAliases}
          />
        )}

        {show('log') && (
          <LogBox logs={logs} />
        )}
      </div>
    </div>
  );
}
