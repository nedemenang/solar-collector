function clamp(value, lo, hi) {
  return Math.max(lo, Math.min(hi, value));
}

function drift(value, step, lo, hi, rng = Math.random) {
  return clamp(value + (rng() - 0.5) * step, lo, hi);
}

function createState() {
  return {
    irradiance: 400,
    panelTemp: 30,
    dcVoltage: 800,
    inverterPower: 200,
  };
}

function tick(state, rng = Math.random) {
  const irradiance = drift(state.irradiance, 40, 0, 1000, rng);
  const panelTemp = clamp(
    15 + irradiance * 0.04 + (rng() - 0.5) * 3,
    15,
    75
  );
  const dcVoltage = drift(state.dcVoltage, 15, 600, 1000, rng);
  const inverterPower = clamp(
    irradiance * 0.45 + (rng() - 0.5) * 15,
    0,
    500
  );

  return { irradiance, panelTemp, dcVoltage, inverterPower };
}

module.exports = { clamp, drift, createState, tick };
