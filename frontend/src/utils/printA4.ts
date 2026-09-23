// A4 票据排版与浏览器打印。
//
// 与小票机通道（/order/reprint）的区别：
//   热敏小票走打印服务真实出纸，纸宽固定 58/80mm，版式受限于纸宽；
//   A4 是「浏览器打印页」，由服务员手动出纸或另存 PDF，适合
//   需要存档 / 交给顾客签字 / 后厨传菜留存等场景。
//   两者是两条独立通道，不要合并：合并后调用方无法预知到底出什么纸。
//
// 版式复刻 utils/tableQr.js 的做法：拼 HTML 字符串 -> 新窗口 write -> @page A4 -> window.print()。

import { openPrintWindow } from './tableQr';
import { ORDER_STATUS } from '../constants/dicts';
import type { OrderDetail, PublicConfig } from '../types/entities';

const HTML_ESCAPE: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;'
};

const esc = (v: unknown): string => String(v == null ? '' : v).replace(/[&<>"']/g, c => HTML_ESCAPE[c]);

const money = (v: unknown): string => Number(v || 0).toFixed(2);

/** 状态文案直接复用 dicts 里的枚举，避免纸质单据与列表显示不一致 */

/** 单据标题 + 是否展示金额 */
const DOC_META: Record<string, { title: string; subtitle: string; withPrice: boolean; foot: string }> = {
  kitchen: { title: '厨房单', subtitle: 'KITCHEN ORDER', withPrice: false, foot: '请按单出菜，出齐后划掉已上菜品' },
  guest: { title: '结账单', subtitle: 'GUEST BILL', withPrice: true, foot: '谢谢惠顾，欢迎再次光临！' }
};

interface A4PrintOptions {
  shopName?: string;
  reprint?: boolean;
  printTime?: string;
  getPublicConfig?: () => Promise<PublicConfig>;
}

function itemRows(order: OrderDetail, withPrice: boolean): string {
  const items = order.items || [];
  if (!items.length) return '<tr class="empty"><td colspan="4">无菜品明细</td></tr>';
  return items
    .map(it => {
      const spec = it.specName ? `<div class="spec">规格：${esc(it.specName)}</div>` : '';
      const remark = it.itemRemark ? `<div class="remark">备注：${esc(it.itemRemark)}</div>` : '';
      const priceCell = withPrice ? `<td class="c">¥${money(it.price)}</td>` : '';
      const amountCell = withPrice ? `<td class="c">¥${money(it.amount)}</td>` : '';
      return `<tr>
        <td><div class="dish">${esc(it.dishName)}</div>${spec}${remark}</td>
        ${priceCell}
        <td class="c qty">${esc(it.quantity)}</td>
        ${amountCell}
      </tr>`;
    })
    .join('');
}

/**
 * 生成 A4 单据 HTML。
 * @param order 订单详情（getOrder 返回的完整对象）
 * @param docType 单据类型
 * @param opt shopName 店铺名 / reprint 是否补打
 */
export function buildA4Html(order: OrderDetail, docType: 'kitchen' | 'guest', opt: A4PrintOptions = {}): string {
  const meta = DOC_META[docType] || DOC_META.guest;
  const withPrice = meta.withPrice;
  const headCols = withPrice
    ? '<tr><th>菜品</th><th class="c">单价</th><th class="c">数量</th><th class="c">小计</th></tr>'
    : '<tr><th>菜品</th><th class="c">数量</th></tr>';

  const tableLabel = order.tableName ? `${order.tableNo}号桌 · ${order.tableName}` : `${order.tableNo}号桌`;
  const people = order.personCount ? `${order.personCount} 人` : '-';
  const statusText = order.orderStatus != null ? ORDER_STATUS[order.orderStatus]?.label : undefined;
  const discountRow =
    order.discountAmount && order.discountAmount > 0
      ? `<div class="sum"><span>优惠</span><b>-¥${money(order.discountAmount)}</b></div>`
      : '';

  const sumRows = withPrice
    ? `<div class="sum"><span>菜品金额</span><b>¥${money(order.dishAmount)}</b></div>
       <div class="sum"><span>餐位费</span><b>¥${money(order.seatFee)}</b></div>
       ${discountRow}
       <div class="sum total"><span>应收金额</span><b>¥${money(order.totalAmount)}</b></div>
       ${order.payStatus === 1 ? `<div class="sum"><span>已支付</span><b>¥${money(order.paidAmount)}</b></div>` : ''}`
    : '';

  const notes: string[] = [];
  if (order.orderRemark) notes.push(`整单备注：${order.orderRemark}`);
  if (order.settleType === 'free' && order.settleRemark) notes.push(`免单原因：${order.settleRemark}`);
  if (order.settleType === 'credit' && order.settleRemark) notes.push(`挂账人/备注：${order.settleRemark}`);
  if (order.cancelReason) notes.push(`取消原因：${order.cancelReason}`);
  const notesHtml = notes.length ? `<div class="notes">${notes.map(n => `<div>${esc(n)}</div>`).join('')}</div>` : '';

  return `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>${meta.title} ${esc(order.orderNo || '')}</title>
<style>
  * { -webkit-print-color-adjust: exact; print-color-adjust: exact; box-sizing: border-box; }
  @page { size: A4; margin: 12mm; }
  html,body { margin:0; padding:0; background:#f3f4f6; font-family:"PingFang SC","Microsoft YaHei",system-ui,sans-serif; color:#1f2329; }
  .toolbar { position:fixed; left:0; right:0; top:0; background:#fff; border-bottom:1px solid #e6e8eb; padding:10px 16px;
    display:flex; align-items:center; gap:12px; z-index:9; box-shadow:0 2px 8px rgba(0,0,0,.06); }
  .toolbar b { font-size:14px; }
  .toolbar span { font-size:12px; color:#8a919f; }
  .toolbar button { margin-left:auto; background:#F0481F; color:#fff; border:0; border-radius:6px; padding:8px 18px; font-size:13px; cursor:pointer; }
  .spacer { height:52px; }
  .sheet { width:186mm; margin:0 auto; background:#fff; padding:14mm 12mm; box-shadow:0 2px 10px rgba(0,0,0,.06); }
  .shop { text-align:center; font-size:20pt; font-weight:700; letter-spacing:1px; }
  .doctitle { text-align:center; margin-top:4mm; }
  .doctitle b { font-size:16pt; border-bottom:2px solid #1f2329; padding-bottom:1mm; }
  .doctitle small { display:block; font-size:9pt; color:#9299a6; letter-spacing:2px; margin-top:1.5mm; }
  .meta { margin-top:6mm; border:1px solid #d9dde3; border-radius:2mm; padding:4mm 5mm; font-size:10.5pt; }
  .meta .row { display:flex; }
  .meta .row > div { flex:1; padding:2px 0; }
  .meta .k { color:#8a919f; display:inline-block; width:22mm; }
  table { width:100%; border-collapse:collapse; margin-top:6mm; font-size:10.5pt; }
  th { border-bottom:1.5px solid #1f2329; padding:2.5mm 1mm; font-weight:600; }
  td { border-bottom:1px dashed #d9dde3; padding:3mm 1mm; vertical-align:top; }
  td.c, th.c { text-align:center; }
  td.qty { font-weight:700; font-size:12pt; width:18mm; }
  .dish { font-size:12pt; font-weight:600; }
  .spec, .remark { font-size:9pt; color:#6b7280; margin-top:1mm; }
  .remark { color:#c2410c; }
  tr.empty td { text-align:center; color:#9299a6; }
  .sums { margin-top:4mm; margin-left:auto; width:70mm; }
  .sum { display:flex; justify-content:space-between; padding:2mm 0; font-size:10.5pt; border-bottom:1px dashed #d9dde3; }
  .sum.total { border-bottom:0; border-top:1.5px solid #1f2329; margin-top:2mm; font-size:13pt; font-weight:700; }
  .sum.total b { color:#c00000; }
  .notes { margin-top:5mm; font-size:10pt; color:#6b7280; line-height:1.8; border-top:1px dashed #d9dde3; padding-top:3mm; }
  .foot { margin-top:8mm; text-align:center; font-size:10pt; color:#9299a6; }
  .foot .sign { margin-top:8mm; display:flex; justify-content:space-between; font-size:10pt; }
  .foot .sign span { border-top:1px solid #9299a6; padding-top:2mm; min-width:50mm; text-align:center; }
  @media print {
    html,body { background:#fff; }
    .toolbar, .spacer { display:none !important; }
    .sheet { box-shadow:none; width:auto; padding:0; }
  }
</style></head>
<body>
  <div class="toolbar">
    <b>${meta.title} · ${esc(order.orderNo || '')}</b>
    <span>${opt.reprint ? '补打件' : ''} 请关闭「页眉页脚」，纸张选择 A4</span>
    <button onclick="window.print()">打印 / 另存为 PDF</button>
  </div>
  <div class="spacer"></div>
  <section class="sheet">
    <div class="shop">${esc(opt.shopName || '')}</div>
    <div class="doctitle"><b>${meta.title}</b><small>${meta.subtitle}${opt.reprint ? ' · REPRINT' : ''}</small></div>
    <div class="meta">
      <div class="row">
        <div><span class="k">订单号</span>${esc(order.orderNo || '-')}</div>
        <div><span class="k">状态</span>${esc(statusText || order.orderStatus || '-')}</div>
      </div>
      <div class="row">
        <div><span class="k">桌台</span>${esc(tableLabel)}</div>
        <div><span class="k">人数</span>${esc(people)}</div>
      </div>
      <div class="row">
        <div><span class="k">下单时间</span>${esc(order.createTime || '-')}</div>
        <div><span class="k">打印时间</span>${esc(opt.printTime || '')}</div>
      </div>
    </div>
    <table>
      <thead>${headCols}</thead>
      <tbody>${itemRows(order, withPrice)}</tbody>
    </table>
    ${withPrice ? `<div class="sums">${sumRows}</div>` : ''}
    ${notesHtml}
    <div class="foot">
      ${meta.foot}
      ${withPrice ? '<div class="sign"><span>收银签字</span><span>顾客签字</span></div>' : '<div class="sign"><span>划单确认</span><span>传菜确认</span></div>'}
    </div>
  </section>
  <script>
    window.addEventListener('load', function () { setTimeout(function () { window.print(); }, 300); });
  </script>
</body></html>`;
}

/** 缓存店铺名：一单可能连点多次 A4，没必要每次都请求配置 */
let shopNameCache: string | null = null;
async function resolveShopName(api: () => Promise<PublicConfig>): Promise<string> {
  if (shopNameCache) return shopNameCache;
  try {
    const cfg = await api();
    shopNameCache = cfg?.shop_name || '';
  } catch {
    shopNameCache = '';
  }
  return shopNameCache;
}

function nowText(): string {
  const d = new Date();
  const p = (n: number): string => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

/**
 * 打开 A4 打印页。
 * @param order 订单详情
 * @param docType 单据类型
 * @param opt { getPublicConfig, reprint }
 * @returns 是否被浏览器拦截
 */
export async function printOrderA4(
  order: OrderDetail | null,
  docType: 'kitchen' | 'guest',
  opt: A4PrintOptions = {}
): Promise<boolean> {
  if (!order) return false;
  const shopName = opt.getPublicConfig ? await resolveShopName(opt.getPublicConfig) : '';
  const html = buildA4Html(order, docType, { shopName, reprint: opt.reprint !== false, printTime: nowText() });
  return openPrintWindow(html);
}
