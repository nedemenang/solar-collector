const test = require("node:test");
const assert = require("node:assert/strict");
const { clamp, drift, createState, tick } = require("./state.js");

test("clamp", async (t) => {
  const cases = [
    { name: "below range clamps to lo", value: -5, lo: 0, hi: 10, expected: 0 },
    { name: "above range clamps to hi", value: 15, lo: 0, hi: 10, expected: 10 },
    { name: "in range passes through", value: 5, lo: 0, hi: 10, expected: 5 },
    { name: "equal to lo passes through", value: 0, lo: 0, hi: 10, expected: 0 },
    { name: "equal to hi passes through", value: 10, lo: 0, hi: 10, expected: 10 },
  ];

  for (const c of cases) {
    await t.test(c.name, () => {
      assert.equal(clamp(c.value, c.lo, c.hi), c.expected);
    });
  }
});

test("drift", async (t) => {
  await t.test("rng = () => 1 trends to/clamps at hi", () => {
    const result = drift(500, 40, 0, 1000, () => 1);
    assert.equal(result, clamp(500 + (1 - 0.5) * 40, 0, 1000));
    assert.equal(result, 520);
  });

  await t.test("rng = () => 1 clamps at hi when near boundary", () => {
    const result = drift(990, 40, 0, 1000, () => 1);
    assert.equal(result, 1000);
  });

  await t.test("rng = () => 0 trends to/clamps at lo", () => {
    const result = drift(500, 40, 0, 1000, () => 0);
    assert.equal(result, clamp(500 + (0 - 0.5) * 40, 0, 1000));
    assert.equal(result, 480);
  });

  await t.test("rng = () => 0 clamps at lo when near boundary", () => {
    const result = drift(10, 40, 0, 1000, () => 0);
    assert.equal(result, 0);
  });

  await t.test("rng = () => 0.5 leaves value unchanged (zero delta)", () => {
    const result = drift(500, 40, 0, 1000, () => 0.5);
    assert.equal(result, 500);
  });

  await t.test("matches exact formula for arbitrary rng", () => {
    const rng = () => 0.75;
    const value = 800,
      step = 15,
      lo = 600,
      hi = 1000;
    const expected = clamp(value + (rng() - 0.5) * step, lo, hi);
    assert.equal(drift(value, step, lo, hi, rng), expected);
  });
});

test("tick", async (t) => {
  await t.test("computes fields from a constant rng = 0.5 (all jitter zero)", () => {
    const input = createState();
    const snapshot = { ...input };

    const result = tick(input, () => 0.5);

    // irradiance: drift is a no-op at rng=0.5, so it equals the clamped input value
    const expectedIrradiance = clamp(input.irradiance, 0, 1000);
    assert.equal(result.irradiance, expectedIrradiance);

    // panelTemp derived from the NEW irradiance, jitter term zero
    const expectedPanelTemp = clamp(15 + expectedIrradiance * 0.04, 15, 75);
    assert.equal(result.panelTemp, expectedPanelTemp);

    // dcVoltage: drift is a no-op at rng=0.5
    const expectedDcVoltage = clamp(input.dcVoltage, 600, 1000);
    assert.equal(result.dcVoltage, expectedDcVoltage);

    // inverterPower derived from the NEW irradiance, jitter term zero
    const expectedInverterPower = clamp(expectedIrradiance * 0.45, 0, 500);
    assert.equal(result.inverterPower, expectedInverterPower);

    // input state must not be mutated
    assert.deepEqual(input, snapshot);
  });

  await t.test("does not mutate input state for non-trivial rng", () => {
    const input = { irradiance: 350, panelTemp: 28, dcVoltage: 770, inverterPower: 180 };
    const snapshot = { ...input };

    tick(input, () => 0.9);

    assert.deepEqual(input, snapshot);
  });

  await t.test("stays within documented bounds for an asymmetric/alternating rng", () => {
    let count = 0;
    const alternatingRng = () => (count++ % 2 === 0 ? 0 : 1);

    let state = createState();
    for (let i = 0; i < 20; i++) {
      state = tick(state, alternatingRng);

      assert.ok(state.irradiance >= 0 && state.irradiance <= 1000, "irradiance in bounds");
      assert.ok(state.panelTemp >= 15 && state.panelTemp <= 75, "panelTemp in bounds");
      assert.ok(state.dcVoltage >= 600 && state.dcVoltage <= 1000, "dcVoltage in bounds");
      assert.ok(state.inverterPower >= 0 && state.inverterPower <= 500, "inverterPower in bounds");
    }
  });
});
