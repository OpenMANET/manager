// =============================================================================
// AudioControls.jsx — Speaker/mic volume sliders, device selectors, VOX
// =============================================================================

import React from 'react';

export default React.memo(function AudioControls({
  speakerVol,
  micVol,
  onSpeakerChange,
  onMicChange,
  voxEnabled,
  voxThreshold,
  onVoxToggle,
  onVoxThresholdChange,
  audioDevices,
  selectedOutput,
  selectedMic,
  onOutputChange,
  onMicDeviceChange,
}) {
  return (
    <div className="card">
      <div className="volume-row">
        <label>Speaker:</label>
        <input
          type="range"
          min="0"
          max="100"
          value={speakerVol}
          onChange={(e) => onSpeakerChange(Number(e.target.value))}
        />
      </div>
      {audioDevices && audioDevices.outputs && audioDevices.outputs.length > 0 && (
        <div className="device-row">
          <label>Output:</label>
          <select
            className="device-select"
            value={selectedOutput || ''}
            onChange={(e) => onOutputChange && onOutputChange(e.target.value)}
          >
            {audioDevices.outputs.map((d) => (
              <option key={d.deviceId} value={d.deviceId}>
                {d.label || `Speaker ${d.deviceId.slice(0, 8)}`}
              </option>
            ))}
          </select>
        </div>
      )}
      <div className="volume-row">
        <label>Mic:</label>
        <input
          type="range"
          min="0"
          max="100"
          value={micVol}
          onChange={(e) => onMicChange(Number(e.target.value))}
        />
      </div>
      {audioDevices && audioDevices.inputs && audioDevices.inputs.length > 0 && (
        <div className="device-row">
          <label>Input:</label>
          <select
            className="device-select"
            value={selectedMic || ''}
            onChange={(e) => onMicDeviceChange && onMicDeviceChange(e.target.value)}
          >
            {audioDevices.inputs.map((d) => (
              <option key={d.deviceId} value={d.deviceId}>
                {d.label || `Mic ${d.deviceId.slice(0, 8)}`}
              </option>
            ))}
          </select>
        </div>
      )}
      <div className="vox-row">
        <input
          type="checkbox"
          id="vox-toggle"
          checked={voxEnabled || false}
          onChange={(e) => onVoxToggle && onVoxToggle(e.target.checked)}
        />
        <label htmlFor="vox-toggle">VOX</label>
        {voxEnabled && (
          <>
            <label className="vox-thresh-label">Threshold:</label>
            <input
              type="range"
              min="0"
              max="50"
              value={Math.round((voxThreshold || 0.15) * 100)}
              onChange={(e) => onVoxThresholdChange && onVoxThresholdChange(Number(e.target.value) / 100)}
              className="vox-slider"
            />
          </>
        )}
      </div>
    </div>
  );
});
