const mqtt = require("mqtt");
const sparkplug = require("sparkplug-payload").get("spBv1.0");
const { createState, tick, buildPayload } = require("./state.js");

const BROKER_URL = process.env.MQTT_BROKER_URL || "mqtt://localhost:1883";
const TOPIC = process.env.MQTT_TOPIC || "spBv1.0/site1/DDATA/gateway1";

let state = createState();

function main() {
  const client = mqtt.connect(BROKER_URL, { reconnectPeriod: 2000 });

  client.on("connect", () => {
    console.log(`mqtt publisher connected to ${BROKER_URL}, publishing on ${TOPIC}`);
  });

  client.on("error", (err) => {
    console.error("mqtt error", err.message);
  });

  client.on("reconnect", () => {
    console.log("mqtt publisher reconnecting...");
  });

  setInterval(() => {
    state = tick(state);
    if (!client.connected) {
      return;
    }
    const encoded = sparkplug.encodePayload(buildPayload(state));
    client.publish(TOPIC, Buffer.from(encoded), { qos: 1 });
  }, 3000);
}

main();
