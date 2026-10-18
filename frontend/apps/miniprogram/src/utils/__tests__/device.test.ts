// 设备信息采集的单测。
//
// mpDeviceInfo 有三条路径:新 API(wx.getDeviceInfo)→ 老回退(getSystemInfoSync)
// → 全失败交空串。三条路径都必须「只读不抛」—— 设备信息拿不到只影响终端
// 分布的精度,绝不能在冷启动埋点这一步炸掉。

import { afterEach, describe, expect, it, vi } from 'vitest';
import { mpDeviceInfo } from '../device';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('mpDeviceInfo 设备信息', () => {
  it('优先走 wx.getDeviceInfo(新 API)', () => {
    vi.stubGlobal('wx', {
      getDeviceInfo: () => ({ system: 'iOS 16.6', platform: 'ios', model: 'iPhone 15 Pro' }),
      getSystemInfoSync: () => {
        throw new Error('不应走到老 API');
      }
    });
    expect(mpDeviceInfo()).toEqual({ system: 'iOS 16.6', platform: 'ios', model: 'iPhone 15 Pro' });
  });

  it('新 API 不存在时回退 getSystemInfoSync', () => {
    vi.stubGlobal('wx', {
      getSystemInfoSync: () => ({ system: 'Android 13', platform: 'android', model: 'Pixel 7' })
    });
    expect(mpDeviceInfo()).toEqual({ system: 'Android 13', platform: 'android', model: 'Pixel 7' });
  });

  it('新老 API 都抛异常时交空串,绝不抛出', () => {
    vi.stubGlobal('wx', {
      getDeviceInfo: () => {
        throw new Error('boom');
      },
      getSystemInfoSync: () => {
        throw new Error('boom');
      }
    });
    expect(mpDeviceInfo()).toEqual({ system: '', platform: '', model: '' });
  });

  it('连 wx 都没有(极简测试宿主)同样交空串', () => {
    vi.stubGlobal('wx', undefined);
    expect(mpDeviceInfo()).toEqual({ system: '', platform: '', model: '' });
  });
});
