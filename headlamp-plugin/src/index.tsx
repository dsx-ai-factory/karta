// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 NVIDIA Corporation

import { registerRoute, registerSidebarEntry } from '@kinvolk/headlamp-plugin/lib';
import { useEffect, useState } from 'react';
import { getKartaWasm } from './lib/karta';

registerSidebarEntry({
  parent: null,
  name: 'karta',
  label: 'Karta',
  url: '/karta/workloads',
  icon: 'mdi:graph-outline',
});

function WorkloadsPlaceholder() {
  const [wasmLoaded, setWasmLoaded] = useState(false);
  const [wasmError, setWasmError] = useState<string | null>(null);

  useEffect(() => {
    getKartaWasm()
      .then(() => setWasmLoaded(true))
      .catch(err => setWasmError(err instanceof Error ? err.message : String(err)));
  }, []);

  return (
    <div>
      <p>Karta workloads — coming soon.</p>
      <p>WASM engine: {wasmError ? `unavailable (${wasmError})` : wasmLoaded ? 'ready' : 'loading…'}</p>
    </div>
  );
}

registerRoute({
  path: '/karta/workloads',
  sidebar: 'karta',
  name: 'karta-workloads',
  exact: true,
  component: WorkloadsPlaceholder,
});
