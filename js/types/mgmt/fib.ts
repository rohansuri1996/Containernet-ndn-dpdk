import type { Counter, Uint } from "../core";
import type { FaceID } from "../iface";
import type { Name } from "../ndni";
import type { NameArg } from "./common";

export interface FibMgmt {
  Info: { args: {}; reply: FibInfo };
  List: { args: {}; reply: Name[] };
  Insert: { args: FibInsertArg; reply: {} };
  Erase: { args: NameArg; reply: {} };
  Find: { args: NameArg; reply: FibLookupReply };
}

export interface FibInfo {
  NEntries: Counter;
}

export interface FibInsertArg extends NameArg {
  Nexthops: FaceID[];
  StrategyId?: Uint;
}

export type FibLookupReply = FibLookupReply.No | FibLookupReply.Yes;

export namespace FibLookupReply {
  export interface No {
    HasEntry: false;
  }

  export interface Yes {
    HasEntry: true;
    Name: Name;
    Nexthops: FaceID[];
    StrategyId: Uint;
  }
}
