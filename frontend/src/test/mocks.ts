import { reactive, type UnwrapNestedRefs } from 'vue';
import { vi } from 'vitest';
import { AUTH_KEYS } from '../utils/authKeys';

type EChartsMockInstance = {
  setOption: ReturnType<typeof vi.fn>;
  clear: ReturnType<typeof vi.fn>;
  resize: ReturnType<typeof vi.fn>;
  dispose: ReturnType<typeof vi.fn>;
};

export type EChartsMock = {
  init: ReturnType<typeof vi.fn<() => EChartsMockInstance>>;
  use: ReturnType<typeof vi.fn>;
};

export function mockECharts() {
  vi.mock('echarts', () => ({
    init: vi.fn(() => ({
      setOption: vi.fn(),
      clear: vi.fn(),
      resize: vi.fn(),
      dispose: vi.fn()
    })),
    use: vi.fn()
  }));
}

export function mockQrcode() {
  vi.mock('qrcode', () => ({
    default: {
      toDataURL: vi.fn(() => Promise.resolve('data:image/png;base64,mock'))
    },
    toDataURL: vi.fn(() => Promise.resolve('data:image/png;base64,mock'))
  }));
}

export function stubMatchMedia() {
  const original = window.matchMedia;

  window.matchMedia = vi.fn().mockImplementation((query: string): MediaQueryList => {
    const listeners = new Set<(e: MediaQueryListEvent) => void>();
    const mediaQueryList: MediaQueryList = {
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn((listener: (e: MediaQueryListEvent) => void) => {
        listeners.add(listener);
      }),
      removeListener: vi.fn((listener: (e: MediaQueryListEvent) => void) => {
        listeners.delete(listener);
      }),
      addEventListener: vi.fn((_type: string, listener: EventListenerOrEventListenerObject) => {
        if (typeof listener === 'function') {
          listeners.add(listener as (e: MediaQueryListEvent) => void);
        }
      }),
      removeEventListener: vi.fn((_type: string, listener: EventListenerOrEventListenerObject) => {
        if (typeof listener === 'function') {
          listeners.delete(listener as (e: MediaQueryListEvent) => void);
        }
      }),
      dispatchEvent: vi.fn(() => true)
    };

    return mediaQueryList;
  });

  return () => {
    window.matchMedia = original;
  };
}

export function stubResizeObserver() {
  const original = window.ResizeObserver;

  window.ResizeObserver = vi.fn().mockImplementation((): ResizeObserver => {
    return {
      observe: vi.fn(),
      unobserve: vi.fn(),
      disconnect: vi.fn()
    };
  });

  return () => {
    window.ResizeObserver = original;
  };
}

export function flushPromises() {
  return new Promise(resolve => setTimeout(resolve));
}

type MockRoute = {
  path: string;
  name?: string;
  params: Record<string, string>;
  query: Record<string, string>;
  hash: string;
  meta: Record<string, unknown>;
};

export type MockRouterControls = {
  route: UnwrapNestedRefs<MockRoute>;
  push: ReturnType<typeof vi.fn>;
  replace: ReturnType<typeof vi.fn>;
};

export function mockRouter(routeOverrides: Partial<MockRoute> = {}): MockRouterControls {
  const push = vi.fn();
  const replace = vi.fn();
  const route = reactive<MockRoute>({
    path: '/',
    params: {},
    query: {},
    hash: '',
    meta: {},
    ...routeOverrides
  });

  vi.mock('vue-router', () => ({
    useRouter: () => ({
      push,
      replace
    }),
    useRoute: () => route
  }));

  return {
    route,
    push,
    replace
  };
}

export function clearAllStorage() {
  for (const key of AUTH_KEYS) {
    localStorage.removeItem(key);
    sessionStorage.removeItem(key);
  }

  localStorage.clear();
  sessionStorage.clear();
}
