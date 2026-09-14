// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

import { ApiProxy } from '@kinvolk/headlamp-plugin/lib';

const PLUGIN_NAME = 'karta';
const WASM_EXPORTS_TIMEOUT_MS = 3000;

export interface Envelope {
  data: string | null;
  error: string | null;
}

/**
 * The raw bindings the WASM module sets on window.karta. Arguments and results
 * are JSON strings; kartaUtil wraps them in typed calls.
 */
export interface KartaWasm {
  /** Builds the workload tree. Returns a WorkloadTree, including the root status. */
  buildTree(definitionJSON: string, workloadJSON: string): Envelope;
  /** Returns the definitions built into the module, as Karta[]. */
  listCatalog(): Envelope;
}

interface GoRuntime {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
}

declare global {
  interface Window {
    Go?: new () => GoRuntime;
    karta?: KartaWasm;
  }
}

function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.src = src;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error(`fetch ${src}: failed to load`));
    document.head.appendChild(script);
  });
}

// Exported so it can be unit-tested directly, without needing to also mock
// the rest of instantiate()'s WASM-loading flow.
export async function loadScriptViaApiProxy(path: string): Promise<void> {
  const resp = (await ApiProxy.request(path, { isJSON: false }, false, false)) as Response;
  const text = await resp.text();
  const blobUrl = URL.createObjectURL(new Blob([text], { type: 'application/javascript' }));
  try {
    await loadScript(blobUrl);
  } finally {
    URL.revokeObjectURL(blobUrl);
  }
}

async function findPluginBase(): Promise<string> {
  try {
    const list = (await ApiProxy.request('/plugins', {}, false, false)) as {
      path?: string;
      name?: string;
    }[];
    const entry = list.find(item => item.name === PLUGIN_NAME);
    if (entry?.path) {
      return entry.path;
    }
  } catch {
    // Fall through to the conventional path.
  }
  return `plugins/${PLUGIN_NAME}`;
}

function isKartaLoaded(karta?: KartaWasm): karta is KartaWasm {
  return !!karta;
}

async function waitForExports(): Promise<KartaWasm> {
  const deadline = Date.now() + WASM_EXPORTS_TIMEOUT_MS;
  while (Date.now() < deadline) {
    if (isKartaLoaded(window.karta)) {
      return window.karta;
    }
    await new Promise(resolve => setTimeout(resolve, 10));
  }
  throw new Error('the WebAssembly module did not register its exports');
}

async function instantiate(): Promise<KartaWasm> {
  const base = await findPluginBase();

  await loadScriptViaApiProxy(`/${base}/wasm_exec.js`);
  if (!window.Go) {
    throw new Error('wasm_exec.js did not define the Go runtime');
  }

  const go = new window.Go();
  const wasmResp = (await ApiProxy.request(
    `/${base}/karta.wasm`,
    { isJSON: false },
    false,
    false
  )) as Response;

  let instance: WebAssembly.Instance;
  try {
    ({ instance } = await WebAssembly.instantiateStreaming(wasmResp, go.importObject));
  } catch (err) {
    console.error('karta: failed to instantiate the WASM module', err);
    throw err;
  }

  go.run(instance);
  return waitForExports();
}

let kartaWasmPromise: Promise<KartaWasm> | null = null;

export function getKartaWasm(): Promise<KartaWasm> {
  if (!kartaWasmPromise) {
    kartaWasmPromise = instantiate().catch(err => {
      kartaWasmPromise = null;
      throw err;
    });
  }
  return kartaWasmPromise;
}
