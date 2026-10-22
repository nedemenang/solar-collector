# solar-collector

A portfolio piece demonstrating the stack behind on-prem/edge industrial
data collection: a Go service polls a solar site over **OPC UA** and
subscribes to **MQTT/Sparkplug B**, buffers every reading locally so it
survives a WAN outage, runs an in-memory **alarm** engine and **rollup**
aggregator on the live stream, and replays everything to a
**Postgres/TimescaleDB** sink through **Toxiproxy** — which is used to
simulate the cloud link dropping and coming back.

## Topology: what's on-site vs what's in the cloud

This is an **edge gateway** setup, not a "devices phone home to a cloud
collector" setup — worth being explicit about, since it drives most of
the design:

- **On-site** (same site, same LAN — assumed reliable): the OPC UA
  server, the MQTT broker + publisher, and **the collector itself**. The
  collector runs right next to the equipment it reads, the way a real
  industrial gateway would sit in a cabinet at the solar site.
- **Cloud** (far away, over the public internet — the unreliable hop):
  Postgres/TimescaleDB. Toxiproxy sits in front of it specifically to
  simulate *that* link — a site's internet uplink, a VPN tunnel, a
  cellular modem — dropping out.

Industrial protocols like OPC UA and Sparkplug B are normally run on a
trusted local network at the site, not exposed to the public internet —
so the collector talks to them locally, and takes on the job of
surviving the flaky part of the journey: the long haul back to the cloud
database. That's also why the local SQLite buffer lives on the collector
and nowhere else — it's solving "I already collected this reading, but
my *outbound* link is down," which is only ever true at the on-site
gateway. A dropped connection between the collector and the OPC UA
server/MQTT broker is a different, unrelated failure mode (see
[What to look out for](#what-to-look-out-for-while-running-it)): no
amount of buffering fixes "the sensor was never read," only "the reading
exists but couldn't be shipped yet."

## How it works

```
┌─────────────────────────── ON-SITE (site LAN — reliable) ────────────────────────────┐
│                                                                                       │
│  ┌─────────────────────┐          ┌──────────────────────────┐                       │
│  │  opcua-server (Node) │          │  mqtt-publisher (Node)   │                       │
│  │  node-opcua sim:     │          │  Sparkplug B DDATA sim:  │                       │
│  │  PanelTemp,          │          │  AmbientTemp,            │                       │
│  │  Irradiance,         │          │  StringCurrent           │                       │
│  │  DCVoltage,          │          │  (encoded as Float,      │                       │
│  │  InverterPower       │          │   not Double — see ⚠️)   │                       │
│  └──────────┬───────────┘          └────────────┬─────────────┘                      │
│             │ OPC UA read                        │ MQTT publish                       │
│             ▼                                    ▼                                    │
│  ┌─────────────────────┐          ┌──────────────────────────┐                       │
│  │ adapters/opcua       │          │  mosquitto (broker)      │                       │
│  │ Poller (polls on a   │          └────────────┬─────────────┘                       │
│  │ fixed interval)      │                       │ subscribe                            │
│  └──────────┬───────────┘          ┌────────────▼─────────────┐                       │
│             │                      │  adapters/mqtt           │                       │
│             │                      │  Subscriber (decodes     │                       │
│             │                      │  Sparkplug protobuf)     │                       │
│             │                      └────────────┬─────────────┘                       │
│             │                                   │                                     │
│             └──────────────┐     ┌──────────────┘                                     │
│                             ▼     ▼                                                    │
│                     model.Reading channel   ◄── all of the above, plus what follows,  │
│                             │                    runs as ONE process: cmd/collector    │
│      ┌──────────────────────┼──────────────────────┐                                  │
│      ▼                      ▼                      ▼                                  │
│ ┌───────────────┐  ┌──────────────────┐  ┌────────────────────┐                       │
│ │ alarm.Engine  │  │ rollup.Aggregator │  │ ingest.Store        │                      │
│ │ (threshold    │  │ (per-tag min/max/ │  │ (local SQLite       │                      │
│ │  breach/clear)│  │  avg/count window)│  │  buffer, WAL mode)  │                      │
│ └───────────────┘  └──────────────────┘  └──────────┬──────────┘                      │
│                                                      │ periodic drain                  │
│                                                      ▼                                 │
│                                            ┌────────────────────┐                      │
│                                            │ ingest.Replayer     │                      │
│                                            └──────────┬──────────┘                      │
│                                                       │ batched upsert                  │
└───────────────────────────────────────────────────────┼──────────────────────────────┘
                                                          │
                                        ≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈▼≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈  WAN — the
                                        unreliable hop; this is what Toxiproxy simulates
                                        cutting (scripts/toggle-partition.sh)
                                                          │
┌──────────────────────────────────────────────────────── ▼ ───────────────────────────┐
│                         CLOUD (public internet away from the site)                    │
│                                                          │                             │
│                                            ┌────────────────────┐                      │
│                                            │  Toxiproxy          │                      │
│                                            │  (:5433, cuttable)  │                      │
│                                            └──────────┬──────────┘                      │
│                                                       ▼                                 │
│                                            ┌────────────────────┐                      │
│                                            │ Postgres/Timescale  │                      │
│                                            │ (readings, alarms,  │                      │
│                                            │  rollups tables)    │                      │
│                                            └────────────────────┘                      │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

Every reading gets a fresh UUID where it enters the system (in the
adapter). It's written locally with `INSERT ... ON CONFLICT DO NOTHING`
and replayed to Postgres with `ON CONFLICT DO UPDATE` — so re-sending a
batch after a crash or partition never duplicates rows. The alarm engine
and rollup aggregator run entirely in memory against the same reading
stream, independent of whether the sink is reachable, so alarms keep
firing and rollups keep completing even while the collector is cut off
from Postgres.

### Component map

| Path | What it does |
|---|---|
| `cmd/collector` | Entrypoint — wires config → adapters → buffer → alarm/rollup → replayer |
| `internal/adapters/opcua` | Polls configured OPC UA tags on a fixed interval, reconnects on failure |
| `internal/adapters/mqtt` | Subscribes to a Sparkplug B `DDATA` topic, decodes protobuf payloads |
| `internal/alarm` | Tracks one active alarm per tag; raises on first breach, clears once |
| `internal/rollup` | Per-tag min/max/avg/count over a fixed time window, in memory |
| `internal/ingest` | Local SQLite buffer (`Store`) + periodic drain-to-sink logic (`Replayer`) |
| `internal/sink` | Postgres writer — batched idempotent upsert via `pgx` |
| `internal/config` | Loads `config/config.yaml`, with env var overrides for container deploys |
| `internal/model` | Shared `Reading`/`Alarm`/`Rollup` types |
| `simulators/opcua-server` | Node/`node-opcua` fake site hardware (drifting Double variables) |
| `simulators/mqtt-publisher` | Node/`mqtt` fake gateway (Sparkplug B `DDATA`, encoded as Float) |
| `docker/mosquitto` | Broker config for the compose stack |
| `db/migrations` | Postgres/Timescale schema (`readings` is a hypertable partitioned on `ts`; `alarms`, `rollups` are plain tables) |
| `scripts/toxiproxy-setup.sh`, `scripts/toggle-partition.sh` | Create the Postgres proxy, then cut/restore it on demand |

## Running the full stack

```bash
docker compose up --build
./scripts/toxiproxy-setup.sh          # one-time: creates the Postgres proxy
./scripts/toggle-partition.sh down    # cut the DB link — watch the collector keep polling/alarming/buffering
./scripts/toggle-partition.sh up      # restore it — watch the replayer drain the backlog
```

Ports exposed on the host:

| Port | Service |
|---|---|
| `4840` | OPC UA simulator |
| `1883` | Mosquitto |
| `8474` | Toxiproxy admin API (used by the two scripts) |
| `5433` | Postgres **through** Toxiproxy — this is what the collector actually uses |
| `5434` | Postgres **directly** — connect here with `psql`/a GUI client to inspect data without going through the proxy |

### What to look out for while running it

- **The Toxiproxy demo only cuts the on-site → cloud (collector →
  Postgres) hop** — see [Topology](#topology-whats-on-site-vs-whats-in-the-cloud).
  If you instead stop `opcua-server` or `mosquitto` to see what happens,
  that's a *different* failure mode: the collector will just log
  reconnect warnings and produce no readings for that tag/topic until the
  source comes back, with no gap-filling — the SQLite buffer doesn't (and
  can't) help there, since there's no reading to buffer if it was never
  taken.
- **Run `toxiproxy-setup.sh` before `toggle-partition.sh`.** The proxy
  doesn't exist until the setup script creates it; `toggle-partition.sh`
  will fail with a 404 from Toxiproxy's admin API if you skip that step.
- **Give Postgres a few seconds to pass its healthcheck** before expecting
  the collector to successfully replay — `docker compose up` starts
  Toxiproxy only after Postgres reports healthy, but the collector itself
  starts immediately and will just buffer locally (by design) until the
  sink is reachable.
- **Connect to port `5434`, not `5433`, if you want to inspect data
  directly** (e.g. `psql -h localhost -p 5434 -U collector -d solar`).
  `5433` is the Toxiproxy-fronted port the collector uses, and disabling
  the proxy (`toggle-partition.sh down`) will make `5433` refuse
  connections entirely — that's the point of the demo, but it'll confuse
  you if you're trying to query data through it.
- **After `toggle-partition.sh down`, expect to see the collector log
  replay errors, not crash.** The replayer retries on its own schedule
  (`replay_interval_seconds` in `config/config.yaml`) and readings keep
  accumulating in the local SQLite file (`/data/collector.db` inside the
  container) in the meantime — nothing is lost.
- **The Sparkplug decoder only understands `Float` (protobuf datatype 9),
  not `Double` (datatype 10)** — a `Double` metric decodes with an empty
  value and is silently dropped. `simulators/mqtt-publisher` deliberately
  encodes as `Float` for this reason. If you ever change the publisher to
  encode as `Double`, the collector will stop seeing `AmbientTemp`/
  `StringCurrent` readings with no error logged — this is the single
  highest-risk spot in the whole pipeline.
- **The OPC UA simulator's tags live under namespace index 1** (node-opcua's
  default "own" namespace), which `internal/adapters/opcua` hardcodes as
  `ns=1;s=<tag>`. If you point the collector at a different OPC UA server,
  its tags likely live in a different namespace and reads will fail.
- **`simulators/mqtt-publisher` pins an old `protobufjs` version with known
  unpatched CVEs**, accepted deliberately because it only ever runs
  locally as a fake device — don't reuse that dependency choice anywhere
  that isn't a throwaway simulator.

## Running the tests

```bash
make tests
```

This runs the Go suite (`go test ./...`) and both simulators' Node suites
(`node --test` in each of `simulators/mqtt-publisher` and
`simulators/opcua-server`).

A few things worth knowing:

- Every package under `internal/` has table-driven unit tests, and the
  packages that would otherwise need a live dependency (`internal/sink`,
  `internal/adapters/opcua`, `internal/adapters/mqtt`) have interfaces
  extracted specifically so those tests run against hand-rolled fakes —
  no real Postgres, OPC UA server, or MQTT broker required.
- `internal/ingest`'s `Store` tests are the one exception: they run
  against a real (temp-file) SQLite database rather than a mock, since
  the database itself is the thing worth testing there.
- `internal/adapters/opcua` and `internal/adapters/mqtt` each also have a
  `_manual_test.go` gated behind `-tags manual`, which hits a **live**
  simulator/broker instead of a fake. These exist because the real risk
  in this codebase is protocol/wire-format mismatches (like the
  Float/Double gotcha above), which a mocked test can't catch. `make
  tests` does not run these — bring up the relevant simulator (e.g. via
  `docker compose up opcua-server mosquitto mqtt-publisher`) and run:

  ```bash
  go test -tags manual ./...
  ```
