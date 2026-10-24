import { hasPerm } from '@dining/auth';
import { NO_PERMISSION_PATH } from './routes';

// ============================================================================
// 侧栏菜单派生
//
// 菜单不单独维护一份数组,而是从**路由表**里筛出来:
//   meta.menuOrder 有值 → 进侧栏;meta.perm 不满足 → 不显示。
// 这样「加一个页面」只需加一条路由,菜单、权限过滤、默认落地页自动跟上。
//
// 路由表由调用方传入(通常传 `router.getRoutes()`,它给出的是扁平化后的绝对路径),
// 使这个模块保持纯函数、可测,也不依赖具体路由实例。
// ============================================================================

/** 侧栏叶子菜单(由路由表派生的一条记录)。 */
export interface MenuLeaf {
  path: string;
  title: string;
  icon: string;
  menuOrder: number;
}

/** 侧栏节点:单条目(path)直接渲染为普通菜单项;多条目(children)渲染为可折叠分组。 */
export interface MenuNode {
  /** 稳定标识:分组 key 或(未分组条目的)路由路径。 */
  key: string;
  title: string;
  icon: string;
  path?: string;
  children?: MenuLeaf[];
}

/** 派生菜单所需的最小路由形状(兼容 RouteRecordRaw 与 Router.getRoutes() 的返回)。 */
export interface RouteLike {
  path: string;
  meta?: {
    title?: string;
    perm?: string;
    icon?: string;
    menuOrder?: number;
    group?: string;
  };
}

/**
 * 侧栏菜单分组:把同类的低频入口收进可折叠子菜单,一级菜单从 15 行压到 7 行。
 * 分组在侧栏的先后由「组内最小 menuOrder」决定(见 menuUnits),不是数组顺序;
 * 组内条目按各自 menuOrder 排序。
 * 分组只影响「展示」:权限过滤仍在每条路由的 meta.perm 上,路由守卫不变;
 * 某组只剩一个可见条目时,侧栏会把它退化成普通菜单项,不多套一层折叠。
 */
const MENU_GROUPS: ReadonlyArray<{ key: string; title: string; icon: string }> = [
  { key: 'order', title: '订单中心', icon: 'OrderAdjustmentColumnIcon' },
  { key: 'dish', title: '菜品中心', icon: 'RiceIcon' },
  { key: 'print', title: '打印中心', icon: 'PrintIcon' },
  { key: 'system', title: '系统管理', icon: 'SettingIcon' }
];

interface MenuEntry {
  leaf: MenuLeaf;
  group?: string;
}

/** 全部「有权限访问」的菜单叶子(已按权限过滤,保留分组标记)。 */
function accessibleEntries(routes: readonly RouteLike[]): MenuEntry[] {
  return (
    routes
      // menuOrder === 0 也是合法排序值,用 != null 而不是真值判断
      .filter(r => r.meta?.menuOrder != null && (!r.meta.perm || hasPerm(r.meta.perm)))
      .map(r => ({
        leaf: {
          path: r.path,
          title: r.meta?.title ?? '',
          icon: r.meta?.icon ?? '',
          menuOrder: r.meta?.menuOrder ?? 0
        },
        group: r.meta?.group
      }))
  );
}

/**
 * 把叶子按「分组」聚合成侧栏的一行单元:有分组的收进一组,无分组的各自一行;
 * 行序 = 组内最小 menuOrder(仪表盘 1 → 订单中心 2 → …),组内按 menuOrder 排。
 * 分组只影响展示:组里只剩一个可见条目时整组退化成普通菜单项,不多套一层。
 */
function menuUnits(routes: readonly RouteLike[]): Array<{ key: string; entries: MenuEntry[] }> {
  const all = accessibleEntries(routes);
  const byGroup = new Map<string, MenuEntry[]>();
  const singles: MenuEntry[] = [];
  for (const e of all) {
    if (e.group) {
      const arr = byGroup.get(e.group) ?? [];
      arr.push(e);
      byGroup.set(e.group, arr);
    } else {
      singles.push(e);
    }
  }
  const known = MENU_GROUPS.filter(g => byGroup.has(g.key)).map(g => ({ key: g.key, entries: byGroup.get(g.key)! }));
  // 路由 meta 里写了 group 但没在 MENU_GROUPS 登记的分组:不静默丢菜单,兜底成一单元。
  const unknown = [...byGroup.keys()].filter(k => !MENU_GROUPS.some(g => g.key === k));
  const units = [
    ...known,
    ...unknown.map(k => ({ key: k, entries: byGroup.get(k)! })),
    ...singles.map(e => ({ key: e.leaf.path, entries: [e] }))
  ];
  const minOrder = (u: { entries: MenuEntry[] }) => Math.min(...u.entries.map(e => e.leaf.menuOrder));
  units.sort((a, b) => minOrder(a) - minOrder(b));
  for (const u of units) {
    u.entries.sort((a, b) => a.leaf.menuOrder - b.leaf.menuOrder);
  }
  return units;
}

/** 侧栏菜单树(分组 + 单条目),供 AdminLayout 渲染折叠菜单。 */
export function menuTree(routes: readonly RouteLike[]): MenuNode[] {
  return menuUnits(routes).map(u => {
    const first = u.entries[0].leaf;
    if (u.entries.length === 1) {
      return { key: u.key, title: first.title, icon: first.icon, path: first.path };
    }
    const g = MENU_GROUPS.find(x => x.key === u.key);
    return {
      key: u.key,
      title: g?.title ?? first.title,
      icon: g?.icon ?? first.icon,
      children: u.entries.map(e => e.leaf)
    };
  });
}

/** 侧栏菜单的扁平叶子列表(含分组内条目),用于底部 tabbar 过滤与默认落地页。 */
export function accessibleMenus(routes: readonly RouteLike[]): MenuLeaf[] {
  return menuUnits(routes).flatMap(u => u.entries.map(e => e.leaf));
}

/** 第一个「有权限访问」的菜单路径;一个都没有时返回兜底页路径。 */
export function firstAccessiblePath(routes: readonly RouteLike[]): string {
  const menu = accessibleMenus(routes);
  return menu.length ? menu[0].path : NO_PERMISSION_PATH;
}
