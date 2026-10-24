import { onBeforeUnmount, onMounted } from 'vue';
// 按需引入:只注册报表用到的 line/bar/pie 与基础组件,比 `import * from 'echarts'`
// 全量引入(约 1MB)小一大截。类型仍从 'echarts' 导入 —— 纯类型导入编译后无运行时成本。
import * as echarts from 'echarts/core';
import { BarChart, LineChart, PieChart } from 'echarts/charts';
import { GridComponent, LegendComponent, TitleComponent, TooltipComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import type { EChartsOption } from 'echarts';

echarts.use([
  LineChart,
  BarChart,
  PieChart,
  TitleComponent,
  TooltipComponent,
  GridComponent,
  LegendComponent,
  CanvasRenderer
]);

export type EChartOption = EChartsOption;

// 无数据时不渲染空坐标轴，给一句居中提示，比光秃秃的网格更清楚
export const EMPTY_OPTION: EChartOption = {
  title: {
    text: '暂无数据',
    left: 'center',
    top: 'middle',
    textStyle: { color: '#C7C7C7', fontSize: 13, fontWeight: 400 }
  }
};

// 按 key 复用：首次 init，之后只 clear + setOption，避免每次刷新都新建实例造成泄漏
const charts: Record<string, echarts.EChartsType> = {};

export function chart(key: string, el: HTMLElement | null | undefined): echarts.EChartsType | null {
  if (!el) return null;
  const existing = charts[key];
  // 复用前必须确认实例还绑在这个 DOM 上:组件被 v-if 卸载后重新挂载时,旧实例绑的是
  // 已分离的 canvas,直接复用会把 clear()+setOption() 画进游离 DOM(可见区域一片空白)。
  // 此时 dispose 旧实例重新 init。
  if (existing) {
    if (existing.getDom() === el) return existing;
    existing.dispose();
    delete charts[key];
  }
  charts[key] = echarts.init(el);
  return charts[key];
}

// resize 用 rAF 节流:拖动窗口时 resize 事件高频触发,逐帧批量处理足够顺滑。
let resizeRafId: number | null = null;
function scheduledResize(): void {
  if (resizeRafId !== null) return;
  resizeRafId = requestAnimationFrame(() => {
    resizeRafId = null;
    resizeAll();
  });
}

export function resizeAll(): void {
  Object.values(charts).forEach(c => c.resize());
}

export function disposeAll(): void {
  Object.values(charts).forEach(c => c.dispose());
  // dispose 后必须清空字典，避免组件重新挂载时复用已销毁实例。
  Object.keys(charts).forEach(key => delete charts[key]);
}

// 消费者引用计数:多个页面同时使用图表时,只有最后一个卸载的才销毁实例字典,
// 避免 A 页卸载把 B 页还挂着的图表 dispose 掉。
let refCount = 0;

/**
 * 图表实例与尺寸自适应统一在此管理：
 * 子组件只负责「数据 -> option」纯转换，通过 chart(key, el) 取实例 setOption。
 * 父壳调用 observeRoot 绑定 ResizeObserver + window.resize，卸载时自动清理。
 */
export function useEChart() {
  // 每个消费者独立的 ResizeObserver:模块级单例会在多页面场景互相覆盖。
  let myRo: ResizeObserver | null = null;

  // 引用计数必须与「实际挂载/卸载」配对。写在 composable 体里的话,keep-alive / HMR
  // 会让计数漂移(挂载路径可能走多次而卸载只走一次),refCount 永远回不到 0,
  // 实例字典也就永不释放。
  onMounted(() => {
    refCount++;
  });

  function observeRoot(root: HTMLElement | null): void {
    if (!root) return;
    if (typeof ResizeObserver !== 'undefined') {
      myRo = new ResizeObserver(scheduledResize);
      myRo.observe(root);
    }
    window.addEventListener('resize', scheduledResize);
  }

  onBeforeUnmount(() => {
    if (myRo) {
      myRo.disconnect();
      myRo = null;
    }
    window.removeEventListener('resize', scheduledResize);
    refCount = Math.max(0, refCount - 1);
    if (refCount === 0) {
      // 最后一个消费者卸载:取消挂起中的 resize 帧,避免卸载后仍回调到已销毁实例
      if (resizeRafId !== null) {
        cancelAnimationFrame(resizeRafId);
        resizeRafId = null;
      }
      disposeAll();
    }
  });

  // 不把 disposeAll 交给消费者:任一组件误调会把别人还在用的图表全部销毁。
  // (需要整体销毁时直接 import 模块级 disposeAll,测试即如此。)
  return { chart, resizeAll, observeRoot };
}
