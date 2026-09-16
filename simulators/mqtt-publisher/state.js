// Pure, testable state/math helpers for the mqtt-publisher simulator.
// No external deps -- kept separate from publish.js (which owns the mqtt
// connection and setInterval loop) so the drift math and payload shape can
// be unit tested without a broker.

function clamp(value, lo, hi) {
  return Math.max(lo, Math.min(hi, value));
}

function drift(value, step, lo, hi, rng = Math.random) {
  return clamp(value + (rng() - 0.5) * step, lo, hi);
}

function createState() {
  return {
    ambientTemp: 22,
    stringCurrent: 8,
  };
}

function tick(state, rng = Math.random) {
  return {
    ambientTemp: drift(state.ambientTemp, 1.5, 10, 45, rng),
    stringCurrent: drift(state.stringCurrent, 0.8, 0, 15, rng),
  };
}

function buildPayload(state, now = Date.now) {
  return {
    timestamp: now(),
    metrics: [
      { name: "AmbientTemp", value: state.ambientTemp, type: "Float" },
      { name: "StringCurrent", value: state.stringCurrent, type: "Float" },
    ],
    seq: 0,
  };
}

module.exports = { clamp, drift, createState, tick, buildPayload };
