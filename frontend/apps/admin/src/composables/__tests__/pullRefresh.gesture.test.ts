import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { defineComponent } from 'vue';
import { mount, flushPromises } from '@vue/test-utils';
import { usePullRefresh, readyDistanceFor, shouldTriggerRefresh } from '../usePullRefresh';

/** jsdom 没有 Touch 构造器,用带 touches 的 Event 代替 */
function fireTouch(el: Element, type: string, y?: number): Event {
  const ev = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(ev, 'touches', { value: y === undefined ? [] : [{ clientY: y }] });
  el.dispatchEvent(ev);
  return ev;
}

describe('usePullRefresh 手势', () => {
  let scroller: HTMLDivElement;
  let onRefresh: ReturnType<typeof vi.fn>;
  let unmount: (() => void) | null = null;

  beforeEach(() => {
    scroller = document.createElement('div');
    scroller.className = 'content';
    document.body.appendChild(scroller);
    onRefresh = vi.fn(() => Promise.resolve());
  });

  afterEach(() => {
    unmount?.();
    unmount = null;
    scroller.remove();
  });

  function mountComposable(): void {
    const wrapper = mount(
      defineComponent({
        setup() {
          usePullRefresh(onRefresh);
          return () => null;
        }
      })
    );
    unmount = () => wrapper.unmount();
  }

  it('下拉超过阈值后松手会触发刷新', async () => {
    mountComposable();
    fireTouch(scroller, 'touchstart', 100);
    fireTouch(scroller, 'touchmove', 250);
    fireTouch(scroller, 'touchend');
    await flushPromises();
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it('接管下拉后阻止原生滚动,避免和指示器抢手势', () => {
    mountComposable();
    fireTouch(scroller, 'touchstart', 100);
    const move = fireTouch(scroller, 'touchmove', 250);
    expect(move.defaultPrevented).toBe(true);
  });

  it('未达阈值松手不刷新', async () => {
    mountComposable();
    fireTouch(scroller, 'touchstart', 100);
    fireTouch(scroller, 'touchmove', 120);
    fireTouch(scroller, 'touchend');
    await flushPromises();
    expect(onRefresh).not.toHaveBeenCalled();
  });

  it('松手前手指轻微回抬仍按本次手势的最大距离刷新', async () => {
    mountComposable();
    fireTouch(scroller, 'touchstart', 100);
    fireTouch(scroller, 'touchmove', 300);
    fireTouch(scroller, 'touchmove', 140);
    fireTouch(scroller, 'touchend');
    await flushPromises();
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it('touchcancel 只收起指示器,不刷新', async () => {
    mountComposable();
    fireTouch(scroller, 'touchstart', 100);
    fireTouch(scroller, 'touchmove', 300);
    fireTouch(scroller, 'touchcancel');
    await flushPromises();
    expect(onRefresh).not.toHaveBeenCalled();
  });

  it('列表先滚到顶、同一手势继续下拉也能刷新', async () => {
    mountComposable();
    scroller.scrollTop = 120;
    fireTouch(scroller, 'touchstart', 500);
    // 先往上划把列表滚回顶部(scrollTop 归零),此时还不该接管
    scroller.scrollTop = 0;
    fireTouch(scroller, 'touchmove', 300);
    fireTouch(scroller, 'touchmove', 500);
    fireTouch(scroller, 'touchend');
    await flushPromises();
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it('提示「松开立即刷新」的距离就是会刷新的距离', () => {
    const ready = readyDistanceFor();
    expect(shouldTriggerRefresh(ready)).toBe(true);
    expect(shouldTriggerRefresh(ready - 0.1)).toBe(false);
  });
});
