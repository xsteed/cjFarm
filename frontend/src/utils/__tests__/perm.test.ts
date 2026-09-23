import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { getProfile } from '../../api';
import {
  restorePerms,
  setAuth,
  clearAuth,
  hasToken,
  tokenExpired,
  permsLoaded,
  hasPerm,
  hasAnyPerm,
  getUsername,
  getDisplayName,
  getRoleName,
  getRoleKey,
  refreshProfile
} from '../perm';
import { LS_TOKEN, LS_PERMS } from '../authKeys';

vi.mock('../../api', () => ({
  getProfile: vi.fn()
}));

describe('perm 登录态与权限', () => {
  beforeEach(() => {
    // 模块级单例（ref），每个用例前从 localStorage 恢复（无缓存时回落到 null）
    restorePerms();
  });

  afterEach(() => {
    // 清空登录态并把 permSet 复位为 null，避免用例间串状态
    clearAuth();
  });

  it('无缓存时 restorePerms 后 permsLoaded 为 false（permSet = null）', () => {
    expect(permsLoaded()).toBe(false);
    expect(hasPerm('any')).toBe(false);
    expect(hasAnyPerm(['any'])).toBe(false);
  });

  it('setAuth 写入后 hasPerm 正确', () => {
    setAuth({ token: 't', username: 'u', perms: ['perm:a', 'perm:b'] });

    expect(permsLoaded()).toBe(true);
    expect(hasPerm('perm:a')).toBe(true);
    expect(hasPerm('perm:b')).toBe(true);
    expect(hasPerm('perm:c')).toBe(false);
    expect(localStorage.getItem(LS_PERMS)).toBe('["perm:a","perm:b"]');
    expect(localStorage.getItem(LS_TOKEN)).toBe('t');
  });

  it('clearAuth 后回到 null', () => {
    setAuth({ token: 't', username: 'u', perms: ['perm:a'] });
    clearAuth();

    expect(permsLoaded()).toBe(false);
    expect(hasPerm('perm:a')).toBe(false);
    expect(localStorage.getItem(LS_TOKEN)).toBeNull();
  });

  it('空 perms 数组区分 null 与空集：permsLoaded=true 但 hasPerm 全 false', () => {
    setAuth({ token: 't', username: 'u', perms: [] });

    expect(permsLoaded()).toBe(true);
    expect(hasPerm('perm:a')).toBe(false);
    expect(hasAnyPerm(['perm:a'])).toBe(false);
  });

  it('hasAnyPerm 多码判断与空码边界', () => {
    setAuth({ perms: ['a'] });

    expect(hasAnyPerm(['b', 'a'])).toBe(true);
    expect(hasAnyPerm(['b', 'c'])).toBe(false);
    expect(hasAnyPerm([])).toBe(true);
    expect(hasPerm('')).toBe(true);
  });

  it('refreshProfile 调 getProfile 并覆盖缓存', async () => {
    const profile = {
      username: 'u1',
      realName: '张三',
      roleKey: 'admin',
      roleName: '管理员',
      perms: ['perm:x']
    };
    vi.mocked(getProfile).mockResolvedValue(profile);

    const result = await refreshProfile();

    expect(vi.mocked(getProfile)).toHaveBeenCalledTimes(1);
    expect(result).toBe(profile);
    expect(hasPerm('perm:x')).toBe(true);
    expect(getUsername()).toBe('u1');
    expect(getDisplayName()).toBe('张三');
    expect(getRoleName()).toBe('管理员');
    expect(getRoleKey()).toBe('admin');
  });

  it('hasToken 与 getDisplayName 兜底', () => {
    expect(hasToken()).toBe(false);
    expect(getDisplayName()).toBe('未登录');

    setAuth({ token: 't', username: 'u2' });
    expect(hasToken()).toBe(true);
    expect(getDisplayName()).toBe('u2');
  });

  it('tokenExpired 未来令牌返回 false、过期令牌返回 true', () => {
    const future = Math.floor(Date.now() / 1000) + 3600;
    const past = Math.floor(Date.now() / 1000) - 3600;
    const makeToken = (exp: number) => btoa(`user|1|1|${exp}`) + '.sig';

    localStorage.setItem(LS_TOKEN, makeToken(future));
    expect(tokenExpired()).toBe(false);

    localStorage.setItem(LS_TOKEN, makeToken(past));
    expect(tokenExpired()).toBe(true);
  });

  it('tokenExpired 无令牌返回 true', () => {
    expect(tokenExpired()).toBe(true);
  });
});
