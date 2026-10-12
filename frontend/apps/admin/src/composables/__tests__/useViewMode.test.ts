import { describe, it, expect, beforeEach, vi } from 'vitest';
import { nextTick } from 'vue';
import { useViewMode } from '../useViewMode';
import { setAuth } from '@dining/auth';
import { clearAllStorage } from '@shell/test/mocks.ts';

// @dining/api 被 @dining/auth 依赖(refreshProfile),这里不需要真实 http,统一 mock 掉。
vi.mock('@dining/api', () => ({
  // @dining/auth 在模块加载时会注册它(见 perm.ts 末尾),漏了会让整个套件加载失败
  onAuthCleared: vi.fn(),
  getProfile: vi.fn(),
  logoutRemember: vi.fn()
}));

describe('useViewMode 视图偏好缓存', () => {
  beforeEach(() => {
    clearAllStorage();
    setAuth({ token: 't', username: 'zhangsan', perms: [] });
  });

  it('无缓存时回落到默认视图', () => {
    expect(useViewMode('dishes').value).toBe('card');
    expect(useViewMode('dishes', 'table').value).toBe('table');
  });

  it('切换后写入缓存，重新读取时沿用上次的选择', async () => {
    const vm = useViewMode('dishes');
    expect(vm.value).toBe('card');

    vm.value = 'table';
    await nextTick();

    // 模拟「刷新页面 / 换个菜单再回来」:重新走一次初始化
    expect(useViewMode('dishes').value).toBe('table');
  });

  it('按用户名隔离：换一个人登录不会被上一个人的选择带偏', async () => {
    const vm = useViewMode('dishes');
    vm.value = 'table';
    await nextTick();

    setAuth({ token: 't2', username: 'lisi', perms: [] });
    expect(useViewMode('dishes').value).toBe('card');
  });

  it('按页面隔离：菜品页切列表不影响桌台页', async () => {
    const vm = useViewMode('dishes');
    vm.value = 'table';
    await nextTick();

    expect(useViewMode('tables').value).toBe('card');
  });

  it('缓存值失效（老版本残留/被手改）时回落默认，不会渲染出空白视图', () => {
    localStorage.setItem('admin_view_mode:zhangsan:dishes', 'grid');
    expect(useViewMode('dishes').value).toBe('card');
  });
});
