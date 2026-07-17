/// <reference types="vite/client" />

import 'vue-router';

declare module 'vue-router' {
  interface RouteMeta {
    public?: boolean;
    title?: string;
    description?: string;
  }
}

import type { DefineComponent } from 'vue';

declare module 'vue' {
  interface GlobalComponents {
    'altcha-widget': DefineComponent<Record<string, unknown>>;
  }
}
