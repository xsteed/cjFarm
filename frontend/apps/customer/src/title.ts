/**
 * 设置 H5 顾客端浏览器标题为「商家名 点餐系统」。
 * 商家名为空时回退为「点餐系统」,避免出现「 点餐系统」这种前导空格。
 */
export function setCustomerTitle(shopName?: string): void {
  const name = (shopName ?? '').trim();
  document.title = name ? `${name} 点餐系统` : '点餐系统';
}
