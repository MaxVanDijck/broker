import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { BrokerService } from "@/gen/broker_pb";

const transport = createConnectTransport({
  baseUrl: window.location.origin,
});

export const broker = createClient(BrokerService, transport);
