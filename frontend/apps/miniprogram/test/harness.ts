// ============================================================================
// 测试脚手架：把小程序运行时的全局（App / Page / Component / getApp）装成可观测的假实现。
//
// 关键点：
//   ・app 实例直接取 `src/app.ts` 真实调用 App() 时传入的 options，
//     所以「session / cart 的初始形态」被测的就是真代码，不是测试里另写一份。
//   ・页面测试通过动态 import 加载页面模块，捕获它传给 Page() 的 options，
//     再用 makeInstance() 造一个带 setData 的假页面实例（与真机用法一致）。
// ============================================================================
import { wxMock } from './mock-wx';
import type { GlobalData } from '../src/types';

type Plain = Record<string, unknown>;

let appInstance: (GlobalData & Plain) | null = null;
let appPristine = '';
const pageCaptured = new Map<string, Plain>();
const componentCaptured = new Map<string, Plain>();
let pageSink: ((options: Plain) => void) | null = null;
let componentSink: ((options: Plain) => void) | null = null;

/**
 * 页面栈替身。
 * 默认空栈（等价「小程序冷启动」），需要测「上一页是谁」的分支时用 setCurrentPages 摆好。
 */
let pageStack: { route?: string }[] = [];

/** 摆一个假的页面栈，供依赖 getCurrentPages() 的逻辑（如订单页「加菜」）判断上一步去哪。 */
export function setCurrentPages(pages: { route?: string }[]): void {
  pageStack = pages;
}

/** 装好全部全局替身（由 test/setup.ts 在测试文件加载前调用一次）。 */
export function installGlobals(): void {
  wxMock.install();
  const g = globalThis as unknown as Record<string, unknown>;

  g.App = (options: Plain): void => {
    appInstance = options as GlobalData & Plain;
    // 只序列化数据部分（函数会被 JSON.stringify 丢弃），作为「初始状态」快照
    appPristine = JSON.stringify(options);
  };
  g.getApp = (): unknown => appInstance;
  g.Page = (options: Plain): void => {
    pageSink?.(options);
  };
  g.Component = (options: Plain): void => {
    componentSink?.(options);
  };
  g.getCurrentPages = (): unknown[] => pageStack;
}

/** 加载 app.ts（触发 App() 与 api/client 的装配），返回 app 实例。 */
export async function bootApp(): Promise<GlobalData & Plain> {
  if (!appInstance) await import('../src/app');
  if (!appInstance) throw new Error('src/app.ts 没有调用 App()');
  return appInstance;
}

/** 把 app 实例的数据恢复到 app.ts 里声明的初始值。 */
export function resetApp(): void {
  if (!appInstance || !appPristine) return;
  const pristine = JSON.parse(appPristine) as Plain;
  Object.assign(appInstance, pristine);
}

/** 动态加载一个页面模块并捕获它传给 Page() 的 options（同一 key 只加载一次）。 */
export async function loadPage(key: string, loader: () => Promise<unknown>): Promise<Plain> {
  const cached = pageCaptured.get(key);
  if (cached) return cached;
  let captured: Plain | null = null;
  pageSink = options => {
    captured = options;
  };
  try {
    await loader();
  } finally {
    pageSink = null;
  }
  if (!captured) throw new Error(`页面模块没有调用 Page(): ${key}`);
  pageCaptured.set(key, captured);
  return captured;
}

/**
 * 页面实例的假实现：data 深拷贝、setData 合并并记账。
 *
 * 用索引签名 + any 是刻意的：页面 options 上挂着几十个方法，逐个声明类型只会让
 * 测试脚手架变成第二份需要维护的实现。这里只保证「data / setData 的行为与真机一致」。
 */
export interface PageInstance {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any;
  data: Plain;
  setDataCalls: Plain[];
  /** triggerEvent 的记账（组件测试用；页面测试不涉及） */
  events: { name: string; detail?: unknown }[];
  triggerEvent(name: string, detail?: unknown): void;
  /** 节点查询桩：exec 的回调收到空数组，依赖测量的逻辑会自行跳过 */
  createSelectorQuery(): unknown;
  setData(patch: Plain, cb?: () => void): void;
}

export function makeInstance(options: Plain): PageInstance {
  // createSelectorQuery 的链式桩：select/selectAll 返回同一个 chain，怎么点都不会抛
  const chain: Record<string, unknown> = {};
  chain.boundingClientRect = () => chain;
  const query = {
    select: () => chain,
    selectAll: () => chain,
    exec: (cb?: (res: unknown[]) => void) => cb?.([])
  };

  const inst: PageInstance = {
    ...options,
    data: JSON.parse(JSON.stringify(options.data)) as Plain,
    setDataCalls: [],
    events: [],
    triggerEvent(name: string, detail?: unknown): void {
      inst.events.push({ name, detail });
    },
    createSelectorQuery: () => query,
    setData(patch: Plain, cb?: () => void): void {
      inst.setDataCalls.push(patch);
      Object.assign(inst.data, patch);
      cb?.();
    }
  };
  return inst;
}

/** 便捷：加载页面并直接造一个实例。 */
export async function mountPage(key: string, loader: () => Promise<unknown>): Promise<PageInstance> {
  return makeInstance(await loadPage(key, loader));
}

/** 动态加载一个自定义组件模块并捕获它传给 Component() 的 options（同一 key 只加载一次）。 */
export async function loadComponent(key: string, loader: () => Promise<unknown>): Promise<Plain> {
  const cached = componentCaptured.get(key);
  if (cached) return cached;
  let captured: Plain | null = null;
  componentSink = options => {
    captured = options;
  };
  try {
    await loader();
  } finally {
    componentSink = null;
  }
  if (!captured) throw new Error(`组件模块没有调用 Component(): ${key}`);
  componentCaptured.set(key, captured);
  return captured;
}

/**
 * 造一个组件实例。与真机对齐的三件事：
 *   ・methods 摊平到实例上（真机上 `this.onXxx` 就是这么来的）；
 *   ・传入的 properties 并入 data（真机上 properties 也出现在 data 里）；
 *   ・observers 留在 `inst.observers` 上，用 fireObserver() 手动跑（真机是属性变化自动触发）。
 */
export async function mountComponent(
  key: string,
  loader: () => Promise<unknown>,
  props: Plain = {}
): Promise<PageInstance> {
  const options = await loadComponent(key, loader);
  const inst = makeInstance(options);
  Object.assign(inst, (options.methods as Plain) ?? {});
  Object.assign(inst.data, props);
  return inst;
}

/** 手动跑一次组件的 observer（真机上由属性变化自动触发）。 */
export function fireObserver(inst: PageInstance, key: string, ...args: unknown[]): void {
  const observers = inst.observers as Record<string, (...a: unknown[]) => void> | undefined;
  observers?.[key]?.call(inst, ...args);
}
