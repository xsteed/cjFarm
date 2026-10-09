import { nextTick, onBeforeUnmount, watch } from 'vue';
import type { Ref } from 'vue';

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])'
].join(',');

/**
 * 底部弹层的无障碍支持:
 *   - 打开:把焦点移入面板(优先第一个可聚焦元素),并在面板内做焦点陷阱(Tab 循环);
 *   - 关闭:焦点归还到打开前的触发元素;
 *   - Esc 关闭(尊重内层控件已消费的 Esc,如正在展开的下拉)。
 *
 * 面板元素需要 tabindex="-1" 以便在无内层可聚焦元素时仍能承接焦点。
 */
export function useSheetA11y(panelRef: Ref<HTMLElement | null>, isOpen: Ref<boolean>, onClose: () => void): void {
  let restoreTo: HTMLElement | null = null;

  function focusableNodes(): HTMLElement[] {
    const panel = panelRef.value;
    if (!panel) return [];
    return Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(el => el.offsetParent !== null);
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      // 内层控件(下拉等)已消费则不再关弹层
      if (!e.defaultPrevented) onClose();
      return;
    }
    if (e.key !== 'Tab') return;
    const nodes = focusableNodes();
    if (!nodes.length) {
      e.preventDefault();
      panelRef.value?.focus();
      return;
    }
    const first = nodes[0];
    const last = nodes[nodes.length - 1];
    const active = document.activeElement;
    if (e.shiftKey && (active === first || active === panelRef.value || active === document.body)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  }

  watch(isOpen, async open => {
    if (open) {
      restoreTo = (document.activeElement as HTMLElement | null) ?? null;
      await nextTick();
      (focusableNodes()[0] ?? panelRef.value)?.focus();
      window.addEventListener('keydown', onKeydown);
    } else {
      window.removeEventListener('keydown', onKeydown);
      restoreTo?.focus?.();
      restoreTo = null;
    }
  });

  onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown));
}
