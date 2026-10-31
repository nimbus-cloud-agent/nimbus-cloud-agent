import {
  newQuickJSWASMModule,
  newVariant,
  RELEASE_SYNC,
} from "quickjs-emscripten";
import { stripTypeScriptTypes } from "node:module";
import { PtcRuntime } from "@deepseek-ai/dsh-ptc-runtime";

export class WasmRuntime extends PtcRuntime {
  language = "typescript";
  isolation = "wasm";
  get timeout() {
    return { defaultMs: 120_000, maxMs: 600_000 };
  }
  get executionInstructions() {
    return "TypeScript runs in isolated QuickJS WebAssembly. Use only generated SDK bindings. No Node, imports, filesystem, environment, fetch, or subprocesses. Use console.log and return JSON.";
  }
  resolve(request) {
    const ms = request.timeoutMs === undefined ? 120_000 : request.timeoutMs;
    if (!Number.isFinite(ms) || ms <= 0 || ms > 600_000)
      throw Error("PTC timeout must be 1–600000 ms");
    return { ...request, cwd: "/workspace", timeoutMs: ms };
  }
  async run(spec) {
    return runWasm(spec);
  }
}

export async function runWasm(spec) {
  const memory = new WebAssembly.Memory({ initial: 256, maximum: 4096 });
  const module = await newQuickJSWASMModule(
    newVariant(RELEASE_SYNC, { wasmMemory: memory }),
  );
  if (module.getWasmMemory() !== memory)
    throw Error("Wasm memory limit was not applied");
  const runtime = module.newRuntime();
  // Leave room for engine metadata inside the hard 256 MiB linear-memory cap.
  runtime.setMemoryLimit(240 * 1024 * 1024);
  runtime.setMaxStackSize(1024 * 1024);
  const deadline = Date.now() + (spec.timeoutMs ?? 120_000);
  runtime.setInterruptHandler(
    () => Date.now() > deadline || !!spec.signal?.aborted,
  );
  const vm = runtime.newContext();
  const logs = [],
    pending = new Set(),
    deferred = new Set();
  let disposed = false,
    outputSize = 0,
    handle,
    settled;
  const jsonHandle = (value) =>
    vm.unwrapResult(vm.evalCode(`(${JSON.stringify(value ?? null)})`));
  const print = vm.newFunction("log", (...args) => {
    const text = args
      .map((h) => {
        const v = vm.dump(h);
        return typeof v === "string" ? v : JSON.stringify(v);
      })
      .join(" ");
    outputSize += Buffer.byteLength(text);
    if (outputSize > 1048576) throw Error("PTC console output exceeds 1 MiB");
    logs.push(text);
  });
  const consoleObject = vm.newObject();
  for (const name of ["log", "warn", "error", "info"])
    vm.setProp(consoleObject, name, print);
  vm.setProp(vm.global, "console", consoleObject);
  consoleObject.dispose();
  print.dispose();
  for (const ns of spec.bindings ?? []) {
    const object = vm.newObject(vm.null);
    if (ns.errorClass) {
      const { name, memberNameProperty } = ns.errorClass;
      vm.unwrapResult(
        vm.evalCode(
          `globalThis[${JSON.stringify(name)}] = class extends Error { constructor(message, member) { super(message); this.name=${JSON.stringify(name)}; this[${JSON.stringify(memberNameProperty)}]=member; } };`,
        ),
      ).dispose();
    }
    for (const [name, call] of Object.entries(ns.functions)) {
      const fn = vm.newFunction(name, (argument) => {
        const promise = vm.newPromise();
        deferred.add(promise);
        const args = vm.dump(argument);
        const task = Promise.resolve()
          .then(() => call(args))
          .then(
            (value) => {
              if (!disposed) {
                const h = jsonHandle(value);
                promise.resolve(h);
                h.dispose();
              }
            },
            (error) => {
              if (!disposed) {
                const h = ns.errorClass
                  ? vm.unwrapResult(
                      vm.evalCode(
                        `new globalThis[${JSON.stringify(ns.errorClass.name)}](${JSON.stringify(String(error.message))},${JSON.stringify(name)})`,
                      ),
                    )
                  : vm.newError(String(error.message));
                promise.reject(h);
                h.dispose();
              }
            },
          )
          .catch((error) => {
            if (!disposed) {
              const h = vm.newError(String(error.message));
              promise.reject(h);
              h.dispose();
            }
          })
          .finally(() => {
            if (!disposed) promise.dispose();
            deferred.delete(promise);
            pending.delete(task);
          });
        pending.add(task);
        return promise.handle;
      });
      vm.setProp(object, name, fn);
      fn.dispose();
    }
    vm.setProp(vm.global, ns.global, object);
    object.dispose();
  }
  try {
    const check = vm.unwrapResult(
      vm.evalCode(
        'typeof process === "undefined" && typeof require === "undefined" && typeof fetch === "undefined"',
      ),
    );
    const safe = vm.dump(check);
    check.dispose();
    if (!safe) throw Error("Wasm isolation assertion failed");
    const source = stripTypeScriptTypes(`(async()=>{${spec.program}\n})()`, {
      mode: "transform",
    });
    handle = vm.unwrapResult(vm.evalCode(source));
    while (true) {
      if (spec.signal?.aborted) throw Error("aborted");
      if (Date.now() > deadline) throw Error("execution deadline exceeded");
      const jobs = runtime.executePendingJobs();
      if (jobs.error) {
        const e = vm.dump(jobs.error);
        jobs.error.dispose();
        throw Error(e.message || String(e));
      }
      const state = vm.getPromiseState(handle);
      if (state.type !== "pending") {
        settled = state;
        break;
      }
      await Promise.race([
        ...pending,
        new Promise((resolve) => setTimeout(resolve, 2)),
      ]);
    }
    if (settled.type === "rejected") {
      const e = vm.dump(settled.error);
      throw Error(e.message || String(e));
    }
    const value = vm.dump(settled.value);
    if (value !== undefined) JSON.stringify(value); // Reject non-JSON completions, including bigint/cycles.
    return { logs, ...(value === undefined ? {} : { value }) };
  } catch (error) {
    return {
      logs,
      error: {
        kind: spec.signal?.aborted
          ? "abort"
          : Date.now() > deadline
            ? "timeout"
            : "exception",
        message: String(error.message),
      },
    };
  } finally {
    disposed = true;
    for (const promise of deferred) promise.dispose();
    if (handle?.alive) handle.dispose();
    if (settled?.error?.alive) settled.error.dispose();
    if (settled?.value?.alive) settled.value.dispose();
    vm.dispose();
    runtime.dispose();
    // The Harness owns cancellation/draining of in-flight SDK calls. Their callbacks
    // check disposed before touching the now-closed Wasm instance.
  }
}
