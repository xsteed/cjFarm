import { describe, it, expect, afterEach, vi } from 'vitest';

type ChangeListener = (e: MediaQueryListEvent) => void;

// 可控 matchMedia stub：按 query 返回 matches，并记录 change 监听器以便手动触发。
// 注意:useMobile 的 syncMatches 收到 change 后是「全量重查 matchMedia」而非使用
// event.matches,因此 trigger 必须先更新查询状态再派发事件,否则断言读到的仍是旧值。
function stubMatchMedia(initial: (query: string) => boolean, legacy = false) {
  const listeners = new Map<string, Set<ChangeListener>>();
  const state = new Map<string, boolean>();
  const matcher = (query: string) => (state.has(query) ? state.get(query)! : initial(query));
  const original = window.matchMedia;

  const add = (query: string, listener: ChangeListener) => {
    let set = listeners.get(query);
    if (!set) {
      set = new Set();
      listeners.set(query, set);
    }
    set.add(listener);
  };

  window.matchMedia = vi.fn().mockImplementation((query: string): MediaQueryList => {
    const mql = {
      matches: matcher(query),
      media: query,
      onchange: null,
      addListener: vi.fn((listener: (e: MediaQueryListEvent) => void) => {
        add(query, listener);
      }),
      removeListener: vi.fn(),
      addEventListener: legacy
        ? undefined
        : vi.fn((_type: string, listener: EventListenerOrEventListenerObject) => {
            if (typeof listener === 'function') {
              add(query, listener as ChangeListener);
            }
          }),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(() => true)
    } as unknown as MediaQueryList;
    return mql;
  });

  return {
    restore: () => {
      window.matchMedia = original;
    },
    trigger: (query: string, matches: boolean) => {
      state.set(query, matches);
      const set = listeners.get(query);
      if (!set) return;
      const event = { matches, media: query } as MediaQueryListEvent;
      for (const cb of set) cb(event);
    }
  };
}

describe('useMobile 响应式断点', () => {
  let restore: (() => void) | undefined;

  afterEach(() => {
    restore?.();
    restore = undefined;
  });

  it('三断点初始值按 matchMedia 结果计算', async () => {
    vi.resetModules();
    const stub = stubMatchMedia(q => {
      if (q === '(max-width: 360px)') return false;
      if (q === '(max-width: 1024px)') return true;
      if (q === '(min-width: 768px) and (max-width: 1024px)') return true;
      return false;
    });
    restore = stub.restore;

    const { useIsMobile } = await import('../useMobile');
    const { isMobile, isTablet, isNarrow } = useIsMobile();

    expect(isNarrow.value).toBe(false);
    expect(isMobile.value).toBe(true);
    expect(isTablet.value).toBe(true);
  });

  it('监听器注册并在 change 触发后更新', async () => {
    vi.resetModules();
    const stub = stubMatchMedia(() => false);
    restore = stub.restore;

    const { useIsMobile } = await import('../useMobile');
    const { isMobile, isTablet, isNarrow } = useIsMobile();

    expect(isNarrow.value).toBe(false);
    expect(isMobile.value).toBe(false);
    expect(isTablet.value).toBe(false);

    stub.trigger('(max-width: 1024px)', true);
    expect(isMobile.value).toBe(true);

    stub.trigger('(min-width: 768px) and (max-width: 1024px)', true);
    expect(isTablet.value).toBe(true);

    stub.trigger('(max-width: 360px)', true);
    expect(isNarrow.value).toBe(true);
  });

  it('无 addEventListener 时走 addListener 兜底', async () => {
    vi.resetModules();
    const stub = stubMatchMedia(() => false, true);
    restore = stub.restore;

    const { useIsMobile } = await import('../useMobile');
    const { isMobile } = useIsMobile();

    expect(isMobile.value).toBe(false);

    stub.trigger('(max-width: 1024px)', true);
    expect(isMobile.value).toBe(true);
  });
});
