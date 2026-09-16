const test = require("node:test");
const assert = require("node:assert/strict");
const { clamp, drift, createState, tick, buildPayload } = require("./state.js");

test("clamp", async (t) => {
  const cases = [
    { name: "below lo clamps to lo", value: -5, lo: 0, hi: 10, expected: 0 },
    { name: "above hi clamps to hi", value: 15, lo: 0, hi: 10, expected: 10 },
    { name: "in range is unchanged", value: 5, lo: 0, hi: 10, expected: 5 },
    { name: "equal to lo is unchanged", value: 0, lo: 0, hi: 10, expected: 0 },
    { name: "equal to hi is unchanged", value: 10, lo: 0, hi: 10, expected: 10 },
    { name: "negative range", value: -20, lo: -10, hi: -1, expected: -10 },
  ];

  for (const c of cases) {
    await t.test(c.name, () => {
      assert.equal(clamp(c.value, c.lo, c.hi), c.expected);
    });
  }
});

test("drift", async (t) => {
  await t.test("rng = () => 1 (max) trends toward and clamps at hi", () => {
    let value = 22;
    const rng = () => 1;
    for (let i = 0; i < 100; i++) {
      value = drift(value, 1.5, 10, 45, rng);
    }
    assert.equal(value, 45);
  });

  await t.test("rng = () => 0 (min) trends toward and clamps at lo", () => {
    let value = 8;
    const rng = () => 0;
    for (let i = 0; i < 100; i++) {
      value = drift(value, 0.8, 0, 15, rng);
    }
    assert.equal(value, 0);
  });

  await t.test("rng = () => 0.5 leaves value unchanged (zero delta)", () => {
    const value = 22;
    const rng = () => 0.5;
    assert.equal(drift(value, 1.5, 10, 45, rng), value);
  });

  await t.test("matches clamp(value + (rng() - 0.5) * step, lo, hi)", () => {
    const cases = [
      { value: 22, step: 1.5, lo: 10, hi: 45, rng: () => 0.75 },
      { value: 8, step: 0.8, lo: 0, hi: 15, rng: () => 0.1 },
      { value: 40, step: 1.5, lo: 10, hi: 45, rng: () => 0.9 },
    ];
    for (const c of cases) {
      const expected = clamp(c.value + (c.rng() - 0.5) * c.step, c.lo, c.hi);
      assert.equal(drift(c.value, c.step, c.lo, c.hi, c.rng), expected);
    }
  });

  await t.test("default rng (Math.random) stays within bounds", () => {
    const result = drift(22, 1.5, 10, 45);
    assert.ok(result >= 10 && result <= 45);
  });
});

test("tick", async (t) => {
  await t.test("drifts ambientTemp and stringCurrent per the formula", () => {
    const state = createState();
    const rng = () => 0.75;

    const expectedAmbientTemp = clamp(state.ambientTemp + (rng() - 0.5) * 1.5, 10, 45);
    const expectedStringCurrent = clamp(state.stringCurrent + (rng() - 0.5) * 0.8, 0, 15);

    const next = tick(state, rng);

    assert.equal(next.ambientTemp, expectedAmbientTemp);
    assert.equal(next.stringCurrent, expectedStringCurrent);
  });

  await t.test("does not mutate the input state object", () => {
    const state = createState();
    const snapshot = { ...state };
    const rng = () => 1;

    const next = tick(state, rng);

    assert.deepEqual(state, snapshot);
    assert.notEqual(next, state);
  });
});

test("buildPayload", async (t) => {
  await t.test("matches the exact expected shape", () => {
    const state = { ambientTemp: 30.25, stringCurrent: 12.5 };
    const now = () => 1234567890;

    const payload = buildPayload(state, now);

    assert.deepEqual(payload, {
      timestamp: 1234567890,
      metrics: [
        { name: "AmbientTemp", value: 30.25, type: "Float" },
        { name: "StringCurrent", value: 12.5, type: "Float" },
      ],
      seq: 0,
    });
  });

  await t.test("both metrics are typed Float (required for the Go decoder)", () => {
    const payload = buildPayload(createState(), () => 0);
    for (const metric of payload.metrics) {
      assert.equal(metric.type, "Float");
    }
  });
});
