// vitest setupFiles：每个测试文件加载前装好小程序全局，并在每个用例前重置状态。
import { beforeEach } from 'vitest';
import { installGlobals, resetApp } from './harness';
import { wxMock } from './mock-wx';

installGlobals();

beforeEach(() => {
  wxMock.reset();
  resetApp();
});
