import { expectTypeOf } from 'vitest';
import type { ApiResponse } from '../../types/api';
import { clearAllStorage, stubMatchMedia } from '../mocks';

describe('vitest smoke', () => {
  it('runs with globals enabled', () => {
    expect(true).toBe(true);
  });

  it('clears local and session storage through shared helper', () => {
    localStorage.setItem('token', 'mock-token');
    sessionStorage.setItem('token', 'mock-token');

    clearAllStorage();

    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });

  it('stubs matchMedia and restores the original implementation', () => {
    const original = window.matchMedia;
    const restore = stubMatchMedia();

    expect(window.matchMedia('(max-width: 600px)').matches).toBe(false);

    restore();
    expect(window.matchMedia).toBe(original);
  });

  it('compiles ApiResponse type assertions', () => {
    type Payload = { id: number };
    const response: ApiResponse<Payload> = {
      code: 200,
      msg: 'ok',
      data: { id: 1 }
    };

    expectTypeOf(response).toMatchTypeOf<ApiResponse<Payload>>();
    expect(response.data.id).toBe(1);
  });
});
