import * as jayson from "jayson";
import { URL } from "url";

/**
 * Wrapper of jayson.Client that provides async API.
 */
export class RpcClient {
  private jaysonClient: jayson.Client;

  constructor(jaysonClient: jayson.Client) {
    this.jaysonClient = jaysonClient;
  }

  public async request<A extends jayson.RequestParamsLike,
                       R extends jayson.JSONRPCResultLike>(method: string, args: A): Promise<R> {
    return new Promise<R>((resolve, reject) => {
      this.jaysonClient.request(method, args,
        (err, error, result: R) => {
          if (err || error) {
            reject(err || error);
            return;
          }
          resolve(result);
        });
    });
  }
}

export function makeMgmtClient(): RpcClient {
  let mgmtEnv = process.env.MGMT;
  if (mgmtEnv === "0") {
    throw new Error("management socket disabled");
  }
  mgmtEnv = mgmtEnv || "tcp://127.0.0.1:6345";

  const u = new URL(mgmtEnv);
  if (!/^tcp[46]?:$/.test(u.protocol)) {
    throw new Error("unsupported MGMT scheme " + u.protocol);
  }

  const jaysonClient = jayson.Client.tcp({
    host: u.hostname,
    port: parseInt(u.port, 10),
  });
  return new RpcClient(jaysonClient);
}