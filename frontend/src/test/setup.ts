/// <reference types="vitest/globals" />

import { afterEach } from 'vitest';
import { clearAllStorage } from './mocks';

afterEach(() => {
  clearAllStorage();
  vi.clearAllMocks();
});
