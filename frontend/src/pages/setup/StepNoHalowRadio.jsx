// =============================================================================
// StepNoHalowRadio.jsx — Error page rendered when no HaLow radio is detected
// =============================================================================
//
// The wizard is for setting up an 802.11ah (HaLow) mesh, so on hardware
// without a HaLow radio there's nothing it can do. Rendered standalone
// (outside SetupProvider) by SetupGate so the user lands on the
// explanation rather than an empty wizard shell. No "retry" button — the
// radio doesn't appear at runtime, so a refresh wouldn't help.

export default function StepNoHalowRadio() {
  return (
    <div className="lat-body grid-3">
      <div className="lat-panel col-span-all" role="alert">
        <div className="panel-head">
          <h3>No HaLow Radio</h3>
        </div>
        <div className="lat-alert crit">
          This device does not appear to have an 802.11ah (HaLow) radio.
        </div>
        <p style={{ marginTop: 12 }}>
          The OpenMANET setup wizard configures HaLow mesh networking and
          cannot proceed on this hardware. If you believe this is a
          mistake, contact your system integrator.
        </p>
      </div>
    </div>
  );
}
