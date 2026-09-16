const { OPCUAServer, Variant, DataType } = require("node-opcua");
const { createState, tick } = require("./state.js");

let state = createState();

function addTag(namespace, device, name, initial) {
  namespace.addVariable({
    componentOf: device,
    nodeId: `s=${name}`,
    browseName: name,
    dataType: "Double",
    value: {
      get: () => new Variant({ dataType: DataType.Double, value: initial() }),
    },
  });
}

async function main() {
  const server = new OPCUAServer({
    port: 4840,
    resourcePath: "/UA/SolarSite",
    // The container's actual hostname is its container ID (e.g.
    // "b736ef1e29c2"), which node-opcua uses to build its advertised
    // endpoint by default. The Go collector connects via the Docker
    // Compose service name instead, so that name must be added here or
    // node-opcua rejects the client's GetEndpoints request as unknown.
    alternateHostname: "opcua-server",
    buildInfo: {
      productName: "SolarSiteSimulator",
      buildNumber: "1",
      buildDate: new Date(),
    },
  });

  await server.initialize();

  const addressSpace = server.engine.addressSpace;
  const namespace = addressSpace.getOwnNamespace();

  const device = namespace.addObject({
    organizedBy: addressSpace.rootFolder.objects,
    browseName: "SolarSite",
  });

  addTag(namespace, device, "Irradiance", () => state.irradiance);
  addTag(namespace, device, "PanelTemp", () => state.panelTemp);
  addTag(namespace, device, "DCVoltage", () => state.dcVoltage);
  addTag(namespace, device, "InverterPower", () => state.inverterPower);

  setInterval(() => {
    state = tick(state);
  }, 1000);

  await server.start();
  console.log(`OPC UA simulator listening at ${server.getEndpointUrl()}`);
}

main().catch((err) => {
  console.error("failed to start OPC UA simulator", err);
  process.exit(1);
});
