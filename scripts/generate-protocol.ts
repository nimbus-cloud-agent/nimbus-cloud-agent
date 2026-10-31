import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";
import {
  quicktype,
  InputData,
  JSONSchemaInput,
  JSONSchemaStore,
} from "quicktype-core";
import { format } from "prettier";

// OpenRPC is the only source of method names, DTOs and service signatures.
// quicktype owns cross-language type generation; these small templates only
// connect generated method signatures to the two transport-independent ports.
interface Method {
  name: string;
  params: { name: string; schema: { $ref: string } }[];
  result: { schema: { $ref: string } };
  "x-service": "Runtime" | "Control";
  "x-function": string;
}
interface Document {
  openrpc: string;
  methods: Method[];
  components: { schemas: Record<string, unknown> };
}
process.chdir(fileURLToPath(new URL("..", import.meta.url)));
const document: Document = JSON.parse(
  await readFile("protocol/runtime.openrpc.json", "utf8"),
);
if (document.openrpc !== "1.3.2") throw Error("Unsupported OpenRPC version");
const schemas = JSON.parse(
  JSON.stringify(document.components.schemas).replaceAll(
    "#/components/schemas/",
    "#/definitions/",
  ),
) as Record<string, unknown>;
const schema = {
  $schema: "http://json-schema.org/draft-07/schema#",
  definitions: schemas,
};
const names = Object.keys(schemas);
const typeSchema = {
  ...schema,
  type: "object",
  properties: Object.fromEntries(
    names.map((n) => [n, { $ref: "#/definitions/" + n }]),
  ),
  required: names,
  additionalProperties: false,
};
const input = new JSONSchemaInput(new JSONSchemaStore());
await input.addSource({
  name: "WireTypes",
  schema: JSON.stringify(typeSchema),
});
const data = new InputData();
data.addInput(input);
const banner =
  "// Code generated from protocol/runtime.openrpc.json; DO NOT EDIT.\n";
const ts = await quicktype({
  inputData: data,
  lang: "typescript",
  rendererOptions: {
    "just-types": "true",
    "prefer-unions": "true",
    "nice-property-names": "false",
  },
});
const go = await quicktype({
  inputData: data,
  lang: "go",
  rendererOptions: { "just-types": "true", package: "generated" },
});
const files = new Map<string, string>();
files.set(
  "runtime/src/generated/types.ts",
  banner + ts.lines.join("\n").replace(/\bany\b/g, "unknown"),
);
files.set(
  "internal/runtimeprotocol/generated/types.go",
  banner +
    'package generated\n\nimport "encoding/json"\n\n' +
    go.lines.join("\n").replace(/interface\{\}/g, "json.RawMessage"),
);
files.set(
  "runtime/src/generated/schema.json",
  JSON.stringify(schema, null, 2) + "\n",
);
files.set(
  "internal/runtimeprotocol/generated/schema.go",
  banner +
    "package generated\n\nconst SchemaJSON = " +
    JSON.stringify(JSON.stringify(schema)) +
    "\n",
);
const dto = (ref: string) => ref.split("/").at(-1)!;
const cap = (s: string) => s[0].toUpperCase() + s.slice(1);
let tsSDK =
  banner +
  `import type * as T from './types.js';\nexport interface RpcPort {
 call<P,R>(method:string,input:P,requestType:string,resultType:string,signal?:AbortSignal):Promise<R>;
 handle<P,R>(method:string,requestType:string,resultType:string,handler:(input:P,signal:AbortSignal)=>Promise<R>):void;
}\n`;
let goSDK =
  banner +
  `package generated\nimport("context";"encoding/json";"fmt")
type Caller interface { Call(context.Context,string,any,any)error }
`;
const used = new Set<string>();
for (const service of ["Runtime", "Control"] as const) {
  const methods = document.methods.filter((m) => m["x-service"] === service);
  tsSDK += `export interface ${service}Service {\n`;
  goSDK += `type ${service}Service interface {\n`;
  for (const m of methods) {
    if (used.has(m.name)) throw Error("Duplicate method " + m.name);
    used.add(m.name);
    const f = m["x-function"],
      p = dto(m.params[0].schema.$ref),
      r = dto(m.result.schema.$ref);
    tsSDK += `${f}(input:T.${p},signal:AbortSignal):Promise<T.${r}>;\n`;
    goSDK += `${cap(f)}(context.Context,${p})(${r},error)\n`;
  }
  tsSDK += "}\n";
  goSDK += "}\n";
  tsSDK += `export class ${service}Client { constructor(private readonly port:RpcPort) {}\n`;
  goSDK += `type ${service}Client struct{ Peer Caller }\n`;
  for (const m of methods) {
    const f = m["x-function"],
      p = dto(m.params[0].schema.$ref),
      r = dto(m.result.schema.$ref);
    tsSDK += `${f}(input:T.${p},signal?:AbortSignal):Promise<T.${r}> { return this.port.call(${JSON.stringify(m.name)},input,${JSON.stringify(p)},${JSON.stringify(r)},signal); }\n`;
    goSDK += `func(c ${service}Client) ${cap(f)}(ctx context.Context,input ${p})(out ${r},err error){err=c.Peer.Call(ctx,${JSON.stringify(m.name)},input,&out);return}\n`;
  }
  tsSDK += "}\n";
  tsSDK += `export function register${service}(port:RpcPort,service:${service}Service):void {\n`;
  goSDK += `func Dispatch${service}(ctx context.Context,service ${service}Service,method string,params json.RawMessage)(any,error){switch method{\n`;
  for (const m of methods) {
    const f = m["x-function"],
      p = dto(m.params[0].schema.$ref),
      r = dto(m.result.schema.$ref);
    tsSDK += `port.handle<T.${p},T.${r}>(${JSON.stringify(m.name)},${JSON.stringify(p)},${JSON.stringify(r)},(input,signal)=>service.${f}(input,signal));\n`;
    goSDK += `case ${JSON.stringify(m.name)}:var input ${p};if err:=json.Unmarshal(params,&input);err!=nil{return nil,err};return service.${cap(f)}(ctx,input)\n`;
  }
  tsSDK += "}\n";
  goSDK +=
    'default:return nil,fmt.Errorf("unregistered method: %s",method)}}\n';
}
goSDK +=
  "type Method struct {Request string; Result string; Service string}\nvar Methods=map[string]Method{\n";
for (const m of document.methods)
  goSDK += `${JSON.stringify(m.name)}:{${JSON.stringify(dto(m.params[0].schema.$ref))},${JSON.stringify(dto(m.result.schema.$ref))},${JSON.stringify(m["x-service"])}},\n`;
goSDK += "}\n";
files.set("runtime/src/generated/sdk.ts", tsSDK);
files.set("internal/runtimeprotocol/generated/sdk.go", goSDK);
let stale = false;
for (let [path, content] of files) {
  if (path.endsWith(".ts"))
    content = await format(content, { parser: "typescript" });
  if (path.endsWith(".go"))
    content = execFileSync("gofmt", { input: content, encoding: "utf8" });
  if (process.argv.includes("--check")) {
    const existing = await readFile(path, "utf8").catch(() => null);
    if (existing !== content) {
      console.error("Generated file is stale: " + path);
      stale = true;
    }
  } else {
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, content);
  }
}
if (stale) process.exitCode = 1;
